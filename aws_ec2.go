package main

import (
	"fmt"
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
	client  AwsEc2
	filters []*ec2.Filter
}

func newAWSEc2Client() AwsEc2 {
	return &AwsEc2Client{
		session: ec2.New(session.New()),
	}
}

func newAWSEc2Controller(awsEc2Client AwsEc2) *AwsEc2Controller {
	return &AwsEc2Controller{
		client: awsEc2Client,
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

func (c *AwsEc2Controller) DescribeInstances(request *ec2.DescribeInstancesInput) ([]*ec2.Instance, error) {
	// Instances are paged
	results := []*ec2.Instance{}
	var nextToken *string
	var err error

	// Set the request filters
	request.Filters, err = c.mergeFilters(request.Filters)
	if err != nil {
		glog.Fatalf("An error occurred describing the ec2 instances: %s", err)
	}

	for {
		response, err := c.client.DescribeInstances(request)

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

func (c *AwsEc2Controller) DescribeInstancesNotMatchingAnsibleVersion(request *ec2.DescribeInstancesInput, ansibleVersion string) ([]*ec2.Instance, error) {
	results, err := c.DescribeInstances(request)
	if err != nil {
		return nil, err
	}

	// Apparently negative filters do not work with AWS so here we filter
	// out the instances which do not match the desired ansible version
	results, err = c.InstancesNotMatchingTagValue("version", ansibleVersion, results)

	return results, err
}

func (c *AwsEc2Controller) getInstanceHealth(instance string) (string, error) {
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

	resp, err := c.client.DescribeTags(params)
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

func (c *AwsEc2Controller) mergeFilters(filters []*ec2.Filter) ([]*ec2.Filter, error) {
	for _, f := range c.filters {
		filters = append(filters, f)
	}

	if len(filters) == 0 {
		// We can't pass a zero-length Filters to AWS (it's an error)
		// So if we end up with no filters; return an error
		return filters, fmt.Errorf("Cannot pass zero-length filters to aws: %s", filters)
	}
	return filters, nil
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

func (c *AwsEc2Controller) terminateInstance(instance string) (*ec2.TerminateInstancesOutput, error) {
	var resp *ec2.TerminateInstancesOutput
	var err error

	glog.V(4).Infof("Terminating instance %s\n", instance)

	params := &ec2.TerminateInstancesInput{
		InstanceIds: []*string{
			aws.String(instance),
		},
		DryRun: aws.Bool(false),
	}
	resp, err = c.client.TerminateInstances(params)
	return resp, err
}

func (c *AwsEc2Controller) findReplacementInstances(myComponent *componentType, ansibleVersion string, count int, t time.Time) ([]string, error) {
	newInstances := make(map[string]struct{})
	var err error

	// Loop until we have new healthy replacements or time has expired
	for loop := 0; loop < 30; loop++ {
		glog.Infof("Checking for %d replacement %s instances - %s - loop %d\n", count, myComponent.name, timeStamp(), loop)

		var inv []*ec2.Instance

		params := &ec2.DescribeInstancesInput{}
		params.Filters = []*ec2.Filter{c.newEC2Filter("tag:ServiceComponent", myComponent.name)}

		inv, err = c.DescribeInstancesNotMatchingAnsibleVersion(params, ansibleVersion)
		if err != nil {
			glog.Fatalf("An error occurred getting the EC2 inventory: %s.\n", err)
		}

		var instanceList []string
		for _, e := range inv {
			instanceList = append(instanceList, *e.InstanceId)
		}

		for _, e := range inv {
			if e.LaunchTime.After(t) {
				// Using a map with empty values gives us a set and/or a unique slice
				newInstances[*e.InstanceId] = struct{}{}
			}
		}

		if len(newInstances) == count {
			break
		}

		time.Sleep(time.Second * 30)
	}

	// We want to return a slice here rather than a map with empty values
	var replacementInstances []string
	for instance := range newInstances {
		replacementInstances = append(replacementInstances, instance)
	}

	if len(replacementInstances) < count {
		glog.Infof("Exiting find with an error for component %s.\n", myComponent.name)
		return replacementInstances, fmt.Errorf("Found %d/%d replacement %s instances. Giving up!\n",
			len(replacementInstances), count, myComponent.name)
	}

	glog.V(4).Infof("Exiting find without an error for component %s.\n", myComponent.name)
	return replacementInstances, err
}

func (c *AwsEc2Controller) verifyReplacementInstances(myComponent *componentType, instances []string) ([]string, error) {
	var err error
	var status string

	for loop := 0; loop < 30; loop++ {
		for i := len(instances) - 1; i >= 0; i-- {
			instance := instances[i]
			status, err = c.getInstanceHealth(instance)
			if err != nil {
				return instances, err
			}
			glog.Infof("Component %s instance %s current status is %s - %s \n", myComponent.name, instance, status, timeStamp())
			if status == "True" {
				glog.Infof("Verification complete component %s instance %s is healthy\n", myComponent.name, instance)
				// Remove instance from the slice so we don't check it again
				instances = append(instances[:i], instances[i+1:]...)
				continue
			}
		}

		// If any instances are not yet healthy, keep checking
		if len(instances) > 0 {
			glog.Infof("Still waiting for the following %s instances to become healthy %s\n", myComponent.name, instances)
			time.Sleep(time.Second * 30)
			continue
		}
		break
	}

	if len(instances) > 0 {
		return instances, fmt.Errorf("Failed to verify %s instances %s", myComponent.name, instances)
	}

	glog.Infof("Verification complete component %s all instances are healthy\n", myComponent.name)
	return instances, nil
}
