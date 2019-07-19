package main

import (
	appsv1 "k8s.io/api/apps/v1"
)

type deploymentController interface {
	getDeployment(kubernetesClient) (*appsv1.Deployment, error)
	updateDeployment(kubernetesClient, *appsv1.Deployment) (*appsv1.Deployment, error)
}

type kubernetesDeployment struct {
	service   string
	namespace string
}

func (k kubernetesDeployment) getDeployment(client kubernetesClient) (*appsv1.Deployment, error) {
	deploymentObject, err := client.getDeployment(k.service, k.namespace)
	return deploymentObject, err
}

func (k kubernetesDeployment) updateDeployment(client kubernetesClient, deployment *appsv1.Deployment) (*appsv1.Deployment, error) {
	deploymentObject, err := client.updateDeployment(deployment)
	return deploymentObject, err
}

func setReplicasForDeployment(client kubernetesClient, deploymentContoller deploymentController, replicaCount int32) (int32, error) {
	deploymentObject, err := deploymentContoller.getDeployment(client)
	if err != nil {
		return replicaCount, err
	}
	deploymentObject.Spec.Replicas = int32p(replicaCount)
	newDeploymentObject, err := deploymentContoller.updateDeployment(client, deploymentObject)
	if err != nil {
		return *deploymentObject.Spec.Replicas, err
	}
	return *newDeploymentObject.Spec.Replicas, nil
}
