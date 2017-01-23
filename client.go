package main

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
	meta_v1 "k8s.io/client-go/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

type KubernetesClient interface {
	getDeployment(service string, namespace string) (*v1beta1.Deployment, error)
	updateDeployment(*v1beta1.Deployment) (*v1beta1.Deployment, error)
	getNodes(v1.ListOptions) (*v1.NodeList, error)
	updateNode(*v1.Node) (*v1.Node, error)
}

type KubernetesClientConfig struct {
	clientset *kubernetes.Clientset
}

func NewClient(server string, username string, password string) KubernetesClient {
	config := &rest.Config{
		Host:     server,
		Username: username,
		Password: password,
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	return &KubernetesClientConfig{clientset: clientset}
}

func (c KubernetesClientConfig) getDeployment(service string, namespace string) (*v1beta1.Deployment, error) {
	deployment := c.clientset.Extensions().Deployments(namespace)
	return deployment.Get(service, meta_v1.GetOptions{})
}

func (c KubernetesClientConfig) updateDeployment(newDeployment *v1beta1.Deployment) (*v1beta1.Deployment, error) {
	deployment := c.clientset.Extensions().Deployments(newDeployment.ObjectMeta.Namespace)
	return deployment.Update(newDeployment)
}

func (c KubernetesClientConfig) getNodes(listOptions v1.ListOptions) (*v1.NodeList, error) {
	nodeList, err := c.clientset.Core().Nodes().List(listOptions)
	return nodeList, err
}

func (c KubernetesClientConfig) updateNode(newNode *v1.Node) (*v1.Node, error) {
	node, err := c.clientset.Core().Nodes().Update(newNode)
	return node, err
}
