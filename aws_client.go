package main

type AwsEc2Helper struct {
	controller *AwsEc2Controller
}

type AwsAutoscalingHelper struct {
	controller *AwsAutoscalingController
}

type AwsClient struct {
	ec2         *AwsEc2Controller
	autoscaling *AwsAutoscalingController
}

func NewAwsClient() *AwsClient {
	awsClient := &AwsClient{
		ec2:         newAWSEc2Controller(newAWSEc2Client()),
		autoscaling: newAWSAutoscalingController(newAWSAutoscalingClient()),
	}
	return awsClient
}
