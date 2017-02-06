package main

import "testing"

func TestAwsClient(t *testing.T) {
	awsClient := NewAwsClient()
	if awsClient.ec2 == nil {
		t.Failed()
	}
	if awsClient.autoscaling == nil {
		t.Failed()
	}
}
