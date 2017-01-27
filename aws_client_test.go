package main

import (
	"testing"

	"fmt"
	"github.com/aws/aws-sdk-go/service/ec2"
)

func TestAwsClient(t *testing.T) {
	// This should probably go into the aws_ec2_test.go file but I wanted to test the client somehow.
	awsClient := NewAwsClient()
	kubernetesCluster = "dev-us-east-1-infra"
	filters := []*ec2.Filter{
		awsClient.ec2.controller.newEC2Filter("tag:KubernetesCluster", kubernetesCluster),
		awsClient.ec2.controller.newEC2Filter("instance-state-name", "running"),
	}
	_, err := awsClient.ec2.controller.updateFilters(filters)
	if err != nil {
		t.Error("Unable to set filters on ec2 controller")
	}
	params := &ec2.DescribeInstancesInput{}
	params.Filters = []*ec2.Filter{
		awsClient.ec2.controller.newEC2Filter("tag:ServiceComponent", "k8s-master"),
	}
	inv, err := awsClient.ec2.controller.DescribeInstancesNotMatchingAnsibleVersion(awsClient.ec2.client, params, ansibleVersion)
	if err != nil {
		t.Errorf("got error when trying to get ec2 instances: %s", err)
	}
	fmt.Println(len(inv))
	//for _, i := range inv {
	//	fmt.Println(fmt.Sprintf("+%v\n", i))
	//}
}
