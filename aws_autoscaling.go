package main

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
)

type awsAutoscaling interface {
	suspendProcesses(*autoscaling.ScalingProcessQuery) (string, error)
	resumeProcesses(*autoscaling.ScalingProcessQuery) (string, error)
	setDesiredCount(*autoscaling.SetDesiredCapacityInput) (string, error)
	describeAutoscalingGroups(*autoscaling.DescribeAutoScalingGroupsInput) (*autoscaling.DescribeAutoScalingGroupsOutput, error)
}

type awsAutoscalingClient struct {
	session *autoscaling.AutoScaling
}

type awsAutoscalingController struct {
	client awsAutoscaling
}

func newAWSAutoscalingClient() awsAutoscaling {
	return &awsAutoscalingClient{
		session: autoscaling.New(session.New()),
	}
}

func newAWSAutoscalingController(awsAutoscalingClient awsAutoscaling) *awsAutoscalingController {
	return &awsAutoscalingController{
		client: awsAutoscalingClient,
	}
}

func (autoScalingClient *awsAutoscalingClient) suspendProcesses(params *autoscaling.ScalingProcessQuery) (string, error) {
	var response *autoscaling.SuspendProcessesOutput
	response, err := autoScalingClient.session.SuspendProcesses(params)
	return response.String(), err
}

func (autoScalingClient *awsAutoscalingClient) resumeProcesses(params *autoscaling.ScalingProcessQuery) (string, error) {
	var response *autoscaling.ResumeProcessesOutput
	response, err := autoScalingClient.session.ResumeProcesses(params)
	return response.String(), err
}

func (autoScalingClient *awsAutoscalingClient) setDesiredCount(desiredCapacity *autoscaling.SetDesiredCapacityInput) (string, error) {
	var response *autoscaling.SetDesiredCapacityOutput
	response, err := autoScalingClient.session.SetDesiredCapacity(desiredCapacity)
	return response.String(), err
}

func (autoScalingClient *awsAutoscalingClient) describeAutoscalingGroups(autoscalingGroupInput *autoscaling.DescribeAutoScalingGroupsInput) (*autoscaling.DescribeAutoScalingGroupsOutput, error) {
	return autoScalingClient.session.DescribeAutoScalingGroups(autoscalingGroupInput)
}

func (c *awsAutoscalingController) manageASGProcesses(asg string, scalingProcesses []*string, action string) (string, error) {
	var err error
	var response string

	params := &autoscaling.ScalingProcessQuery{
		AutoScalingGroupName: aws.String(asg),
		ScalingProcesses:     scalingProcesses,
	}

	if action == "suspend" {
		response, err = c.client.suspendProcesses(params)
	} else {
		response, err = c.client.resumeProcesses(params)
	}
	return response, err
}

func (c *awsAutoscalingController) setDesiredCount(asg string, desiredCapacity int64) (string, error) {
	scalingProcessQuery := &autoscaling.SetDesiredCapacityInput{
		AutoScalingGroupName: &asg,
		DesiredCapacity:      &desiredCapacity,
	}
	return c.client.setDesiredCount(scalingProcessQuery)
}

func (c *awsAutoscalingController) getDesiredCount(asg string) (int64, error) {
	autoscalingGroupInput := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{
			&asg,
		},
	}
	autoscalingGroupOutput, err := c.client.describeAutoscalingGroups(autoscalingGroupInput)
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

func (c *awsAutoscalingController) getInstanceCount(asg string) (int, error) {
	var instances []string
	autoscalingGroupInput := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{
			&asg,
		},
	}
	autoscalingGroupOutput, err := c.client.describeAutoscalingGroups(autoscalingGroupInput)
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
