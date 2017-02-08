package main

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
)

type AwsAutoscaling interface {
	SuspendProcesses(*autoscaling.ScalingProcessQuery) (string, error)
	ResumeProcesses(*autoscaling.ScalingProcessQuery) (string, error)
	SetDesiredCount(*autoscaling.SetDesiredCapacityInput) (string, error)
	DescribeAutoscalingGroups(*autoscaling.DescribeAutoScalingGroupsInput) (*autoscaling.DescribeAutoScalingGroupsOutput, error)
}

type AwsAutoscalingClient struct {
	session *autoscaling.AutoScaling
}

type AwsAutoscalingController struct {
	client AwsAutoscaling
}

func newAWSAutoscalingClient() AwsAutoscaling {
	return &AwsAutoscalingClient{
		session: autoscaling.New(session.New()),
	}
}

func newAWSAutoscalingController(awsAutoscalingClient AwsAutoscaling) *AwsAutoscalingController {
	return &AwsAutoscalingController{
		client: awsAutoscalingClient,
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

func (autoScalingClient *AwsAutoscalingClient) SetDesiredCount(desiredCapacity *autoscaling.SetDesiredCapacityInput) (string, error) {
	var response *autoscaling.SetDesiredCapacityOutput
	response, err := autoScalingClient.session.SetDesiredCapacity(desiredCapacity)
	return response.String(), err
}

func (autoScalingClient *AwsAutoscalingClient) DescribeAutoscalingGroups(autoscalingGroupInput *autoscaling.DescribeAutoScalingGroupsInput) (*autoscaling.DescribeAutoScalingGroupsOutput, error) {
	return autoScalingClient.session.DescribeAutoScalingGroups(autoscalingGroupInput)
}

func (c *AwsAutoscalingController) manageASGProcesses(asg string, scalingProcesses []*string, action string) (string, error) {
	var err error
	var response string

	params := &autoscaling.ScalingProcessQuery{
		AutoScalingGroupName: aws.String(asg),
		ScalingProcesses:     scalingProcesses,
	}

	if action == "suspend" {
		response, err = c.client.SuspendProcesses(params)
	} else {
		response, err = c.client.ResumeProcesses(params)
	}

	return response, err
}

func (c *AwsAutoscalingController) setDesiredCount(asg string, desiredCapacity int64) (string, error) {
	scalingProcessQuery := &autoscaling.SetDesiredCapacityInput{
		AutoScalingGroupName: &asg,
		DesiredCapacity:      &desiredCapacity,
	}
	return c.client.SetDesiredCount(scalingProcessQuery)
}

func (c *AwsAutoscalingController) getDesiredCount(asg string) (int64, error) {
	autoscalingGroupInput := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{
			&asg,
		},
	}
	autoscalingGroupOutput, err := c.client.DescribeAutoscalingGroups(autoscalingGroupInput)
	if err != nil {
		return -1, err
	}
	for _, autoscalingGroup := range autoscalingGroupOutput.AutoScalingGroups {
		if *autoscalingGroup.AutoScalingGroupName == asg {
			desiredCapacity := *autoscalingGroup.DesiredCapacity
			return desiredCapacity, nil
		}
	}
	return -1, fmt.Errorf("Could not find desired count for ASG %s", asg)
}

func (c *AwsAutoscalingController) getInstanceCount(asg string) (int, error) {
	var instances []string
	autoscalingGroupInput := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{
			&asg,
		},
	}
	autoscalingGroupOutput, err := c.client.DescribeAutoscalingGroups(autoscalingGroupInput)
	if err != nil {
		return -1, err
	}
	for _, autoscalingGroup := range autoscalingGroupOutput.AutoScalingGroups {
		if *autoscalingGroup.AutoScalingGroupName == asg {
			for _, instance := range autoscalingGroup.Instances {
				instances = append(instances, *instance.InstanceId)
			}
		}
	}
	return len(instances), nil
}
