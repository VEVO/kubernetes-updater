package main

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type FakeKubernetesClientConfig struct{}

var fakeDeployment = &appsv1.Deployment{
	Spec: appsv1.DeploymentSpec{
		Replicas: int32p(1),
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "fake-service",
				Namespace: "fake-namespace",
			},
			Spec: corev1.PodSpec{},
		},
	},
}

var fakeNode = corev1.Node{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "fake-service",
		Namespace: "fake-namespace",
	},
	Spec: corev1.NodeSpec{
		Unschedulable: false,
	},
}

func fakeNodeList(listOptions metav1.ListOptions) *corev1.NodeList {
	labels := make(map[string]string)
	labels["instance-id"] = "i-fake-instanceid"

	if listOptions.LabelSelector != keysString(labels) {
		return &corev1.NodeList{
			ListMeta: metav1.ListMeta{},
			Items:    []corev1.Node{},
		}
	}

	fakeNode.Labels = labels

	nodeList := &corev1.NodeList{
		ListMeta: metav1.ListMeta{},
		Items: []corev1.Node{
			fakeNode,
		},
	}
	return nodeList
}

func newFakeClient() kubernetesClient {
	return &FakeKubernetesClientConfig{}
}

func (c FakeKubernetesClientConfig) getDeployment(service string, namespace string) (*appsv1.Deployment, error) {
	if service == fakeDeployment.Spec.Template.ObjectMeta.Name && namespace ==
		fakeDeployment.Spec.Template.ObjectMeta.Namespace {
		return fakeDeployment, nil
	}
	err := fmt.Errorf("deployments.extensions \"%s\" not found", service)
	return &appsv1.Deployment{}, err
}

func (c FakeKubernetesClientConfig) updateDeployment(newDeployment *appsv1.Deployment) (*appsv1.Deployment, error) {
	return newDeployment, nil
}

func (c FakeKubernetesClientConfig) getNodes(listOptions metav1.ListOptions) (*corev1.NodeList, error) {
	return fakeNodeList(listOptions), nil
}

func (c FakeKubernetesClientConfig) updateNode(newNode *corev1.Node) (*corev1.Node, error) {
	return newNode, nil
}
func (c FakeKubernetesClientConfig) drainNode(newNode *corev1.Node) (error) {
	return nil
}