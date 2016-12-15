package main

import (
	"bytes"
	"encoding/json"
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
	cluster           = os.Getenv("CLUSTER")
	awsProfile        = os.Getenv("AWS_PROFILE")
	awsRegion         = os.Getenv("AWS_REGION")
	slackToken        = os.Getenv("SLACK_WEBHOOK")
	rollerComponents  = os.Getenv("ROLLER_COMPONENTS")
	ansibleVersion    = os.Getenv("ANSIBLE_VERSION")
	cloud             *awsCloudClient
	state             *rollerState
	kubernetesCluster string
	targetComponents  []string
	defaultComponents = []string{
		"k8s-node",
		"k8s-master",
		"etcd",
	}
	verboseLogging = "true"
)

type awsCloud interface {
	EC2() *ec2.EC2
	Autoscaling() *autoscaling.AutoScaling
}

type awsCloudClient struct {
	ec2         *ec2.EC2
	autoscaling *autoscaling.AutoScaling
	filterTags  map[string]string
}

type componentType struct {
	name      string
	start     time.Time
	finish    time.Time
	status    bool
	instances []*ec2.Instance
	asgs      []string
}

type rollerState struct {
	components     []*componentType
	startTime      time.Time
	summaryMessage string
	inventory      []*ec2.Instance
	SlackText      string `json:"text"`
}

func timeStamp() string {
	return time.Now().Format(time.RFC822)
}

func verboseLog(l string) {
	if verboseLogging != "" {
		fmt.Println(l)
	}
}

func newAWSCloudClient() *awsCloudClient {
	f := map[string]string{
		"tag:KubernetesCluster": kubernetesCluster,
		// We only want things that have completed an ansible run we will
		// filter out the instances by version later.
		//"tag:version":         "*",
		"instance-state-name": "running",
	}

	return &awsCloudClient{
		ec2:         ec2.New(session.New()),
		autoscaling: autoscaling.New(session.New()),
		filterTags:  f,
	}
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
		if c.status != true {
			status = "failure"
			break
		}
	}

	duration := time.Since(s.startTime)
	summary = fmt.Sprintf("Finished a rolling update on cluster %s with the components %+v as the target components.\nOverall status: %s\nOverall duration: %v\n", kubernetesCluster, rollerComponents, status, duration-(duration%time.Minute))

	for _, c := range s.components {
		var status string
		duration := time.Since(c.start)
		if c.status {
			status = "success"
		} else {
			status = "failure"
		}

		cs := fmt.Sprintf("Component %s status: %s - duration: %v\n", c.name, status, duration-(duration%time.Minute))
		summary = summary + cs
	}

	s.SlackText = summary
	err := s.SlackPost()
	return err
}

func (c *awsCloudClient) EC2() *ec2.EC2 {
	return c.ec2
}

func (c *awsCloudClient) Autoscaling() *autoscaling.AutoScaling {
	return c.autoscaling
}

func (c *awsCloudClient) DescribeInstances(request *ec2.DescribeInstancesInput) ([]*ec2.Instance, error) {
	// Instances are paged
	results := []*ec2.Instance{}
	var nextToken *string

	// Merge the default and request filters
	request.Filters = c.addFilters(request.Filters)

	for {
		response, err := c.ec2.DescribeInstances(request)
		if err != nil {
			return nil, fmt.Errorf("error listing AWS instances: %v", err)
		}

		for _, reservation := range response.Reservations {
			results = append(results, reservation.Instances...)
		}

		nextToken = response.NextToken
		if aws.StringValue(nextToken) == "" {
			break
		}
		request.NextToken = nextToken
	}

	// Apparently negative filters do not work with AWS so here we filter
	// out the instances which do not match the desired ansible version
	results, err := c.InstancesNotMatchingTagValue("version", ansibleVersion, results)
	return results, err
}

func (c *awsCloudClient) getInstanceHealth(instance string) (string, error) {
	status := "Unset"
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
					aws.String(instance),
				},
			},
		},
	}

	resp, err := c.ec2.DescribeTags(params)
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

func (c *awsCloudClient) InstancesMatchingTagValue(tagName, tagValue string, instances []*ec2.Instance) ([]*ec2.Instance, error) {

	return c.FiltersInstancesByTagValue(tagName, tagValue, false, instances)
}

func (c *awsCloudClient) InstancesNotMatchingTagValue(tagName, tagValue string, instances []*ec2.Instance) ([]*ec2.Instance, error) {
	return c.FiltersInstancesByTagValue(tagName, tagValue, true, instances)
}

func (c *awsCloudClient) FiltersInstancesByTagValue(tagName, tagValue string, inverse bool, instances []*ec2.Instance) ([]*ec2.Instance, error) {
	results := []*ec2.Instance{}
	for _, instance := range instances {
		var tagMatch bool

		for _, tag := range instance.Tags {
			if *tag.Key == tagName {
				if *tag.Value == tagValue {
					tagMatch = true
				}
				break
			}
		}
		if tagMatch && !inverse {
			results = append(results, instance)
		} else if inverse && !tagMatch {
			results = append(results, instance)
		}
	}
	return results, nil
}

func (c *awsCloudClient) GetUniqueTagValues(tagName string, instances []*ec2.Instance) ([]string, error) {
	var results []string

	for _, instance := range instances {
		var tagValue string
		var exists bool

		for _, tag := range instance.Tags {
			if *tag.Key == tagName {
				tagValue = *tag.Value
				break
			}
		}

		for _, seen := range results {
			if seen == tagValue {
				exists = true
				break
			}
		}

		if !exists {
			results = append(results, tagValue)
		}

	}
	return results, nil
}

func (c *awsCloudClient) addFilters(filters []*ec2.Filter) []*ec2.Filter {
	for k, v := range c.filterTags {
		filters = append(filters, newEC2Filter(k, v))
	}
	if len(filters) == 0 {
		// We can't pass a zero-length Filters to AWS (it's an error)
		// So if we end up with no filters; just return nil
		return nil
	}

	return filters
}

func newEC2Filter(name string, value string) *ec2.Filter {
	filter := &ec2.Filter{
		Name: aws.String(name),
		Values: []*string{
			aws.String(value),
		},
	}
	return filter
}

func (c *awsCloudClient) terminateInstance(instance string) (*ec2.TerminateInstancesOutput, error) {
	var resp *ec2.TerminateInstancesOutput
	var err error

	verboseLog(fmt.Sprintf("Terminating instance %s\n", instance))

	params := &ec2.TerminateInstancesInput{
		InstanceIds: []*string{
			aws.String(instance),
		},
		DryRun: aws.Bool(false),
	}
	resp, err = c.ec2.TerminateInstances(params)
	return resp, err
}

func (c *awsCloudClient) findReplacementInstance(component string, t time.Time) (string, error) {
	var newInstance string
	var err error

	// Loop until we have a new healthy replacemen or time has expired
	for loop := 0; loop < 30; loop++ {
		fmt.Printf("Checking for replacement %s instance - %s\n", component, timeStamp())

		params := &ec2.DescribeInstancesInput{}
		params.Filters = []*ec2.Filter{newEC2Filter("tag:ServiceComponent", component)}
		inv, err := c.DescribeInstances(params)
		if err != nil {
			log.Fatalf("An error occurred getting the EC2 inventory: %s.\n", err)
		}

		for _, e := range inv {
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
		return newInstance, fmt.Errorf("Could not find a replacement %s instance.  Giving up!\n", component)
	}
	return newInstance, err
}

func (c *awsCloudClient) verifyReplacementInstance(instance string) error {
	var err error
	var status string

	for loop := 0; loop < 30; loop++ {
		status, err = c.getInstanceHealth(instance)
		fmt.Printf("Instance %s current status is %s - %s \n", instance, status, timeStamp())
		if err != nil {
			return err
		}

		if status == "True" {
			fmt.Printf("Verification complete instance %s is healthy\n", instance)
			return err
		}
		time.Sleep(time.Second * 60)
	}
	return fmt.Errorf("Timed out waiting for instance %s to be healthy", instance)
}

func (c *awsCloudClient) terminateAndVerifyComponentInstances(component string, wg *sync.WaitGroup) error {
	defer wg.Done()
	// Get list of instances by filter on tag ServiceComponent == component
	//	params.Filters = append(params.Filters, newEC2Filter("tag:ServiceComponent", "k8s-master"))
	i, err := c.InstancesMatchingTagValue("ServiceComponent", component, state.inventory)
	if err != nil {
		return err
	}

	a, err := c.GetUniqueTagValues("aws:autoscaling:groupName", i)
	if err != nil {
		return err
	}

	myComponent := &componentType{
		name:      component,
		start:     time.Now(),
		instances: i,
		asgs:      a}

	state.components = append(state.components, myComponent)

	// Pause autoscaling activities
	for _, e := range myComponent.asgs {
		c.manageASGProceses(e, "suspend")
		if err != nil {
			return fmt.Errorf("An error occurred while suspending processes on %s\n Error: %s\n", e, err)
		}
	}

	// Defer resume autoscaling activities
	for _, e := range myComponent.asgs {
		defer c.manageASGProceses(e, "resume")
		if err != nil {
			return fmt.Errorf("An error occurred while resuming processes on %s\n Error: %s\n", e, err)
		}
	}

	for _, n := range myComponent.instances {
		r, err := c.terminateInstance(*n.InstanceId)
		if err != nil {
			return fmt.Errorf("An error occurred while terminating instance %s\n Error: %s\n Response: %s\n", *n.InstanceId, err, r)
		}

		newInstance, err := c.findReplacementInstance(component, time.Now())
		if err != nil {
			return fmt.Errorf("An error occurred finding the replacement instance for component %s\n Error: %s\n", component, err)
		}

		err = c.verifyReplacementInstance(newInstance)
		if err != nil {
			return fmt.Errorf("An error occurred verifying the health of instance %s\n Error: %s\n", newInstance, err)
		}
	}

	myComponent.status = true
	myComponent.finish = time.Now()

	return nil
}

func (c *awsCloudClient) manageASGProceses(asg string, action string) (string, error) {
	var err error
	var resp string

	params := &autoscaling.ScalingProcessQuery{
		AutoScalingGroupName: aws.String(asg),
		ScalingProcesses: []*string{
			aws.String("AZRebalance"),
		},
	}

	if action == "suspend" {
		var r *autoscaling.SuspendProcessesOutput
		r, err = c.autoscaling.SuspendProcesses(params)
		resp = r.String()
	} else {
		var r *autoscaling.ResumeProcessesOutput
		r, err = c.autoscaling.ResumeProcesses(params)
		resp = r.String()
	}

	return resp, err
}

func init() {
	// Force the use of ~/.aws/config
	_ = os.Setenv("AWS_SDK_LOAD_CONFIG", "true")

	// for testing
	_ = os.Setenv("AWS_PROFILE", "dev")
	_ = os.Setenv("AWS_REGION", "us-east-1")
	_ = os.Setenv("CLUSTER", "infra")
	_ = os.Setenv("ANSIBLE_VERSION", "EXAMPLE_VERSION")

	if cluster == "" {
		log.Fatal("Please specify an env var CLUSTER that contains the name of the target kubernetes cluster")
	}

	if awsRegion == "" {
		log.Fatal("Please specify an env var AWS_REGION that contains the name of the desired AWS region")
	}

	if awsProfile == "" {
		log.Fatal("Please specify an env var AWS_PROFILE that contains the name of the desired AWS environemnt")
	}

	if ansibleVersion == "" {
		log.Fatal("Please specify an env var ANSIBLE_VERSION that contains desired ansible git sha")
	}

	kubernetesCluster = fmt.Sprintf("%s-%s-%s", awsProfile, awsRegion, cluster)
}

func main() {
	cloud = newAWSCloudClient()
	params := &ec2.DescribeInstancesInput{}
	inv, err := cloud.DescribeInstances(params)

	if err != nil {
		log.Fatalf("An error occurred getting the EC2 inventory: %s.\n", err)
	}

	state = &rollerState{startTime: time.Now(),
		inventory: inv}

	// Are we going to roll all of etcd, k8s-master and k8s-node or just
	// a subset.
	if rollerComponents != "" {
		targetComponents = strings.Split(rollerComponents, ",")
	} else {
		targetComponents = defaultComponents
	}

	state.SlackText = fmt.Sprintf("Starting a rolling update on cluster %s with the components %+v as the target components.", kubernetesCluster, targetComponents)

	err = state.SlackPost()
	if err != nil {
		fmt.Printf("An error occurred psting to slack.\nError %s\n", err)
	}

	var wg sync.WaitGroup
	for _, component := range targetComponents {
		wg.Add(1)
		go cloud.terminateAndVerifyComponentInstances(component, &wg)
	}

	wg.Wait()

	err = state.Summary()
	if err != nil {
		fmt.Printf("An error occurred psting to slack.\nError %s\n", err)
	}
}
