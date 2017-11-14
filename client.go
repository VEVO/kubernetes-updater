package main

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
	meta_v1 "k8s.io/client-go/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

type kubernetesClient interface {
	getDeployment(service string, namespace string) (*v1beta1.Deployment, error)
	updateDeployment(*v1beta1.Deployment) (*v1beta1.Deployment, error)
	getNodes(v1.ListOptions) (*v1.NodeList, error)
	updateNode(*v1.Node) (*v1.Node, error)
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

func (c kubernetesClientConfig) getDeployment(service string, namespace string) (*v1beta1.Deployment, error) {
	deployment := c.clientset.Extensions().Deployments(namespace)
	return deployment.Get(service, meta_v1.GetOptions{})
}

func (c kubernetesClientConfig) updateDeployment(newDeployment *v1beta1.Deployment) (*v1beta1.Deployment, error) {
	deployment := c.clientset.Extensions().Deployments(newDeployment.ObjectMeta.Namespace)
	return deployment.Update(newDeployment)
}

func (c kubernetesClientConfig) getNodes(listOptions v1.ListOptions) (*v1.NodeList, error) {
	nodeList, err := c.clientset.Core().Nodes().List(listOptions)
	return nodeList, err
}

func (c kubernetesClientConfig) updateNode(newNode *v1.Node) (*v1.Node, error) {
	node, err := c.clientset.Core().Nodes().Update(newNode)
	return node, err
}
