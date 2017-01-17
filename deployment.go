package main

import v1beta1 "k8s.io/client-go/pkg/apis/extensions/v1beta1"

type DeploymentController interface {
	GetDeployment(KubernetesClient) (*v1beta1.Deployment, error)
	UpdateDeployment(KubernetesClient, *v1beta1.Deployment) (*v1beta1.Deployment, error)
}

type KubernetesDeployment struct {
	service   string
	namespace string
}

func (k KubernetesDeployment) GetDeployment(client KubernetesClient) (*v1beta1.Deployment, error) {
	deploymentObject, err := client.getDeployment(k.service, k.namespace)
	return deploymentObject, err
}

func (k KubernetesDeployment) UpdateDeployment(client KubernetesClient, deployment *v1beta1.Deployment) (*v1beta1.Deployment, error) {
	deploymentObject, err := client.updateDeployment(deployment)
	return deploymentObject, err
}

func SetReplicasForDeployment(client KubernetesClient, deploymentContoller DeploymentController, replicaCount int32) (int32, error) {
	deploymentObject, err := deploymentContoller.GetDeployment(client)
	if err != nil {
		return replicaCount, err
	}
	deploymentObject.Spec.Replicas = int32p(replicaCount)
	newDeploymentObject, err := deploymentContoller.UpdateDeployment(client, deploymentObject)
	if err != nil {
		return *deploymentObject.Spec.Replicas, err
	}
	return *newDeploymentObject.Spec.Replicas, nil
}
