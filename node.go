package main

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type kubernetesNode struct {
	name string
}

type kubernetesNodes struct {
	list []kubernetesNode
}

func (k kubernetesNodes) getNodesByLabel(client kubernetesClient, labels map[string]string) (*corev1.NodeList, error) {
	listOptions := metav1.ListOptions{
		LabelSelector: keysString(labels),
	}
	nodeObject, err := client.getNodes(listOptions)
	return nodeObject, err
}

func (k kubernetesNodes) updateNode(client kubernetesClient, node *corev1.Node) (*corev1.Node, error) {
	node, err := client.updateNode(node)
	return node, err
}
