package main

import (
	"fmt"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
	meta_v1 "k8s.io/client-go/pkg/apis/meta/v1"
)

type FakeKubernetesClientConfig struct{}

var fakeDeployment = &v1beta1.Deployment{
	Spec: v1beta1.DeploymentSpec{
		Replicas: int32p(1),
		Template: v1.PodTemplateSpec{
			ObjectMeta: v1.ObjectMeta{
				Name:      "fake-service",
				Namespace: "fake-namespace",
			},
			Spec: v1.PodSpec{},
		},
	},
}

var fakeNode = v1.Node{
	ObjectMeta: v1.ObjectMeta{
		Name:      "fake-service",
		Namespace: "fake-namespace",
	},
	Spec: v1.NodeSpec{
		Unschedulable: false,
	},
}

func fakeNodeList(listOptions v1.ListOptions) *v1.NodeList {
	labels := make(map[string]string)
	labels["instance-id"] = "i-fake-instanceid"

	if listOptions.LabelSelector != keysString(labels) {
		return &v1.NodeList{
			ListMeta: meta_v1.ListMeta{},
			Items:    []v1.Node{},
		}
	}

	fakeNode.Labels = labels

	nodeList := &v1.NodeList{
		ListMeta: meta_v1.ListMeta{},
		Items: []v1.Node{
			fakeNode,
		},
	}
	return nodeList
}

func NewFakeClient() KubernetesClient {
	return &FakeKubernetesClientConfig{}
}

func (c FakeKubernetesClientConfig) getDeployment(service string, namespace string) (*v1beta1.Deployment, error) {
	if service == fakeDeployment.Spec.Template.ObjectMeta.Name && namespace ==
		fakeDeployment.Spec.Template.ObjectMeta.Namespace {
		return fakeDeployment, nil
	} else {
		err := fmt.Errorf("deployments.extensions \"%s\" not found", service)
		return &v1beta1.Deployment{}, err
	}
}

func (c FakeKubernetesClientConfig) updateDeployment(newDeployment *v1beta1.Deployment) (*v1beta1.Deployment, error) {
	return newDeployment, nil
}

func (c FakeKubernetesClientConfig) getNodes(listOptions v1.ListOptions) (*v1.NodeList, error) {
	return fakeNodeList(listOptions), nil
}

func (c FakeKubernetesClientConfig) updateNode(newNode *v1.Node) (*v1.Node, error) {
	return newNode, nil
}
