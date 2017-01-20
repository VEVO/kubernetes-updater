package main

import (
	"fmt"
	"testing"
)

func TestKubernetesNodes_GetNodesByLabel(t *testing.T) {
	client := NewFakeClient()
	nodesController := KubernetesNodes{}
	labels := make(map[string]string)
	labels["instance-id"] = "i-fake-instanceid"
	nodeList, err := nodesController.GetNodesByLabel(client, labels)
	if err != nil {
		t.Error(fmt.Sprintf("failed to populate node by label: %s", err))
	}

	if len(nodeList.Items) <= 0 {
		t.Error("failed to get node by label")
	}

	for _, node := range nodeList.Items {
		if node.ObjectMeta.Name != "fake-service" {
			t.Error(fmt.Sprintf("expected fake-service but got %s", node.ObjectMeta.Name))
		}
		if node.ObjectMeta.Namespace != "fake-namespace" {
			t.Error(fmt.Sprintf("expected fake-namespace but got %s", node.ObjectMeta.Namespace))
		}
	}
}

func TestKubernetesNodes_GetNodesByLabelMissing(t *testing.T) {
	client := NewFakeClient()
	nodesController := KubernetesNodes{}
	labels := make(map[string]string)
	labels["instance-id"] = "i-missing-instanceid"
	nodeList, err := nodesController.GetNodesByLabel(client, labels)

	if err != nil {
		t.Error(fmt.Sprintf("failed to populate node by label: %s", err))
	}

	if len(nodeList.Items) > 0 {
		t.Error("got undesired node by label")
	}
}

func TestKubernetesNodes_UpdateNodesByLabel(t *testing.T) {
	client := NewFakeClient()
	nodesController := KubernetesNodes{}
	labels := make(map[string]string)
	labels["instance-id"] = "i-fake-instanceid"
	nodeList, err := nodesController.GetNodesByLabel(client, labels)
	if err != nil {
		t.Error(fmt.Sprintf("failed to populate node by label: %s", err))
	}

	for _, node := range nodeList.Items {
		node.Spec.Unschedulable = true
		node := &node
		updatedNode, err := nodesController.UpdateNode(client, node)
		if err != nil {
			t.Error("failed to update node")
		}
		if updatedNode.Spec.Unschedulable != true {
			t.Error("failed to update node")
		}
	}
}
