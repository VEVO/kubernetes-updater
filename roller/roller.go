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

type component struct {
	name      string
	start     time.Time
	status    bool
	instances []*ec2.Instance
}

type rollerState struct {
	components     []*component
	startTime      time.Time
	summaryMessage string
	inventory      []*ec2.Instance
	SlackText      string `json:"text"`
}

func newAWSCloudClient() *awsCloudClient {
	f := map[string]string{
		"tag:KubernetesCluster": kubernetesCluster,
		// We only want things that have completed an ansible run we will
		// filter out the instances by version later.
		"tag:version":         "*",
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
	b, err := json.Marshal(s.SlackText)
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
				tagValue == *tag.Value
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

func (c *awsCloudClient) terminateAndVerifyComponentInstances(component string) error {

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

	myComponent := &component{
		name:      c,
		start:     time.Now(),
		instances: i,
		asgs:      a}

	// Pause autoscaling activities
	for _, e := range myComponent.asgs {
		c.manageASGProceses(e, "suspend")
		if err != nil {
			fmt.Printf("An error occurred while suspending processes on %s\n", e)
			fmt.Printf("%s\n", err)
		}
	}

	// Defer resume autoscaling activities
	for _, e := range myComponent.asgs {
		defer c.manageASGProceses(e, "resume")
		if err != nil {
			fmt.Printf("An error occurred while resuming processes on %s\n", e)
			fmt.Printf("%s\n", err)
		}
	}
	// Defer unpause autoscaling activities
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
	state.SlackPost()

	var wg sync.WaitGroup
	for _, component := range targetComponents {
		wg.Add(1)
		go cloud.terminateAndVerifyComponentInstances(component)
	}

	wg.Wait()
	state.SlackText = "summary here"
}
