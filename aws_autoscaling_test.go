package main

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
)

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

func TestAwsManageASGProcesesSuspend(t *testing.T) {
	awsAutoscalingclient := newFakeAWSAutoscalingClient()
	awsAutoscalingController := &AwsAutoscalingController{}
	scalingProcesses := []*string{
		aws.String("AZRebalance"),
	}
	_, err := awsAutoscalingController.manageASGProcesses(awsAutoscalingclient, "infra-k8s-worker", scalingProcesses, "suspend")
	if err != nil {
		t.Error("got error when attempting to suspend an ASG")
	}
}

func TestAwsManageASGProcesesResume(t *testing.T) {
	awsAutoscalingclient := newFakeAWSAutoscalingClient()
	awsAutoscalingController := &AwsAutoscalingController{}
	scalingProcesses := []*string{
		aws.String("AZRebalance"),
	}
	_, err := awsAutoscalingController.manageASGProcesses(awsAutoscalingclient, "infra-k8s-worker", scalingProcesses, "resume")
	if err != nil {
		t.Error("got error when attempting to suspend an ASG")
	}
}

func TestAwsSetDesiredCount(t *testing.T) {
	awsAutoscalingclient := newFakeAWSAutoscalingClient()
	awsAutoscalingController := &AwsAutoscalingController{}
	_, err := awsAutoscalingController.setDesiredCount(awsAutoscalingclient, "infra-k8s-worker", 4)
	if err != nil {
		t.Error("got error when attempting to set disired capacity for an ASG")
	}
}
