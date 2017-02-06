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

	"k8s.io/client-go/pkg/api/v1"

	"github.com/aws/aws-sdk-go/aws"
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

func addComponentToState(awsClient *AwsClient, component string, state *rollerState) (*componentType, error) {
	myComponent := &componentType{
		name:  component,
		start: time.Now(),
	}

	// Get list of instances by filter on tag ServiceComponent == component
	//	params.Filters = append(params.Filters, newEC2Filter("tag:ServiceComponent", "k8s-master"))
	instances, err := awsClient.ec2.controller.InstancesMatchingTagValue("ServiceComponent", component, state.inventory)
	if err != nil {
		return myComponent, err
	}
	myComponent.instances = instances

	asgs, err := awsClient.ec2.controller.GetUniqueTagValues("aws:autoscaling:groupName", instances)
	if err != nil {
		return myComponent, err
	}
	myComponent.asgs = asgs

	state.components = append(state.components, myComponent)
	return myComponent, nil
}

func validateEtcdInstances(awsClient *AwsClient, component *componentType) error {
	instances, err := awsClient.ec2.controller.InstancesMatchingTagValue("healthy", "True", component.instances)
	if err != nil {
		return err
	}

	if len(instances) != len(component.instances) {
		component.err = fmt.Errorf("Etcd components are not healthy.  Please fix and run again")
		glog.V(4).Infof("%s", component.err)
		return component.err
	}
	return nil
}

// Obtains initial list of instances, does etcd validation, and initializes the state
// with the component objects.
func replaceInstancesPrepare(awsClient *AwsClient, component string, scalingProcesses []*string) (*componentType, []string, error) {
	var instanceList []string

	myComponent, err := addComponentToState(awsClient, component, state)
	if err != nil {
		return myComponent, instanceList, fmt.Errorf("Failed to add component to state: %s", err)
	}

	if component == "etcd" {
		err = validateEtcdInstances(awsClient, myComponent)
		if err != nil {
			return myComponent, instanceList, fmt.Errorf("Failed to validate etcd instances: %s", err)
		}
	}

	for _, e := range myComponent.instances {
		instanceList = append(instanceList, *e.InstanceId)
	}
	glog.V(4).Infof("Component %s has starting instance Ids %v\n", component, instanceList)

	for _, asg := range myComponent.asgs {
		glog.V(4).Infof("Suspending autoscaling processes for %s\n", asg)
		_, err := awsClient.autoscaling.controller.manageASGProcesses(awsClient.autoscaling.client, asg, scalingProcesses, "suspend")
		if err != nil {
			return myComponent, instanceList, fmt.Errorf("An error occurred while suspending processes on %s\n Error: %s\n", asg, err)
		}
	}

	// Defer resume autoscaling activities
	for _, asg := range myComponent.asgs {
		defer resumeASGProcesses(awsClient, asg, scalingProcesses, myComponent)
	}

	return myComponent, instanceList, nil
}

func resumeASGProcesses(awsClient *AwsClient, asg string, scalingProcesses []*string, component *componentType) {
	glog.V(4).Infof("Resuming autoscaling processes for %s\n", asg)
	_, err := awsClient.autoscaling.controller.manageASGProcesses(awsClient.autoscaling.client, asg, scalingProcesses, "resume")
	if err != nil {
		glog.Errorf("An error occurred while resuming processes on %s\n Error: %s\n", asg, err)
		component.status = false
	}
}

func cordonKubernetesNodes(kubernetesClient KubernetesClient, instanceList []string) error {
	nodesController := KubernetesNodes{}
	labels := make(map[string]string)
	var nodeListToCordon []v1.Node

	glog.V(4).Infof("Fetching kubernetes nodes for instance IDs: %s\n", instanceList)
	for _, instanceId := range instanceList {
		labels["instance-id"] = instanceId
		nodeList, err := nodesController.GetNodesByLabel(kubernetesClient, labels)
		if err != nil {
			return fmt.Errorf("Failed to populate node by label: %s", err)
		}
		for _, node := range nodeList.Items {
			nodeListToCordon = append(nodeListToCordon, node)
		}
	}

	for _, node := range nodeListToCordon {
		glog.V(4).Infof("Cordoning kubernetes node: %s\n", node.Name)
		node.Spec.Unschedulable = true
		node := &node
		updatedNode, err := nodesController.UpdateNode(kubernetesClient, node)
		if err != nil {
			return fmt.Errorf("Failed to update node: %s", err)
		}
		if updatedNode.Spec.Unschedulable != true {
			return fmt.Errorf("Failed to update node for unknown reason")
		}
	}
	return nil
}

// Terminates and checks one or more instances at a time, in a "rolling" fashion. Differs from
// replaceInstancesVerifyAndTerminate() in that it terminates the instances before verifying replacements.
// Useful for small ASGs or when there is an upper limit to the number of instances you can have in the an ASG.
func replaceInstancesTerminateAndVerify(state *rollerState, awsClient *AwsClient, component string, ansibleVersion string, wg *sync.WaitGroup) error {
	defer wg.Done()

	// The number of instances to terminate and replace at a time
	newInstanceRollingCount := 1

	scalingProcesses := []*string{
		aws.String("AZRebalance"),
	}

	myComponent, _, err := replaceInstancesPrepare(awsClient, component, scalingProcesses)
	if err != nil {
		err = fmt.Errorf("An error occurred while preparing for instance replacement for %s\n Error: %s\n", myComponent.name, err)
		glog.V(4).Infof("%s", err)
		return err
	}

	glog.V(4).Infof("Starting instance termination verify loop for component %s", myComponent.name)
	for _, n := range myComponent.instances {
		terminateTime := time.Now()
		r, err := awsClient.ec2.controller.terminateInstance(awsClient.ec2.client, *n.InstanceId)
		if err != nil {
			err = fmt.Errorf("An error occurred while terminating %s instance %s\n Error: %s\n Response: %s\n", myComponent.name, *n.InstanceId, err, r)
			glog.V(4).Infof("%s", err)
			return err
		}

		newInstances, err := awsClient.ec2.controller.findReplacementInstances(awsClient.ec2.client, component, ansibleVersion, newInstanceRollingCount, terminateTime)
		if err != nil {
			err = fmt.Errorf("An error occurred finding the replacement instances for component %s\n Error: %s\n", component, err)
			glog.V(4).Infof("%s", err)
			return err
		}

		err = awsClient.ec2.controller.verifyReplacementInstances(awsClient.ec2.client, component, newInstances)
		if err != nil {
			err = fmt.Errorf("An error occurred verifying the health of instances %s\n Error: %s\n", newInstances, err)
			glog.V(4).Infof("%s", err)
			return err
		}
	}

	myComponent.status = true
	myComponent.finish = time.Now()

	glog.V(4).Infof("Completed normal instance termination verify loop for component %s", myComponent.name)
	return nil
}

// Spins up new replacement instances, verifies them, and then terminates the old instances. Differs from
// replaceInstancesTerminateAndVerify() in that it verifies replacements before terminating the old instances.
// Useful for large ASGs when there is no upper limit to the number of instances you can have in the ASG.
func (c *AwsEc2Controller) replaceInstancesVerifyAndTerminate(awsClient *AwsClient, awsAutoscaling AwsAutoscaling, component string, ansibleVersion string, wg *sync.WaitGroup) error {
	defer wg.Done()

	scalingProcesses := []*string{
		aws.String("AZRebalance"),
		aws.String("Terminate"),
	}

	myComponent, instanceList, err := replaceInstancesPrepare(awsClient, component, scalingProcesses)
	if err != nil {
		err = fmt.Errorf("An error occurred while preparing for instance replacement for %s\n Error: %s\n", myComponent.name, err)
		glog.V(4).Infof("%s", err)
		return err
	}

	var desiredCount int

	// Ensure the total current instance count is the same as the desired count of the ASG
	for _, asg := range myComponent.asgs {
		count, err := awsClient.autoscaling.controller.getDesiredCount(awsClient.autoscaling.client, asg)
		desiredCount = int(count)
		if err != nil {
			err = fmt.Errorf("Got error when trying to get the desired count for ASG %s: %s. ", asg, err)
			glog.V(4).Infof("%s", err)
			return err
		}
		if len(instanceList) != desiredCount {
			err := fmt.Errorf("The desired count (%d) in the ASG %s does not match the number of instances in the instance list: %s. ", len(asg), instanceList)
			glog.V(4).Infof("%s", err)
			return err
		}
	}

	// Double the desired count
	temporaryDesiredCount := int64(desiredCount * 2)
	creationTime := time.Now()
	for _, asg := range myComponent.asgs {
		_, err = awsClient.autoscaling.controller.setDesiredCount(awsClient.autoscaling.client, asg, temporaryDesiredCount)
		if err != nil {
			err = fmt.Errorf("Got error when trying to set the desired count for ASG %s: %s. ", asg, err)
			glog.V(4).Infof("%s", err)
			return err
		}
	}

	// Wait for all new nodes to come up before continuing
	newInstances, err := awsClient.ec2.controller.findReplacementInstances(awsClient.ec2.client, component, ansibleVersion, desiredCount, creationTime)
	if err != nil {
		err = fmt.Errorf("An error occurred finding the replacement instances for component %s\n Error: %s\n", component, err)
		glog.V(4).Infof("%s", err)
		return err
	}

	err = awsClient.ec2.controller.verifyReplacementInstances(awsClient.ec2.client, component, newInstances)
	if err != nil {
		err = fmt.Errorf("An error occurred verifying the health of instances %s\n Error: %s\n", newInstances, err)
		glog.V(4).Infof("%s", err)
		return err
	}

	// Mark all the old kubernetes nodes as unschedulable. This is necessary because during the following
	// termination step, we do not want pods to be rescheduled on the old nodes
	glog.V(4).Infof("Starting kubernetes cordon process for %s", myComponent.name)
	kubernetesClient := NewClient(kubernetesEndpoint, kubernetesUsername, kubernetesPassword)
	err = cordonKubernetesNodes(kubernetesClient, instanceList)
	if err != nil {
		err = fmt.Errorf("An error occurred attempting to cordon kubernetes nodes %s\n Error: %s\n", newInstances, err)
		glog.V(4).Infof("%s", err)
		return err
	}

	// Terminate the original instances one at a time and sleep for sleepSeconds in between
	glog.V(4).Infof("Starting instance termination for cordoned kubernetes nodes for %s", myComponent.name)
	sleepSeconds := time.Duration(30 * time.Second)
	for _, instanceId := range instanceList {
		response, err := awsClient.ec2.controller.terminateInstance(awsClient.ec2.client, instanceId)
		if err != nil {
			err = fmt.Errorf("An error occurred while terminating %s instance %s\n Error: %s\n Response: %s\n", myComponent.name, instanceId, err, response)
			glog.V(4).Infof("%s", err)
			return err
		}
		glog.V(4).Infof("Waiting %s for %s to terminate", sleepSeconds, instanceId)
		time.Sleep(sleepSeconds)
	}

	// Set desired count back to what it was originally
	for _, asg := range myComponent.asgs {
		_, err = awsClient.autoscaling.controller.setDesiredCount(awsClient.autoscaling.client, asg, int64(desiredCount))
		if err != nil {
			err = fmt.Errorf("Got error when trying to set the desired count for ASG %s: %s. ", asg, err)
			glog.V(4).Infof("%s", err)
			return err
		}
	}

	return err
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
		go replaceInstancesTerminateAndVerify(state, awsClient, component, ansibleVersion, &wg)
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
