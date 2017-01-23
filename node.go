package main

import (
	"k8s.io/client-go/pkg/api/v1"
)

type NodesController interface {
	GetNodesByLabel(KubernetesClient) (*v1.NodeList, error)
	UpdateNode(KubernetesClient, *v1.Node) (*v1.Node, error)
}

type KubernetesNode struct {
	name string
}

type KubernetesNodes struct {
	list []KubernetesNode
}

func (k KubernetesNodes) GetNodesByLabel(client KubernetesClient, labels map[string]string) (*v1.NodeList, error) {
	listOptions := v1.ListOptions{
		LabelSelector: keysString(labels),
	}
	nodeObject, err := client.getNodes(listOptions)
	return nodeObject, err
}

func (k KubernetesNodes) UpdateNode(client KubernetesClient, node *v1.Node) (*v1.Node, error) {
	node, err := client.updateNode(node)
	return node, err
}
