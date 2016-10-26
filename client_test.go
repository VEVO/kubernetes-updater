package main

import (
	"fmt"

	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
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

func NewFakeClient() KubernetesClient {
	return &FakeKubernetesClientConfig{}
}

func (c FakeKubernetesClientConfig) getDeployment(service string, namespace string) (*v1beta1.Deployment, error) {
	if service == fakeDeployment.Spec.Template.ObjectMeta.Name && namespace == fakeDeployment.Spec.Template.ObjectMeta.Namespace {
		return fakeDeployment, nil
	} else {
		error := fmt.Errorf("deployments.extensions \"%s\" not found", service)
		return &v1beta1.Deployment{}, error
	}
}

func (c FakeKubernetesClientConfig) updateDeployment(newDeployment *v1beta1.Deployment) (*v1beta1.Deployment, error) {
	return newDeployment, nil
}
