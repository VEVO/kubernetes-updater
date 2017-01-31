package main

import (
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/glog"
)

type AwsEc2 interface {
	DescribeInstances(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error)
	DescribeTags(*ec2.DescribeTagsInput) (*ec2.DescribeTagsOutput, error)
	TerminateInstances(*ec2.TerminateInstancesInput) (*ec2.TerminateInstancesOutput, error)
}

type AwsEc2Client struct {
	session *ec2.EC2
}

type AwsEc2Controller struct {
	filters []*ec2.Filter
}

func newAWSEc2Client() *AwsEc2Client {
	return &AwsEc2Client{
		session: ec2.New(session.New()),
	}
}

func (e AwsEc2Client) DescribeInstances(input *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
	return e.session.DescribeInstances(input)
}

func (e AwsEc2Client) DescribeTags(input *ec2.DescribeTagsInput) (*ec2.DescribeTagsOutput, error) {
	return e.session.DescribeTags(input)
}

func (e AwsEc2Client) TerminateInstances(input *ec2.TerminateInstancesInput) (*ec2.TerminateInstancesOutput, error) {
	return e.session.TerminateInstances(input)
}

func (c *AwsEc2Controller) DescribeInstances(ec2Client AwsEc2, request *ec2.DescribeInstancesInput) ([]*ec2.Instance, error) {
	// Instances are paged
	results := []*ec2.Instance{}
	var nextToken *string
	var err error

	// Set the request filters
	request.Filters, err = c.updateFilters(request.Filters)
	if err != nil {
		glog.Fatalf("An error occurred describing the ec2 instances: %s", err)
	}

	for {
		response, err := ec2Client.DescribeInstances(request)

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
	results, err = c.InstancesNotMatchingTagValue("version", ansibleVersion, results)

	return results, err
}
func (c *AwsEc2Controller) DescribeInstancesNotMatchingAnsibleVersion(ec2Client AwsEc2, request *ec2.DescribeInstancesInput, ansibleVersion string) ([]*ec2.Instance, error) {
	results, err := c.DescribeInstances(ec2Client, request)
	if err != nil {
		return nil, err
	}

	// Apparently negative filters do not work with AWS so here we filter
	// out the instances which do not match the desired ansible version
	results, err = c.InstancesNotMatchingTagValue("version", ansibleVersion, results)

	return results, err
}

func (c *AwsEc2Controller) getInstanceHealth(ec2Client AwsEc2, instance string) (string, error) {
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

	resp, err := ec2Client.DescribeTags(params)
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

func (c *AwsEc2Controller) InstancesMatchingTagValue(tagName, tagValue string, instances []*ec2.Instance) ([]*ec2.Instance, error) {
	return c.FiltersInstancesByTagValue(tagName, tagValue, false, instances)
}

func (c *AwsEc2Controller) InstancesNotMatchingTagValue(tagName, tagValue string, instances []*ec2.Instance) ([]*ec2.Instance, error) {
	return c.FiltersInstancesByTagValue(tagName, tagValue, true, instances)
}

func (c *AwsEc2Controller) FiltersInstancesByTagValue(tagName, tagValue string, inverse bool, instances []*ec2.Instance) ([]*ec2.Instance, error) {
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

func (c *AwsEc2Controller) GetUniqueTagValues(tagName string, instances []*ec2.Instance) ([]string, error) {
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

func (c *AwsEc2Controller) updateFilters(filters []*ec2.Filter) ([]*ec2.Filter, error) {
	for _, f := range filters {
		c.filters = append(c.filters, f)
	}

	if len(c.filters) == 0 {
		// We can't pass a zero-length Filters to AWS (it's an error)
		// So if we end up with no filters; return an error
		return filters, fmt.Errorf("Cannot pass zero-length filters to aws: %s", filters)
	}
	return c.filters, nil
}

func (c *AwsEc2Controller) newEC2Filter(name string, value string) *ec2.Filter {
	filter := &ec2.Filter{
		Name: aws.String(name),
		Values: []*string{
			aws.String(value),
		},
	}
	return filter
}

func (c *AwsEc2Controller) terminateInstance(ec2Client AwsEc2, instance string) (*ec2.TerminateInstancesOutput, error) {
	var resp *ec2.TerminateInstancesOutput
	var err error

	glog.V(4).Infof("Terminating instance %s\n", instance)

	params := &ec2.TerminateInstancesInput{
		InstanceIds: []*string{
			aws.String(instance),
		},
		DryRun: aws.Bool(false),
	}
	resp, err = ec2Client.TerminateInstances(params)
	return resp, err
}

func (c *AwsEc2Controller) findReplacementInstance(ec2Client AwsEc2, component string, ansibleVersion string, t time.Time) (string, error) {
	var newInstance string
	var err error
	var inv []*ec2.Instance

	// Loop until we have a new healthy replacement or time has expired
	for loop := 0; loop < 30; loop++ {
		glog.Infof("Checking for replacement %s instance - %s - loop %d\n", component, timeStamp(), loop)

		params := &ec2.DescribeInstancesInput{}
		params.Filters = []*ec2.Filter{c.newEC2Filter("tag:ServiceComponent", component)}
		inv, err = c.DescribeInstancesNotMatchingAnsibleVersion(ec2Client, params, ansibleVersion)
		if err != nil {
			glog.Fatalf("An error occurred getting the EC2 inventory: %s.\n", err)
		}

		var instanceList []string
		for _, e := range inv {
			instanceList = append(instanceList, *e.InstanceId)
		}

		for _, e := range inv {
			if e.LaunchTime.After(t) {
				newInstance = *e.InstanceId
			}
		}

		if newInstance != "" {
			break
		}
		time.Sleep(time.Second * 30)
	}

	if newInstance == "" {
		glog.Infof("Exiting find with an error for component %s.\n", component)
		return newInstance, fmt.Errorf("Could not find a replacement %s instance.  Giving up!\n", component)
	}
	glog.V(4).Infof("Exiting find without an error for component %s.\n", component)
	return newInstance, err
}

func (c *AwsEc2Controller) verifyReplacementInstance(ec2Client AwsEc2, component string, instance string) error {
	var err error
	var status string

	for loop := 0; loop < 30; loop++ {
		status, err = c.getInstanceHealth(ec2Client, instance)
		glog.Infof("Component %s instance %s current status is %s - %s \n", component, instance, status, timeStamp())
		if err != nil {
			return err
		}

		if status == "True" {
			glog.Infof("Verification complete component %s instance %s is healthy\n", component, instance)
			return err
		}
		time.Sleep(time.Second * 60)
	}
	return fmt.Errorf("Timed out waiting for component %s instance %s to be healthy", component, instance)
}

func (c *AwsEc2Controller) terminateAndVerifyComponentInstances(ec2Client AwsEc2, awsAutoscaling AwsAutoscaling, component string, ansibleVersion string, wg *sync.WaitGroup) error {
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

	if component == "etcd" {
		var e []*ec2.Instance
		e, err = c.InstancesMatchingTagValue("healthy", "True", myComponent.instances)
		if err != nil {
			return err
		}
		var ee []*string
		var ii []*string
		for _, r := range e {
			ee = append(ee, r.InstanceId)
		}
		for _, q := range e {
			ii = append(ii, q.InstanceId)
		}

		if len(e) != len(i) {
			myComponent.err = fmt.Errorf("Etcd components are not healthy.  Please fix and run again")
			glog.V(4).Infof("%s", myComponent.err)
			return myComponent.err
		}
	}

	var instanceList []string
	for _, e := range myComponent.instances {
		instanceList = append(instanceList, *e.InstanceId)
	}
	glog.V(4).Infof("Component %s has starting instance Ids %v\n", component, instanceList)

	// Pause autoscaling activities
	awsAutoscalingController := AwsAutoscalingController{}
	scalingProcesses := []*string{
		aws.String("AZRebalance"),
		aws.String("Terminate"),
	}
	for _, e := range myComponent.asgs {
		_, err = awsAutoscalingController.manageASGProcesses(awsAutoscaling, e, scalingProcesses, "suspend")
		if err != nil {
			return fmt.Errorf("An error occurred while suspending processes on %s\n Error: %s\n", e, err)
		}
	}

	// Defer resume autoscaling activities
	for _, e := range myComponent.asgs {
		defer awsAutoscalingController.manageASGProcesses(awsAutoscaling, e, scalingProcesses, "resume")

		if err != nil {
			return fmt.Errorf("An error occurred while resuming processes on %s\n Error: %s\n", e, err)
		}
	}

	glog.V(4).Infof("Starting instance termination verify loop for component %s", myComponent.name)
	for _, n := range myComponent.instances {
		terminateTime := time.Now()
		r, err := c.terminateInstance(ec2Client, *n.InstanceId)
		if err != nil {
			err = fmt.Errorf("An error occurred while terminating %s instance %s\n Error: %s\n Response: %s\n", myComponent.name, *n.InstanceId, err, r)
			glog.V(4).Infof("%s", err)
			return err
		}

		newInstance, err := c.findReplacementInstance(ec2Client, component, ansibleVersion, terminateTime)
		if err != nil {
			err = fmt.Errorf("An error occurred finding the replacement instance for component %s\n Error: %s\n", component, err)
			glog.V(4).Infof("%s", err)
			return err
		}

		err = c.verifyReplacementInstance(ec2Client, component, newInstance)
		if err != nil {
			err = fmt.Errorf("An error occurred verifying the health of instance %s\n Error: %s\n", newInstance, err)
			glog.V(4).Infof("%s", err)
			return err
		}
	}

	myComponent.status = true
	myComponent.finish = time.Now()

	glog.V(4).Infof("Completed normal instance termination verify loop for component %s", myComponent.name)
	return nil
}
