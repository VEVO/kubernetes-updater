package main

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
)

var fakeDescribeAutoScalingGroupsOutput = &autoscaling.DescribeAutoScalingGroupsOutput{}

type FakeAwsAutoscalingClient struct{}

func newFakeAWSAutoscalingClient() AwsAutoscaling {
	return &FakeAwsAutoscalingClient{}
}

func (autoScalingClient *FakeAwsAutoscalingClient) SuspendProcesses(params *autoscaling.ScalingProcessQuery) (string, error) {
	return "{}", nil
}

func (autoScalingClient *FakeAwsAutoscalingClient) ResumeProcesses(params *autoscaling.ScalingProcessQuery) (string, error) {
	return "{}", nil
}

func (autoScalingClient *FakeAwsAutoscalingClient) SetDesiredCount(input *autoscaling.SetDesiredCapacityInput) (string, error) {
	return "{}", nil
}

func (autoScalingClient *FakeAwsAutoscalingClient) GetDesiredCount(autoscalingInstanceInput *autoscaling.DescribeAutoScalingGroupsInput) (*autoscaling.DescribeAutoScalingGroupsOutput, error) {
	return fakeDescribeAutoScalingGroupsOutput, nil
}

func TestAwsManageASGProcessesSuspend(t *testing.T) {
	awsAutoscalingController := newAWSAutoscalingController(newFakeAWSAutoscalingClient())
	scalingProcesses := []*string{
		aws.String("AZRebalance"),
	}
	_, err := awsAutoscalingController.manageASGProcesses("infra-k8s-worker", scalingProcesses, "suspend")
	if err != nil {
		t.Error("got error when attempting to suspend an ASG")
	}
}

func TestAwsManageASGProcessesResume(t *testing.T) {
	awsAutoscalingController := newAWSAutoscalingController(newFakeAWSAutoscalingClient())
	scalingProcesses := []*string{
		aws.String("AZRebalance"),
	}
	_, err := awsAutoscalingController.manageASGProcesses("infra-k8s-worker", scalingProcesses, "resume")
	if err != nil {
		t.Error("got error when attempting to suspend an ASG")
	}
}

func TestAwsSetDesiredCount(t *testing.T) {
	awsAutoscalingController := newAWSAutoscalingController(newFakeAWSAutoscalingClient())
	_, err := awsAutoscalingController.setDesiredCount("infra-k8s-worker", 4)
	if err != nil {
		t.Error("got error when attempting to set disired capacity for an ASG")
	}
}

func TestAwsGetDesiredCount(t *testing.T) {
	awsAutoscalingController := newAWSAutoscalingController(newFakeAWSAutoscalingClient())
	asgName := "infra-k8s-worker"
	asgCount := int64(3)
	fakeAutoscalingGroup := autoscaling.Group{
		AutoScalingGroupName: &asgName,
		DesiredCapacity:      &asgCount,
	}
	fakeAutoscalingGroupPointer := &fakeAutoscalingGroup
	fakeDescribeAutoScalingGroupsOutput = &autoscaling.DescribeAutoScalingGroupsOutput{
		AutoScalingGroups: []*autoscaling.Group{
			fakeAutoscalingGroupPointer,
		},
	}
	count, err := awsAutoscalingController.getDesiredCount(asgName)
	if err != nil {
		t.Errorf("got error when attempting to get disired capacity for an ASG: %s", err)
	}
	if count != 3 {
		t.Errorf("got wrong count when attempting to get disired capacity for an ASG: expected 3, got %d", count)
	}
}
