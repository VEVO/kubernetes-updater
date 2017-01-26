package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/glog"
)

var (
	cluster            = os.Getenv("CLUSTER")
	awsAccount         = os.Getenv("AWS_ACCOUNT")
	awsProfile         = os.Getenv("AWS_PROFILE")
	awsRegion          = os.Getenv("AWS_REGION")
	slackToken         = os.Getenv("SLACK_WEBHOOK")
	rollerComponents   = os.Getenv("ROLLER_COMPONENTS")
	rollerLogLevel     = os.Getenv("ROLLER_LOG_LEVEL")
	ansibleVersion     = os.Getenv("ANSIBLE_VERSION")
	kubernetesUsername = os.Getenv("KUBERNETES_USERNAME")
	kubernetesPassword = os.Getenv("KUBERNETES_PASSWORD")
	state              *rollerState
	kubernetesCluster  string
	kubernetesEndpoint string
	targetComponents   []string
	defaultComponents  = []string{
		"k8s-node",
		"k8s-master",
		"etcd",
	}
	clusterAutoscalerServiceName      = "cluster-autoscaler"
	clusterAutoscalerServiceNamespace = "kube-system"
)

type componentType struct {
	name      string
	start     time.Time
	finish    time.Time
	status    bool
	instances []*ec2.Instance
	asgs      []string
	err       error
}

type rollerState struct {
	components        []*componentType
	startTime         time.Time
	inventory         []*ec2.Instance
	SlackText         string `json:"text"`
	clusterAutoscaler clusterAutoscalerState
}

type clusterAutoscalerState struct {
	enabled bool
	status  string
	err     error
}

func timeStamp() string {
	return time.Now().Format(time.RFC822)
}

func (s *rollerState) SlackPost() error {
	client := &http.Client{}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(
		"POST",
		slackToken,
		bytes.NewBuffer(b))

	if err != nil {
		return err
	}

	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)
	defer resp.Body.Close()
	if err != nil {
		return err
	}

	_, err = ioutil.ReadAll(resp.Body)
	return err
}

func (s *rollerState) Summary() error {
	var summary string
	status := "success"

	for _, c := range s.components {
		if !c.status {
			status = "failure"
			break
		}
	}

	if s.clusterAutoscaler.status == "failure" {
		status = "failure"
	}

	duration := time.Since(s.startTime)
	summary = fmt.Sprintf("Finished a rolling update on cluster %s with the components %+v as the target components.\nOverall status: %s\nOverall duration: %v\n", kubernetesCluster, targetComponents, status, duration-(duration%time.Minute))

	for _, c := range s.components {
		var status string
		duration := time.Since(c.start)
		if c.status {
			status = "success"
		} else {
			status = "failure"
		}

		cs := fmt.Sprintf("Component %s status: %s - duration: %v\n", c.name, status, duration-(duration%time.Minute))
		if c.err != nil {
			cs = cs + fmt.Sprintf("Component %s error: %s\n", c.name, c.err)
		}

		summary = summary + cs
	}

	summary = summary + fmt.Sprintf("Cluster autoscaler enabled: %t, status: %s", s.clusterAutoscaler.enabled, s.clusterAutoscaler.status)

	s.SlackText = summary
	err := s.SlackPost()
	return err
}

func setReplicas(replicas int32) error {
	client := NewClient(kubernetesEndpoint, kubernetesUsername, kubernetesPassword)
	deploymentController := KubernetesDeployment{
		service:   clusterAutoscalerServiceName,
		namespace: clusterAutoscalerServiceNamespace,
	}
	_, err := SetReplicasForDeployment(client, deploymentController, replicas)
	return err
}

func disableClusterAutoscaler(*rollerState) {
	glog.V(4).Info("Disabling the cluster autoscaler")
	err := setReplicas(0)
	if err == nil {
		glog.V(4).Info("Successfully disabled the cluster autoscaler")
		state.clusterAutoscaler.enabled = true
	} else {
		state.clusterAutoscaler.status = "failure"
		errorMsg := fmt.Sprintf("Error: unable to manage the cluster-autoscaler deployment, will skip. Error was: %s", err)
		state.clusterAutoscaler.err = errors.New(errorMsg)
		fmt.Println(errorMsg)
	}
}

func enableClusterAutoscaler(*rollerState) {
	glog.V(4).Info("Enabling the cluster autoscaler")
	err := setReplicas(1)
	if err == nil {
		glog.V(4).Info("Successfully enabled the cluster autoscaler")
		state.clusterAutoscaler.enabled = true
	} else {
		state.clusterAutoscaler.status = "failure"
		errorMsg := fmt.Sprintf("Error: unable to re-enable the cluster-autoscaler deployment. Error was: %s", err)
		state.clusterAutoscaler.err = errors.New(errorMsg)
		fmt.Println(errorMsg)
	}
}

func main() {
	flag.Parse()
	flag.Lookup("logtostderr").Value.Set("true")

	_ = os.Setenv("AWS_SDK_LOAD_CONFIG", "true")

	if rollerLogLevel != "" {
		flag.Lookup("v").Value.Set(rollerLogLevel)
	} else {
		flag.Lookup("v").Value.Set("2")
	}

	glog.Info("Log level set to: ", flag.Lookup("v").Value)

	if cluster == "" {
		glog.Fatal("Set the CLUSTER variable to the name of the target kubernetes cluster")
	}

	if awsRegion == "" {
		glog.Fatal("Set the AWS_REGION variable to the name of the desired AWS region")
	}

	if awsAccount == "" && awsProfile == "" {
		glog.Fatal("Set one of the variables AWS_ACCOUNT or AWS_PROFILE")
	}

	if ansibleVersion == "" {
		glog.Fatal("Set the ANSIBLE_VERSION variable to the desired ansible git sha")
	}

	if slackToken == "" {
		glog.Fatal("Set the SLACK_WEBHOOK variable to desired webhook")
	}

	kubernetesCluster = fmt.Sprintf("%s-%s-%s", awsAccount, awsRegion, cluster)

	if kubernetesUsername == "" {
		glog.Fatal("Set the KUBERNETES_USERNAME variable to desired kubernetes username")
	}

	if kubernetesPassword == "" {
		glog.Fatal("Set the KUBERNETES_PASSWORD variable to desired kubernetes password")
	}

	kubernetesEndpoint = fmt.Sprintf("https://%s-%s-kubernetes.vevo%s.com", awsRegion, cluster, awsAccount)

	awsClient := NewAwsClient()
	params := &ec2.DescribeInstancesInput{}
	params.Filters = []*ec2.Filter{
		awsClient.ec2.controller.newEC2Filter("tag:KubernetesCluster", kubernetesCluster),
		awsClient.ec2.controller.newEC2Filter("instance-state-name", "running"),
	}
	inv, err := awsClient.ec2.controller.DescribeInstancesNotMatchingAnsibleVersion(awsClient.ec2.client, params, ansibleVersion)

	if err != nil {
		glog.Fatalf("An error occurred getting the EC2 inventory: %s.\n", err)
	}

	state = &rollerState{
		startTime: time.Now(),
		inventory: inv,
		clusterAutoscaler: clusterAutoscalerState{
			enabled: false,
			status:  "success",
		},
	}

	// Are we going to roll all of etcd, k8s-master and k8s-node or just
	// a subset.
	if rollerComponents != "" {
		targetComponents = strings.Split(rollerComponents, ",")
	} else {
		targetComponents = defaultComponents
	}

	// Only manage the cluster autoscaler if rolling the k8s-node component.
	// If managing it fails, continue but consider the overall state failed.
	for _, component := range targetComponents {
		if component == "k8s-node" {
			disableClusterAutoscaler(state)
		}
	}

	state.SlackText = fmt.Sprintf("Starting a rolling update on cluster %s with the components %+v as the target components.\nAnsible version is set to %s\nManagement of cluster autoscaler is set to %t", kubernetesCluster, targetComponents, ansibleVersion, state.clusterAutoscaler.enabled)

	err = state.SlackPost()
	glog.V(4).Infof("Slack Post: %s", state.SlackText)
	if err != nil {
		glog.Errorf("An error occurred posting to slack.\nError %s\n", err)
	}

	var wg sync.WaitGroup
	for _, component := range targetComponents {
		wg.Add(1)
		go awsClient.ec2.controller.terminateAndVerifyComponentInstances(awsClient.ec2.client, newAWSAutoscalingClient(), component, ansibleVersion, &wg)
	}

	wg.Wait()

	if state.clusterAutoscaler.enabled {
		enableClusterAutoscaler(state)
	}

	err = state.Summary()
	if err != nil {
		glog.Errorf("An error occurred psting to slack.\nError %s\n", err)
	}
	glog.V(4).Infof("Slack Post: %s", state.SlackText)
}
