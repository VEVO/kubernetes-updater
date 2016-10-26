package main

import (
	"fmt"
	"testing"
)

func TestGetReplicaCount(t *testing.T) {
	client := NewFakeClient()
	deploymentController := KubernetesDeployment{service: "fake-service",
		namespace: "fake-namespace"}
	deploymentObject, _ := deploymentController.GetDeployment(client)
	replicas := *deploymentObject.Spec.Replicas
	if replicas != int32(1) {
		t.Error(fmt.Sprintf("expected 1, got %i", replicas))
	}
}

func TestIncreasingReplicaCount(t *testing.T) {
	client := NewFakeClient()
	deploymentController := KubernetesDeployment{service: "fake-service",
		namespace: "fake-namespace"}
	replicas, _ := SetReplicasForDeployment(client, deploymentController, int32(10))
	if replicas != int32(10) {
		t.Error(fmt.Sprintf("expected 10, got %d", replicas))
	}
}

func TestDecreasingReplicaCount(t *testing.T) {
	client := NewFakeClient()
	deploymentController := KubernetesDeployment{service: "fake-service",
		namespace: "fake-namespace"}
	replicas, _ := SetReplicasForDeployment(client, deploymentController, int32(5))
	if replicas != int32(5) {
		t.Error(fmt.Sprintf("expected 5, got %d", replicas))
	}
}

func TestMissingService(t *testing.T) {
	client := NewFakeClient()
	deploymentController := KubernetesDeployment{service: "missing-service",
		namespace: "fake-namespace"}
	_, err := deploymentController.GetDeployment(client)
	if err == nil {
		t.Error("expected error but got nil")
	}
	if err != nil {
		if fmt.Sprintf("%s", err) != "deployments.extensions \"missing-service\" not found" {
			t.Error(fmt.Sprintf("expected error \"deployments.extensions \"missing-service\" not found\", but got \"%s\"", err))
		}
	}
}
