package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
)

type AwsAutoscaling interface {
	SuspendProcesses(*autoscaling.ScalingProcessQuery) (string, error)
	ResumeProcesses(*autoscaling.ScalingProcessQuery) (string, error)
}

type AwsAutoscalingClient struct {
	session *autoscaling.AutoScaling
}

type AwsAutoscalingController struct {
}

func newAWSAutoscalingClient() *AwsAutoscalingClient {
	return &AwsAutoscalingClient{
		session: autoscaling.New(session.New()),
	}
}

func (autoScalingClient *AwsAutoscalingClient) SuspendProcesses(params *autoscaling.ScalingProcessQuery) (string, error) {
	var response *autoscaling.SuspendProcessesOutput
	response, err := autoScalingClient.session.SuspendProcesses(params)
	return response.String(), err
}

func (autoScalingClient *AwsAutoscalingClient) ResumeProcesses(params *autoscaling.ScalingProcessQuery) (string, error) {
	var response *autoscaling.ResumeProcessesOutput
	response, err := autoScalingClient.session.ResumeProcesses(params)
	return response.String(), err
}

func (c *AwsAutoscalingController) awsManageASGProcesses(autoscalingClient AwsAutoscaling, asg string, action string) (string, error) {
	var err error
	var response string

	params := &autoscaling.ScalingProcessQuery{
		AutoScalingGroupName: aws.String(asg),
		ScalingProcesses: []*string{
			aws.String("AZRebalance"),
		},
	}

	if action == "suspend" {
		response, err = autoscalingClient.SuspendProcesses(params)
	} else {
		response, err = autoscalingClient.ResumeProcesses(params)
	}

	return response, err
}
