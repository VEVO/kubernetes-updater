package main

import (
	"testing"

	"github.com/aws/aws-sdk-go/service/ec2"
)

type FakeAwsEc2Client struct{}

func newFakeAWSEc2Client() AwsEc2 {
	return &FakeAwsEc2Client{}
}

func fakeEc2Instance() *ec2.Instance {
	instanceId := "blah"
	version := "version"
	return &ec2.Instance{
		InstanceId: &instanceId,
		Tags: []*ec2.Tag{
			&ec2.Tag{
				Key:   &version,
				Value: &instanceId,
			},
		},
	}
}

func (e FakeAwsEc2Client) DescribeInstances(input *ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
	reservation := &ec2.Reservation{
		Instances: []*ec2.Instance{
			fakeEc2Instance(),
		},
	}
	describeInstancesOutput := &ec2.DescribeInstancesOutput{
		Reservations: []*ec2.Reservation{
			reservation,
		},
	}
	return describeInstancesOutput, nil
}

func (e FakeAwsEc2Client) DescribeTags(input *ec2.DescribeTagsInput) (*ec2.DescribeTagsOutput, error) {
	return &ec2.DescribeTagsOutput{}, nil
}

func (e FakeAwsEc2Client) TerminateInstances(input *ec2.TerminateInstancesInput) (*ec2.TerminateInstancesOutput, error) {
	return &ec2.TerminateInstancesOutput{}, nil
}

func TestAwsEc2Client_DescribeInstances(t *testing.T) {
	ec2Controller := newAWSEc2Controller(newFakeAWSEc2Client())
	params := &ec2.DescribeInstancesInput{}
	params.Filters = []*ec2.Filter{
		ec2Controller.newEC2Filter("instance-state-name", "running"),
	}
	instancesOutput, _ := ec2Controller.DescribeInstances(params)

	if len(instancesOutput) < 1 {
		t.Error("Could not describe instances")
	}
}
