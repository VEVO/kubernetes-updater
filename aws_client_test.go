package main

import (
	"testing"

	"github.com/aws/aws-sdk-go/service/ec2"
)

func TestAwsClient(t *testing.T) {
	// This should probably go into the aws_ec2_test.go file but I wanted to test the client somehow.
	client := NewAwsClient()
	filters := []*ec2.Filter{
		client.ec2.controller.newEC2Filter("tag:KubernetesCluster", kubernetesCluster),
		client.ec2.controller.newEC2Filter("instance-state-name", "running"),
	}
	_, err := client.ec2.controller.setFilters(filters)
	if err != nil {
		t.Error("Unable to set filters on ec2 controller")
	}
}
