package main

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type kubernetesClient interface {
	getDeployment(service string, namespace string) (*appsv1.Deployment, error)
	updateDeployment(*appsv1.Deployment) (*appsv1.Deployment, error)
	getNodes(metav1.ListOptions) (*corev1.NodeList, error)
	updateNode(*corev1.Node) (*corev1.Node, error)
}

type kubernetesClientConfig struct {
	clientset *kubernetes.Clientset
}

func newClient(server string, username string, password string) kubernetesClient {
	config := &rest.Config{
		Host:     server,
		Username: username,
		Password: password,
		QPS:      100.0,
		Burst:    200,
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	return &kubernetesClientConfig{clientset: clientset}
}

func (c kubernetesClientConfig) getDeployment(service string, namespace string) (*appsv1.Deployment, error) {
	deployment := c.clientset.Apps().Deployments(namespace)
	return deployment.Get(service, metav1.GetOptions{})
}

func (c kubernetesClientConfig) updateDeployment(newDeployment *appsv1.Deployment) (*appsv1.Deployment, error) {
	deployment := c.clientset.Apps().Deployments(newDeployment.ObjectMeta.Namespace)
	return deployment.Update(newDeployment)
}

func (c kubernetesClientConfig) getNodes(listOptions metav1.ListOptions) (*corev1.NodeList, error) {
	nodeList, err := c.clientset.Core().Nodes().List(listOptions)
	return nodeList, err
}

func (c kubernetesClientConfig) updateNode(newNode *corev1.Node) (*corev1.Node, error) {
	node, err := c.clientset.Core().Nodes().Update(newNode)
	return node, err
}
