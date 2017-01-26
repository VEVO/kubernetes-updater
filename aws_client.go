package main

type AwsEc2Container struct {
	client     *AwsEc2Client
	controller *AwsEc2Controller
}

type AwsAutoscalingContainer struct {
	client     *AwsAutoscalingClient
	controller *AwsAutoscalingController
}

type AwsClient struct {
	ec2         *AwsEc2Container
	autoscaling *AwsAutoscalingContainer
}

func NewAwsClient() *AwsClient {
	awsClient := &AwsClient{
		ec2: &AwsEc2Container{
			client:     newAWSEc2Client(),
			controller: &AwsEc2Controller{},
		},
		autoscaling: &AwsAutoscalingContainer{
			client:     newAWSAutoscalingClient(),
			controller: &AwsAutoscalingController{},
		},
	}
	return awsClient
}
