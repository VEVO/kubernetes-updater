package main

import "testing"

func TestAwsClient(t *testing.T) {
	awsClient := newAwsClient()
	if awsClient.ec2 == nil {
		t.Failed()
	}
	if awsClient.autoscaling == nil {
		t.Failed()
	}
}
