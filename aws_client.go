package main

type AwsEc2Helper struct {
	client     *AwsEc2Client
	controller *AwsEc2Controller
}

type AwsAutoscalingHelper struct {
	client     *AwsAutoscalingClient
	controller *AwsAutoscalingController
}

type AwsClient struct {
	ec2         *AwsEc2Helper
	autoscaling *AwsAutoscalingHelper
}

func NewAwsClient() *AwsClient {
	awsClient := &AwsClient{
		ec2: &AwsEc2Helper{
			client:     newAWSEc2Client(),
			controller: &AwsEc2Controller{},
		},
		autoscaling: &AwsAutoscalingHelper{
			client:     newAWSAutoscalingClient(),
			controller: &AwsAutoscalingController{},
		},
	}
	return awsClient
}
