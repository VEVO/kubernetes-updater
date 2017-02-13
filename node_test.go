package main

import "testing"

func TestKubernetesNodes_GetNodesByLabel(t *testing.T) {
	client := newFakeClient()
	nodesController := kubernetesNodes{}
	labels := make(map[string]string)
	labels["instance-id"] = "i-fake-instanceid"
	nodeList, err := nodesController.getNodesByLabel(client, labels)
	if err != nil {
		t.Errorf("failed to populate node by label: %s", err)
	}

	if len(nodeList.Items) <= 0 {
		t.Error("failed to get node by label")
	}

	for _, node := range nodeList.Items {
		if node.ObjectMeta.Name != "fake-service" {
			t.Errorf("expected fake-service but got %s", node.ObjectMeta.Name)
		}
		if node.ObjectMeta.Namespace != "fake-namespace" {
			t.Errorf("expected fake-namespace but got %s", node.ObjectMeta.Namespace)
		}
	}
}

func TestKubernetesNodes_GetNodesByLabelMissing(t *testing.T) {
	client := newFakeClient()
	nodesController := kubernetesNodes{}
	labels := make(map[string]string)
	labels["instance-id"] = "i-missing-instanceid"
	nodeList, err := nodesController.getNodesByLabel(client, labels)
	if err != nil {
		t.Errorf("failed to populate node by label: %s", err)
	}

	if len(nodeList.Items) > 0 {
		t.Error("got undesired node by label")
	}
}

func TestKubernetesNodes_UpdateNodesByLabel(t *testing.T) {
	client := newFakeClient()
	nodesController := kubernetesNodes{}
	labels := make(map[string]string)
	labels["instance-id"] = "i-fake-instanceid"
	nodeList, err := nodesController.getNodesByLabel(client, labels)
	if err != nil {
		t.Errorf("failed to populate node by label: %s", err)
	}

	for _, node := range nodeList.Items {
		node.Spec.Unschedulable = true
		node := &node
		updatedNode, err := nodesController.updateNode(client, node)
		if err != nil {
			t.Error("failed to update node")
		}
		if !updatedNode.Spec.Unschedulable {
			t.Error("failed to update node")
		}
	}
}
