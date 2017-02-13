package main

import (
	"fmt"
	"testing"
)

func TestGetReplicaCount(t *testing.T) {
	client := newFakeClient()
	deploymentController := kubernetesDeployment{service: "fake-service",
		namespace: "fake-namespace"}
	deploymentObject, _ := deploymentController.getDeployment(client)
	replicas := *deploymentObject.Spec.Replicas
	if replicas != int32(1) {
		t.Errorf("expected 1, got %d", replicas)
	}
}

func TestIncreasingReplicaCount(t *testing.T) {
	client := newFakeClient()
	deploymentController := kubernetesDeployment{service: "fake-service",
		namespace: "fake-namespace"}
	replicas, _ := setReplicasForDeployment(client, deploymentController, int32(10))
	if replicas != int32(10) {
		t.Errorf("expected 10, got %d", replicas)
	}
}

func TestDecreasingReplicaCount(t *testing.T) {
	client := newFakeClient()
	deploymentController := kubernetesDeployment{service: "fake-service",
		namespace: "fake-namespace"}
	replicas, _ := setReplicasForDeployment(client, deploymentController, int32(5))
	if replicas != int32(5) {
		t.Errorf("expected 5, got %d", replicas)
	}
}

func TestMissingService(t *testing.T) {
	client := newFakeClient()
	deploymentController := kubernetesDeployment{service: "missing-service",
		namespace: "fake-namespace"}
	_, err := deploymentController.getDeployment(client)
	if err == nil {
		t.Error("expected error but got nil")
	}
	if err != nil {
		if fmt.Sprintf("%s", err) != "deployments.extensions \"missing-service\" not found" {
			t.Errorf("expected error \"deployments.extensions \"missing-service\" not found\","+
				"but got \"%s\"", err)
		}
	}
}
