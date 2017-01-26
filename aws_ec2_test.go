package main

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go/service/ec2"
)

var ec2Instance = &ec2.Instance{}

func TestAwsEC2_Client(t *testing.T) {
	// This is pointing at AWS for the moment. Need to move it to the fake client.
	kubernetesCluster := "dev-us-east-1-infra"
	ec2Client := newAWSEc2Client()
	ec2Controller := &AwsEc2Controller{}
	params := &ec2.DescribeInstancesInput{}
	params.Filters = []*ec2.Filter{
		ec2Controller.newEC2Filter("tag:KubernetesCluster", kubernetesCluster),
		ec2Controller.newEC2Filter("tag:ServiceComponent", "k8s-node"),
		ec2Controller.newEC2Filter("instance-state-name", "running"),
	}
	inv, err := ec2Controller.DescribeInstancesNotMatchingAnsibleVersion(ec2Client, params, ansibleVersion)

	if err != nil {
		t.Errorf("Failed to list ec2 instances: %s", err)
	}

	for _, i := range inv {
		fmt.Println(reflect.TypeOf(i.Tags))
	}
}
