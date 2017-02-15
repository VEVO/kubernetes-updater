package main

type awsClient struct {
	ec2         *awsEc2Controller
	autoscaling *awsAutoscalingController
}

func newAwsClient() *awsClient {
	awsClient := &awsClient{
		ec2:         newAWSEc2Controller(newAWSEc2Client()),
		autoscaling: newAWSAutoscalingController(newAWSAutoscalingClient()),
	}
	return awsClient
}
