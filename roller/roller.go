/*TODO
1.  If the serviceComponent is k8s-worker then we want to first provision more instances.   We should provision as many nodes as we are going to terminate.  Once the new nodes are online then set the old nodes to unschedulable in kubernetes and start terminating the nodes one at a time.
2.  Run the rolling updater as a kubernetes job.  This will require that the node running the pod get the instance-id of the node its running on and terminate that node last and bail out without waiting for the verification part to occur.
3.  Have serverspec post updates to an elasticcache endpoint so that we do not need to query datadog to get the nodes health status as this adds about 4 minutes per node.
*/
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
)

var (
	//Leaving the DD env vars as we may use this for other monitors
	apiKey            = os.Getenv("DATADOG_API_KEY")
	appKey            = os.Getenv("DATADOG_APP_KEY")
	cluster           = os.Getenv("CLUSTER")
	awsProfile        = os.Getenv("AWS_PROFILE")
	awsRegion         = os.Getenv("AWS_REGION")
	slackToken        = os.Getenv("SLACK_WEBHOOK")
	kubernetesCluster string
	verboseLogging    = os.Getenv("ROLLER_VERBOSE_MODE")
	rollerComponents  = os.Getenv("ROLLER_COMPONENTS")
	defaultComponents = []string{
		"k8s-node",
		"k8s-master",
		"etcd",
	}
)

type slackPost struct {
	Text string `json:"text"`
}

type inventory struct {
	Components map[string][]*ec2.Instance
	ASGs       []string
}

func verboseLog(l string) {
	if verboseLogging != "" {
		fmt.Println(l)
	}
}

func timeStamp() string {
	return time.Now().Format(time.RFC822)
}

func (s *slackPost) PostMessage() error {
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
	if err != nil {
		return err
	}
	return err
}

func (i *inventory) GetInventory() error {
	svc := ec2.New(session.New())

	params := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String("tag:KubernetesCluster"),
				Values: []*string{
					aws.String(strings.Join([]string{"*", kubernetesCluster, "*"}, "")),
				},
			},
			{
				Name: aws.String("instance-state-name"),
				Values: []*string{
					aws.String("running"),
				},
			},
		},
	}
	resp, err := svc.DescribeInstances(params)
	if err != nil {
		return err
	}

	err = i.SortByComponent(resp)
	return err
}

func (i *inventory) SortByComponent(r *ec2.DescribeInstancesOutput) error {
	var err error

	for idx := range r.Reservations {
		for _, inst := range r.Reservations[idx].Instances {
			var exists bool
			var c string
			var asg string
			for _, tag := range inst.Tags {
				if *tag.Key == "ServiceComponent" {
					c = *tag.Value
				}
				if *tag.Key == "aws:autoscaling:groupName" {
					asg = *tag.Value
				}
			}

			i.Components[c] = append(i.Components[c], inst)
			for _, seen := range i.ASGs {
				if seen == asg {
					exists = true
					break
				}
			}
			if !exists {
				i.ASGs = append(i.ASGs, asg)
			}
		}
	}
	return err
}

func newInventory() *inventory {
	return &inventory{Components: make(map[string][]*ec2.Instance)}
}

func getHostStatus(host string) (string, error) {
	status := "Unset"

	sess, err := session.NewSession()
	if err != nil {
		log.Fatalf("failed to create session %v\n", err)
	}

	svc := ec2.New(sess)

	params := &ec2.DescribeTagsInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String("tag:healthy"),
				Values: []*string{
					aws.String("*"),
				},
			},
			{
				Name: aws.String("resource-id"),
				Values: []*string{
					aws.String(host),
				},
			},
		},
	}
	resp, err := svc.DescribeTags(params)

	if err != nil {
		return status, err
	}

	for _, tag := range resp.Tags {
		if *tag.Key == "healthy" {
			status = *tag.Value
		}
	}
	return status, err
}

func manageASGProceses(asg string, action string) (string, error) {
	var err error
	var resp string

	sess, err := session.NewSession()
	if err != nil {
		return resp, err
	}

	svc := autoscaling.New(sess)

	params := &autoscaling.ScalingProcessQuery{
		AutoScalingGroupName: aws.String(asg),
		ScalingProcesses: []*string{
			// If we suspend termination we will never get a new node provisioned
			//			aws.String("Terminate"),
			aws.String("AZRebalance"),
		},
	}

	if action == "suspend" {
		var r *autoscaling.SuspendProcessesOutput
		r, err = svc.SuspendProcesses(params)
		resp = r.String()
	} else {
		var r *autoscaling.ResumeProcessesOutput
		r, err = svc.ResumeProcesses(params)
		resp = r.String()
	}

	return resp, err
}

func terminateEC2Instance(i string) (*ec2.TerminateInstancesOutput, error) {
	var resp *ec2.TerminateInstancesOutput

	verboseLog(fmt.Sprintf("Terminating instance %s\n", i))
	sess, err := session.NewSession()
	if err != nil {
		return resp, err
	}

	svc := ec2.New(sess)

	params := &ec2.TerminateInstancesInput{
		InstanceIds: []*string{
			aws.String(i),
		},
		DryRun: aws.Bool(false),
	}
	resp, err = svc.TerminateInstances(params)
	return resp, err
}

func verifyReplacement(c string, t time.Time) error {
	var newInstance string
	var errString string
	var err error
	// Check for our new instance to get provisioned - loop for 15 minutes
	for loop := 0; loop < 30; loop++ {
		fmt.Printf("Checking AWS for replacement instances - %s\n", timeStamp())
		currentInventory := newInventory()
		err = currentInventory.GetInventory()
		if err != nil {
			return err
		}
		verboseLog(fmt.Sprintf("The current components: %+v\n", currentInventory.Components))
		for _, e := range currentInventory.Components[c] {
			if e.LaunchTime.After(t) {
				newInstance = *e.InstanceId
				fmt.Printf("Found our replacement instance %s\n", newInstance)
			}
		}
		if newInstance != "" {
			break
		}
		time.Sleep(time.Second * 30)
	}

	if newInstance == "" {
		errString = fmt.Sprintf("Could not find a replacement %s instance.  Giving up!\n", c)
		return errors.New(errString)
	}

	var status string
	// Until we get onto docker 1.12 we need a long timeout for the etcd
	// nodes because they are hitting a bug https://vevowiki.atlassian.net/browse/SE-958
	for loop := 0; loop < 45; loop++ {
		status, err = getHostStatus(newInstance)
		fmt.Printf("Instance %s current status is %s - %s \n", newInstance, status, timeStamp())
		if err != nil {
			return err
		}
		if status == "True" {
			fmt.Printf("Verification complete instance %s is healthy\n", newInstance)
			return err
		}
		time.Sleep(time.Second * 60)
	}

	errString = fmt.Sprintf("The replacement instance %s has a healthy state of %s so we are stopping the rolling update for component %s\n", newInstance, status, c)
	return errors.New(errString)
}

func terminateComponentInstances(component string, nodes []*ec2.Instance, wg *sync.WaitGroup) error {
	var err error
	var loop int

	defer wg.Done()

	fmt.Printf("Terminating %s components\n", component)

	for _, n := range nodes {
		loop++
		//Useful for testing
		//		if loop > 1 {
		//			return err
		//		}

		fmt.Printf("Terminating instance %s in component %s\n", *n.InstanceId, component)
		terminateEC2Instance(*n.InstanceId)

		curTime := time.Now()
		for s := 0; s <= 4; s++ {
			fmt.Printf("Sleeping while we wait for the new %s instance to terminate and the new one to come online - %s\n", component, timeStamp())
			time.Sleep(time.Second * 30)
		}

		err = verifyReplacement(component, curTime)
		if err != nil {
			fmt.Println(err)
			return err
		}

	}

	fmt.Printf("Completed termination of %s components\n", component)
	return err
}

func init() {
	// Force the use of ~/.aws/config
	_ = os.Setenv("AWS_SDK_LOAD_CONFIG", "true")

	if cluster == "" {
		log.Fatal("Please specify an env var CLUSTER that contains the name of the target kubernetes cluster")
	}

	if awsRegion == "" {
		log.Fatal("Please specify an env var AWS_REGION that contains the name of the desired AWS region")
	}

	if awsProfile == "" {
		log.Fatal("Please specify an env var AWS_PROFILE that contains the name of the desired AWS environemnt")
	}

	if apiKey == "" {
		log.Fatal("Please specify an env var DATADOG_API_KEY that contains the datadog api key to use")
	}

	if appKey == "" {
		log.Fatal("Please specify an env var DATADOG_APP_KEY that contains the datadog app key to use")
	}

	if slackToken == "" {
		log.Fatal("Please specify an env var SLACK_WEBHOOK that contains the slack webhook URL")
	}
}

func main() {
	var slack slackPost

	kubernetesCluster = fmt.Sprintf("%s-%s-%s", awsProfile, awsRegion, cluster)
	verboseLog(fmt.Sprintf("Kubernetes cluster is set to %s\n", kubernetesCluster))
	verboseLog(fmt.Sprintf("AWS_PROFILE is set to %s\n", awsProfile))

	// Get a current inventory broken down into an array for each component (k8s-node,k8s-master and etcd) as well a unique list of autoscalinggroups assocaited with the nodes
	initialInventory := newInventory()
	err := initialInventory.GetInventory()
	if err != nil {
		log.Fatalf("An error occurred getting the list of instances: %s\n", err.Error())
	}

	verboseLog(fmt.Sprintf("The starting components: %+v\n", initialInventory.Components))
	verboseLog(fmt.Sprintf("The list of ASG's: %+q\n", initialInventory.ASGs))

	fmt.Printf("Suspending rebalance process on ASGs: %v\n", initialInventory.ASGs)
	for _, e := range initialInventory.ASGs {
		manageASGProceses(e, "suspend")
		if err != nil {
			fmt.Printf("An error occurred while suspending processes on %s\n", e)
			fmt.Printf("%s\n", err)
		}
	}

	var targetComponents []string
	// Figure out if we are rolling all components or just a subset
	if rollerComponents != "" {
		targetComponents = strings.Split(rollerComponents, ",")
	} else {
		targetComponents = defaultComponents
	}

	slack.Text = fmt.Sprintf("Slack Integration - Starting a rolling update on cluster %s with the components %+v as the target components.", kubernetesCluster, targetComponents)
	slack.PostMessage()
	verboseLog(fmt.Sprintf("Target components are set to %v\n", targetComponents))

	// Iterate over the different groups of components performing terminations and verifications of each group concurrently
	var wg sync.WaitGroup
	for k, v := range initialInventory.Components {
		for _, c := range targetComponents {
			if c == k {
				wg.Add(1)
				go terminateComponentInstances(k, v, &wg)
				break
			}
		}
	}

	wg.Wait()

	slack.Text = fmt.Sprintf("Completed a rolling update on cluster %s with the components %+v as the target components.", kubernetesCluster, targetComponents)
	slack.PostMessage()

	fmt.Printf("Resuming rebalance process on ASGs: %v\n", initialInventory.ASGs)
	for _, e := range initialInventory.ASGs {
		manageASGProceses(e, "resume")
		if err != nil {
			fmt.Printf("An error occurred while resuming processes on %s\n", e)
			fmt.Printf("%s\n", err)
		}
	}
}
