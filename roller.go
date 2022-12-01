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

	corev1 "k8s.io/api/core/v1"

	"strconv"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/glog"
)

var (
	cluster                  = os.Getenv("CLUSTER")
	awsAccount               = os.Getenv("AWS_ACCOUNT")
	awsProfile               = os.Getenv("AWS_PROFILE")
	awsRegion                = os.Getenv("AWS_REGION")
	slackToken               = os.Getenv("SLACK_WEBHOOK")
	rollerComponents         = os.Getenv("ROLLER_COMPONENTS")
	rollerLogLevel           = os.Getenv("ROLLER_LOG_LEVEL")
	ansibleVersion           = os.Getenv("ANSIBLE_VERSION")
	kubernetesServer         = os.Getenv("KUBERNETES_SERVER")
	kubernetesToken          = os.Getenv("KUBERNETES_TOKEN")
	terminationWaitPeriodStr = os.Getenv("TERMINATION_WAIT_PERIOD_SECONDS")
	desiredCountStepStr      = os.Getenv("TERMINATION_BATCH_NODES_SIZE")
	desiredCountStep         = 5
	state                    *rollerState
	kubernetesCluster        string
	targetComponents         []string
	defaultComponents        = []string{
		"k8s-node",
		"k8s-master",
		"etcd",
	}
	clusterAutoscalerServiceName      = "cluster-autoscaler"
	clusterAutoscalerServiceNamespace = "kube-system"
	clusterTerminatorServiceName      = "terminator"
	clusterTerminatorServiceNamespace = "kube-system"
	provisionAttemptCounter           = make(map[string]int)
	terminationWaitPeriod             = time.Duration(5 * time.Second)
	apiKey                            = os.Getenv("DATADOG_API_KEY")
	appKey                            = os.Getenv("DATADOG_APP_KEY")
)

const (
	remainingThreshold = 10
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
	clusterTerminator clusterTerminatorState
	downtimeID        int
	dd                *ddClientConfig
}

type clusterAutoscalerState struct {
	enabled bool
	status  string
	err     error
}

type clusterTerminatorState struct {
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
	if err != nil {
		return err
	}
	defer resp.Body.Close()

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
		duration := c.finish.Sub(c.start)
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

func setReplicas(deployment, namespace string, replicas int32) error {
	glog.V(4).Infof("Setting replicas to %d for deployment %s", replicas, deployment)
	client := newClient(kubernetesServer, kubernetesToken)
	deploymentController := kubernetesDeployment{
		service:   deployment,
		namespace: namespace,
	}
	_, err := setReplicasForDeployment(client, deploymentController, replicas)
	return err
}

func disableClusterAutoscaler(*rollerState) {
	glog.V(4).Info("Disabling the cluster autoscaler")
	err := setReplicas(clusterAutoscalerServiceName, clusterAutoscalerServiceNamespace, 0)
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

func disableClusterTerminator(*rollerState) {
	glog.V(4).Info("Disabling the cluster terminator")
	err := setReplicas(clusterTerminatorServiceName, clusterTerminatorServiceNamespace, 0)
	if err == nil {
		glog.V(4).Info("Successfully disabled the cluster terminator")
		state.clusterTerminator.enabled = true
	} else {
		state.clusterTerminator.status = "failure"
		errorMsg := fmt.Sprintf("Error: unable to manage the terminator deployment, will skip. Error was: %s", err)
		state.clusterTerminator.err = errors.New(errorMsg)
		fmt.Println(errorMsg)
	}
}

func enableClusterAutoscaler(*rollerState) {
	glog.V(4).Info("Enabling the cluster autoscaler")
	err := setReplicas(clusterAutoscalerServiceName, clusterAutoscalerServiceNamespace, 1)
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

func enableClusterTerminator(*rollerState) {
	glog.V(4).Info("Enabling the cluster terminator")
	err := setReplicas(clusterTerminatorServiceName, clusterTerminatorServiceNamespace, 1)
	if err == nil {
		glog.V(4).Info("Successfully enabled the cluster terminator")
		state.clusterTerminator.enabled = true
	} else {
		state.clusterTerminator.status = "failure"
		errorMsg := fmt.Sprintf("Error: unable to re-enable the terminator deployment. Error was: %s", err)
		state.clusterTerminator.err = errors.New(errorMsg)
		fmt.Println(errorMsg)
	}
}

func addComponentToState(awsClient *awsClient, component string, state *rollerState) (*componentType, error) {
	myComponent := &componentType{
		name:  component,
		start: time.Now(),
	}

	// Get list of instances by filter on tag ServiceComponent == component
	//	params.Filters = append(params.Filters, newEC2Filter("tag:ServiceComponent", "k8s-master"))
	instances, err := awsClient.ec2.instancesMatchingTagValue("ServiceComponent", component, state.inventory)
	if err != nil {
		return myComponent, err
	}
	myComponent.instances = instances

	asgs, err := awsClient.ec2.getUniqueTagValues("aws:autoscaling:groupName", instances)
	if err != nil {
		return myComponent, err
	}
	myComponent.asgs = asgs

	state.components = append(state.components, myComponent)
	return myComponent, nil
}

func validateEtcdInstances(awsClient *awsClient, component *componentType) error {
	instances, err := awsClient.ec2.instancesMatchingTagValue("healthy", "True", component.instances)
	if err != nil {
		return err
	}

	if len(instances) != len(component.instances) {
		component.err = fmt.Errorf("etcd components are not healthy.  Please fix and run again")
		glog.V(4).Infof("%s", component.err)
		return component.err
	}
	return nil
}

// Obtains initial list of instances, does etcd validation, and initializes the state
// with the component objects.
func replaceInstancesPrepare(awsClient *awsClient, component string, scalingProcesses []*string) (*componentType, []string, error) {
	var instanceList []string

	myComponent, err := addComponentToState(awsClient, component, state)
	if err != nil {
		return myComponent, instanceList, fmt.Errorf("failed to add component to state: %s", err)
	}

	if component == "etcd" {
		err = validateEtcdInstances(awsClient, myComponent)
		if err != nil {
			return myComponent, instanceList, fmt.Errorf("failed to validate etcd instances: %s", err)
		}
	}

	for _, e := range myComponent.instances {
		instanceList = append(instanceList, *e.InstanceId)
	}
	glog.V(4).Infof("Component %s has starting instance Ids %v\n", component, instanceList)

	for _, asg := range myComponent.asgs {
		glog.V(4).Infof("Suspending autoscaling processes for %s\n", asg)
		_, err := awsClient.autoscaling.manageASGProcesses(asg, scalingProcesses, "suspend")
		if err != nil {
			return myComponent, instanceList, fmt.Errorf("an error occurred while suspending processes on %s\n Error: %s", asg, err)
		}
	}

	return myComponent, instanceList, nil
}

func resumeASGProcesses(awsClient *awsClient, scalingProcesses []*string, component *componentType) {
	for _, asg := range component.asgs {
		glog.V(4).Infof("Resuming autoscaling processes for %s\n", asg)
		_, err := awsClient.autoscaling.manageASGProcesses(asg, scalingProcesses, "resume")
		if err != nil {
			glog.Errorf("an error occurred while resuming processes on %s\n Error: %s", asg, err)
			component.status = false
		}
	}
}

func cordonKubernetesNodes(kubernetesClient kubernetesClient, instanceList []string) error {
	nodesController := kubernetesNodes{}
	labels := make(map[string]string)
	var nodeListToCordon []corev1.Node

	glog.V(4).Infof("Fetching kubernetes nodes for instance IDs: %s\n", instanceList)
	for _, instanceID := range instanceList {
		labels["instance-id"] = instanceID
		nodeList, err := nodesController.getNodesByLabel(kubernetesClient, labels)
		if err != nil {
			return fmt.Errorf("failed to populate node by label: %s", err)
		}
		nodeListToCordon = append(nodeListToCordon, nodeList.Items...)
	}

	nodesFail := make(map[string]error)
	for _, node := range nodeListToCordon {
		glog.V(4).Infof("Cordoning kubernetes node: %s\n", node.Name)
		node.Spec.Unschedulable = true
		node := &node
		updatedNode, err := nodesController.updateNode(kubernetesClient, node)
		if err != nil {
			nodesFail[node.Name] = err
		}
		if !updatedNode.Spec.Unschedulable {
			nodesFail[node.Name] = fmt.Errorf("failed for unknown reason")
		}
	}

	if len(nodesFail) > 0 {
		return fmt.Errorf("failed to cordon nodes: %s", nodesFail)
	}
	return nil
}

func drainKubernetesNodes(kubernetesClient kubernetesClient, instanceList []string) error {
	nodesController := kubernetesNodes{}
	labels := make(map[string]string)
	var nodeListToDrain []corev1.Node

	glog.V(4).Infof("Fetching kubernetes nodes for instance IDs: %s\n", instanceList)
	for _, instanceID := range instanceList {
		labels["instance-id"] = instanceID
		nodeList, err := nodesController.getNodesByLabel(kubernetesClient, labels)
		if err != nil {
			return fmt.Errorf("failed to populate node by label: %s", err)
		}
		nodeListToDrain = append(nodeListToDrain, nodeList.Items...)
	}

	nodesFail := make(map[string]error)
	for _, node := range nodeListToDrain {
		glog.V(4).Infof("Draining kubernetes node: %s\n", node.Name)
		node := &node
		err := nodesController.drainNode(kubernetesClient, node)
		if err != nil {
			nodesFail[node.Name] = err
		}
	}

	if len(nodesFail) > 0 {
		return fmt.Errorf("failed to drain nodes: %s", nodesFail)
	}
	return nil
}

// Terminates and checks one or more instances at a time, in a "rolling" fashion. Differs from
// replaceInstancesVerifyAndTerminate() in that it terminates the instances before verifying replacements.
// Useful for small ASGs or when there is an upper limit to the number of instances you can have in the an ASG.
func replaceInstancesTerminateAndVerify(awsClient *awsClient, component, ansibleVersion string, wg *sync.WaitGroup) error {
	glog.V(4).Infof("Starting process to terminate and replace instances for %s", component)

	defer wg.Done()

	// The number of instances to terminate and replace at a time
	newInstanceRollingCount := 1

	scalingProcesses := []*string{
		aws.String("AZRebalance"),
	}

	myComponent, _, err := replaceInstancesPrepare(awsClient, component, scalingProcesses)
	if err != nil {
		err = fmt.Errorf("an error occurred while preparing for instance replacement for %s\n Error: %s", myComponent.name, err)
		glog.V(4).Infof("%s", err)
		return err
	}

	// Defer resume autoscaling activities
	defer resumeASGProcesses(awsClient, scalingProcesses, myComponent)

	glog.V(4).Infof("Starting instance termination verify loop for component %s", myComponent.name)
	for _, n := range myComponent.instances {
		terminateTime := time.Now()
		r, err := awsClient.ec2.terminateInstance(*n.InstanceId)
		if err != nil {
			err = fmt.Errorf("an error occurred while terminating %s instance %s\n Error: %s\n Response: %s", myComponent.name, *n.InstanceId, err, r)
			glog.V(4).Infof("%s", err)
			return err
		}

		_, err = findAndVerifyReplacementInstances(awsClient, myComponent, ansibleVersion, newInstanceRollingCount, terminateTime)
		if err != nil {
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
func replaceInstancesVerifyAndTerminate(awsClient *awsClient, component string, ansibleVersion string, wg *sync.WaitGroup) error {
	glog.V(4).Infof("Starting process to start new instances and terminate existing for %s", component)

	defer wg.Done()

	scalingProcesses := []*string{
		aws.String("AZRebalance"),
		aws.String("Terminate"),
	}
	myComponent, instanceList, err := replaceInstancesPrepare(awsClient, component, scalingProcesses)
	if err != nil {
		err = fmt.Errorf("an error occurred while preparing for instance replacement for %s\n Error: %s", myComponent.name, err)
		glog.V(4).Infof("%s", err)
		return err
	}

	// Defer resume autoscaling activities
	scalingProcesses = []*string{
		aws.String("AZRebalance"),
		aws.String("Terminate"),
		aws.String("Launch"),
	}
	defer resumeASGProcesses(awsClient, scalingProcesses, myComponent)

	var desiredCount int

	// Ensure the total current instance count is the same as the desired count of the ASG
	for _, asg := range myComponent.asgs {
		count, err := awsClient.autoscaling.getDesiredCount(asg)
		desiredCount = int(count)
		glog.V(4).Infof("Starting desired count for ASG %s is %d", asg, desiredCount)
		if err != nil {
			err = fmt.Errorf("got error when trying to get the desired count for ASG %s: %s. ", asg, err)
			glog.V(4).Infof("%s", err)
			return err
		}
		if len(instanceList) != desiredCount {
			err := fmt.Errorf("the desired count (%d) in the ASG %s does not match the number of instances in the instance list: %s. ", desiredCount, asg, instanceList)
			glog.V(4).Infof("%s", err)
			return err
		}
	}

	desiredCountTarget := desiredCount * 2
	temporaryDesiredCount := desiredCount
	var findNewCount int

	for remaining := desiredCountTarget - temporaryDesiredCount; remaining != 0; {

		// Ensure that someone named Derek didn't enable the autoscaler while we are rolling the cluster
		disableClusterAutoscaler(state)

		remaining = desiredCountTarget - temporaryDesiredCount
		glog.V(4).Infof("Remaining nodes %d", remaining)

		if remaining <= remainingThreshold {
			temporaryDesiredCount = desiredCountTarget
			findNewCount = remaining
		} else {
			temporaryDesiredCount = temporaryDesiredCount + desiredCountStep
			findNewCount = desiredCountStep
		}

		glog.V(4).Infof("desiredCount is %d, desiredCountTarget is %d and temporaryDesiredCount is %d", desiredCount, desiredCountTarget, temporaryDesiredCount)

		creationTime := time.Now()
		for _, asg := range myComponent.asgs {
			glog.V(4).Infof("Setting desired count for ASG %s to %d", asg, temporaryDesiredCount)
			_, err = awsClient.autoscaling.setDesiredCount(asg, int64(temporaryDesiredCount))
			if err != nil {
				err = fmt.Errorf("got error when trying to set the desired count for ASG %s: %s. ", asg, err)
				glog.V(4).Infof("%s", err)
				return err
			}
		}

		// Verify the new ec2 instances are created and that they are valid
		newInstances, err := findAndVerifyReplacementInstances(awsClient, myComponent, ansibleVersion, findNewCount, creationTime)
		glog.V(4).Infof("newInstances are %v", newInstances)
		if err != nil {
			return err
		}
	}

	// Mark all the old kubernetes nodes as unschedulable. This is necessary because during the following
	// termination step, we do not want pods to be rescheduled on the old nodes
	glog.V(4).Infof("Starting kubernetes cordon process for %s", myComponent.name)
	kubernetesClient := newClient(kubernetesServer, kubernetesToken)
	err = cordonKubernetesNodes(kubernetesClient, instanceList)
	if err != nil {
		err = fmt.Errorf("an error occurred attempting to cordon kubernetes nodes %s\n Error: %s", instanceList, err)
		glog.V(4).Infof("%s", err)
	}
	// Drain all previous nodes, this moves the workload onto the new nodes first, so we come up before we start killing nodes.
	glog.V(4).Infof("Starting kubernetes drain process for %s", myComponent.name)
	err = drainKubernetesNodes(kubernetesClient, instanceList)
	if err != nil {
		err = fmt.Errorf("an error occurred attempting to cordon kubernetes nodes %s\n Error: %s", instanceList, err)
		glog.V(4).Infof("%s", err)
	}
	// Wait for 60 seconds just to let the drain finish and things to calm down
	glog.V(4).Infof("Pausing 1 minute for the drain to calm down")
	time.Sleep(60 * time.Second)

	// Suspend the launch process so the ASG doesn't backfill the instances we're about to terminate
	scalingProcesses = []*string{
		aws.String("Launch"),
	}
	for _, asg := range myComponent.asgs {
		_, err := awsClient.autoscaling.manageASGProcesses(asg, scalingProcesses, "suspend")
		if err != nil {
			return fmt.Errorf("an error occurred while suspending processes on %s\n Error: %s", asg, err)
		}
	}

	// We have to unlock the Terminate process otherwise the instances will never be evicted from the ASG
	scalingProcesses = []*string{
		aws.String("Terminate"),
	}
	resumeASGProcesses(awsClient, scalingProcesses, myComponent)

	// Terminate the original instances one at a time and sleep for sleepSeconds in between
	err = terminateInstances(awsClient, instanceList, myComponent, terminationWaitPeriod)
	if err != nil {
		return err
	}

	for _, asg := range myComponent.asgs {
		asgOk := false
		for loop := 0; loop < 30; loop++ {
			instanceCount, err := awsClient.autoscaling.getInstanceCount(asg)
			if err != nil {
				err = fmt.Errorf("an error occurred attempting to validate number of instances in ASG %s\n Error: %s", asg, err)
				glog.V(4).Infof("%s", err)
				return err
			}
			if instanceCount != desiredCount {
				glog.V(4).Infof("Waiting for all nodes to terminate. Previous desired count for ASG %s must match the number"+
					"of instances in the ASG", asg)
				time.Sleep(30 * time.Second)
				continue
			}
			glog.V(4).Infof("All old nodes in ASG %s have terminated", asg)
			asgOk = true
			break
		}
		if !asgOk {
			err = fmt.Errorf("an error occurred attempting to validate number of instances in ASG %s\n "+
				"Error: Timed out waiting for instances to be removed from ASG", asg)
			glog.V(4).Infof("%s", err)
			return err
		}
	}

	// Set desired count back to what it was originally
	for _, asg := range myComponent.asgs {
		glog.V(4).Infof("Setting desired count for ASG %s to %d", asg, desiredCount)
		_, err = awsClient.autoscaling.setDesiredCount(asg, int64(desiredCount))
		if err != nil {
			err = fmt.Errorf("got error when trying to set the desired count for ASG %s: %s. ", asg, err)
			glog.V(4).Infof("%s", err)
			return err
		}
	}

	myComponent.status = true
	myComponent.finish = time.Now()

	glog.V(4).Infof("Completed normal instance verify and termination loop for component %s", myComponent.name)
	return nil
}

func terminateInstances(awsClient *awsClient, instanceList []string, myComponent *componentType, sleepSeconds time.Duration) error {
	glog.V(2).Infof("Starting instance termination for %s nodes", myComponent.name)
	for _, instanceID := range instanceList {
		response, err := awsClient.ec2.terminateInstance(instanceID)
		if err != nil {
			err = fmt.Errorf("an error occurred while terminating %s instance %s\n Error: %s\n Response: %s", myComponent.name, instanceID, err, response)
			glog.V(4).Infof("%s", err)
			return err
		}
		glog.V(2).Infof("Waiting %s for %s to terminate", sleepSeconds, instanceID)
		time.Sleep(sleepSeconds)
	}
	return nil
}

func findAndVerifyReplacementInstances(awsClient *awsClient, myComponent *componentType, ansibleVersion string, desiredCount int, creationTime time.Time) ([]string, error) {
	if _, ok := provisionAttemptCounter[myComponent.name]; ok {
		provisionAttemptCounter[myComponent.name]++
	} else {
		provisionAttemptCounter[myComponent.name] = 1
	}

	// Wait for all new nodes to come up before continuing
	newInstances, err := awsClient.ec2.findReplacementInstances(myComponent, ansibleVersion, desiredCount, creationTime)
	if err != nil {
		err = fmt.Errorf("an error occurred finding the replacement instances for component %s\n Error: %s", myComponent.name, err)
		glog.V(4).Infof("%s", err)
		return newInstances, err
	}

	instances, err := awsClient.ec2.verifyReplacementInstances(myComponent, newInstances)
	if err != nil {
		if len(instances) > 0 {
			startingInstanceCount := len(newInstances)
			// If failure rate is at or under 25%, we will terminate and retry the failed instances. The exception
			// to this is if we only start out with one or two instances, we will retry if there was only a
			// single node failure.
			retryFailureThreshold := .25

			// If we have a high number of failures, don't attempt to try again
			if startingInstanceCount > 2 {
				if float64(len(instances))/float64(startingInstanceCount) > retryFailureThreshold {
					err = fmt.Errorf("%s: Failure threshold too high (%f%%)", err, retryFailureThreshold*100)
					glog.Error(err)
					return instances, err
				}
			} else {
				if len(instances) > 1 {
					err = fmt.Errorf("%s: Failure threshold too high (%d)", err, len(instances))
					glog.Error(err)
					return instances, err
				}
			}

			// If we've already tried twice with no success, it's time to give up
			if _, ok := provisionAttemptCounter[myComponent.name]; ok {
				if provisionAttemptCounter[myComponent.name] >= 2 {
					err = fmt.Errorf("%s: Reached max number of attempts", err)
					glog.Error(err)
					return instances, err
				}
				glog.Infof("Failed to find valid replacement %s instances. Trying again", myComponent.name)
				now := time.Now()
				terminateInstances(awsClient, instances, myComponent, time.Duration(30*time.Second))
				findAndVerifyReplacementInstances(awsClient, myComponent, ansibleVersion, len(instances), now)
			}
			glog.Errorf("%s", err)
			return instances, err
		}
	}
	if err != nil {
		err = fmt.Errorf("an error occurred verifying the health of instances %s\n Error: %s", newInstances, err)
		glog.V(4).Infof("%s", err)
		return newInstances, err
	}
	return newInstances, nil
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

	kubernetesCluster = fmt.Sprintf("%s-%s-%s", awsAccount, awsRegion, cluster)

	switch {
	case cluster == "":
		glog.Fatal("Set the CLUSTER variable to the name of the target kubernetes cluster")
	case awsRegion == "":
		glog.Fatal("Set the AWS_REGION variable to the name of the desired AWS region")
	case awsAccount == "" && awsProfile == "":
		glog.Fatal("Set one of the variables AWS_ACCOUNT or AWS_PROFILE")
	case ansibleVersion == "":
		glog.Fatal("Set the ANSIBLE_VERSION variable to the desired ansible git sha")
	case slackToken == "":
		glog.Fatal("Set the SLACK_WEBHOOK variable to desired webhook")
	case kubernetesServer == "":
		glog.Fatal("Set the KUBERNETES_SERVER variable to desired kubernetes server")
	case kubernetesToken == "":
		glog.Fatal("Set the KUBERNETES_TOKEN variable to desired kubernetes token")
	case apiKey == "":
		glog.Fatal("Set the DATADOG_API_KEY")
	case appKey == "":
		glog.Fatal("Set the DATADOG_APP_KEY")
	}

	if terminationWaitPeriodStr != "" {
		waitPeriod, err := strconv.ParseInt(terminationWaitPeriodStr, 10, 64)
		if err != nil {
			glog.Fatalf("Unable to parse TERMINATION_WAIT_PERIOD_SECONDS: %s", err)
		}
		terminationWaitPeriod = (time.Duration(waitPeriod) * time.Second)
	}

	if desiredCountStepStr != "" {
		var err error
		desiredCountStep, err = strconv.Atoi(desiredCountStepStr)
		if err != nil {
			glog.Fatalf("Unable to parse TERMINATION_BATCH_NODES_SIZE: %s", err)
		}
	}

	// Are we going to roll all of etcd, k8s-master and k8s-node or just
	// a subset.
	if rollerComponents != "" {
		targetComponents = strings.Split(rollerComponents, ",")
	} else {
		targetComponents = defaultComponents
	}

	awsClient := newAwsClient()
	params := &ec2.DescribeInstancesInput{}
	params.Filters = []*ec2.Filter{
		awsClient.ec2.newEC2Filter("tag:KubernetesCluster", kubernetesCluster),
		awsClient.ec2.newEC2Filter("instance-state-name", "running"),
	}
	inv, err := awsClient.ec2.describeInstancesNotMatchingAnsibleVersion(params, ansibleVersion)

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
		dd: newDataDogClient(apiKey, appKey),
	}

	// Set downtime in datadog for the cluster
	state.downtimeID, err = state.dd.startDownTime([]string{fmt.Sprintf("kubernetescluster:%s", kubernetesCluster)})
	if err != nil {
		glog.Errorf("an error occurred setting datadog downtime.\nError %s", err)
	}

	// Only manage the cluster autoscaler if rolling the k8s-node component.
	// If managing it fails, continue but consider the overall state failed.
	for _, component := range targetComponents {
		if component == "k8s-node" {
			disableClusterAutoscaler(state)
			disableClusterTerminator(state)
		}
	}

	state.SlackText = fmt.Sprintf("Starting a rolling update on cluster %s with the components %+v as the target components.\nAnsible version is set to %s\nManagement of cluster autoscaler is set to %t", kubernetesCluster, targetComponents, ansibleVersion, state.clusterAutoscaler.enabled)

	err = state.SlackPost()
	glog.V(4).Infof("Slack Post: %s", state.SlackText)
	if err != nil {
		glog.Errorf("an error occurred posting to slack.\nError %s", err)
	}

	var wg sync.WaitGroup
	var masterWg sync.WaitGroup

	// Roll through the master instances
	for _, component := range targetComponents {
		if component == "k8s-master" {
			masterWg.Add(1)
			go func(component string) {
				err := replaceInstancesTerminateAndVerify(awsClient, component, ansibleVersion, &masterWg)
				if err != nil {
					glog.Error(err)
				}
			}(component)
		}
	}

	// Roll through the node or etcd instances
	for _, component := range targetComponents {
		if component != "k8s-master" {
			wg.Add(1)
			go func(component string) {
				var err error
				// Batch replace k8s-worker nodes and replace one at a time for etcd components
				if component == "k8s-node" {
					glog.V(2).Info("Waiting for any masters to complete before continuing with nodes")
					masterWg.Wait()
					err = replaceInstancesVerifyAndTerminate(awsClient, component, ansibleVersion, &wg)
				} else {
					err = replaceInstancesTerminateAndVerify(awsClient, component, ansibleVersion, &wg)
				}
				if err != nil {
					glog.Error(err)
				}
			}(component)
		}
	}

	wg.Wait()
	masterWg.Wait()

	if state.clusterAutoscaler.enabled {
		enableClusterAutoscaler(state)
		enableClusterTerminator(state)
	}

	// End datadog downtime
	err = state.dd.endDownTime(state.downtimeID)
	if err != nil {
		glog.Errorf("An error occurred unsetting the datadog downtime.\nError %s", err)
	}

	err = state.Summary()
	if err != nil {
		glog.Errorf("an error occurred psting to slack.\nError %s", err)
	}
	glog.V(4).Infof("Slack Post: %s", state.SlackText)
}
