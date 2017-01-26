package main

import (
	"testing"

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

func TestAwsManageASGProcesesSuspend(t *testing.T) {
	awsAutoscalingclient := newFakeAWSAutoscalingClient()
	awsAutoscalingController := &AwsAutoscalingController{}
	_, err := awsAutoscalingController.awsManageASGProcesses(awsAutoscalingclient, "infra-k8s-worker", "suspend")
	if err != nil {
		t.Error("got error when attempting to suspend an ASG")
	}
}

func TestAwsManageASGProcesesResume(t *testing.T) {
	awsAutoscalingclient := newFakeAWSAutoscalingClient()
	awsAutoscalingController := &AwsAutoscalingController{}
	_, err := awsAutoscalingController.awsManageASGProcesses(awsAutoscalingclient, "infra-k8s-worker", "resume")
	if err != nil {
		t.Error("got error when attempting to suspend an ASG")
	}
}
