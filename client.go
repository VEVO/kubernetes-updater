package main

import (
	"context"
	"fmt"
	"github.com/golang/glog"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/drain"
	"os"
	"time"
)

type kubernetesClient interface {
	getDeployment(service string, namespace string) (*appsv1.Deployment, error)
	updateDeployment(*appsv1.Deployment) (*appsv1.Deployment, error)
	getNodes(metav1.ListOptions) (*corev1.NodeList, error)
	updateNode(*corev1.Node) (*corev1.Node, error)
	drainNode(*corev1.Node) error
}

type kubernetesClientConfig struct {
	clientset *kubernetes.Clientset
}

func newClient(server string, token string) kubernetesClient {
	config := &rest.Config{
		Host:            server,
		BearerToken:     token,
		QPS:             100.0,
		Burst:           200,
		TLSClientConfig: rest.TLSClientConfig{Insecure: true},
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	return &kubernetesClientConfig{clientset: clientset}
}

func (c kubernetesClientConfig) getDeployment(service string, namespace string) (*appsv1.Deployment, error) {
	deployment := c.clientset.AppsV1().Deployments(namespace)
	return deployment.Get(context.TODO(), service, metav1.GetOptions{})
}

func (c kubernetesClientConfig) updateDeployment(newDeployment *appsv1.Deployment) (*appsv1.Deployment, error) {
	deployment := c.clientset.AppsV1().Deployments(newDeployment.ObjectMeta.Namespace)
	return deployment.Update(context.TODO(), newDeployment, metav1.UpdateOptions{})
}

func (c kubernetesClientConfig) getNodes(listOptions metav1.ListOptions) (*corev1.NodeList, error) {
	nodeList, err := c.clientset.CoreV1().Nodes().List(context.TODO(), listOptions)
	return nodeList, err
}

func (c kubernetesClientConfig) updateNode(newNode *corev1.Node) (*corev1.Node, error) {
	node, err := c.clientset.CoreV1().Nodes().Update(context.TODO(), newNode, metav1.UpdateOptions{})
	return node, err
}

func (c kubernetesClientConfig) drainNode(node *corev1.Node) error {
	if c.clientset == nil {
		return fmt.Errorf("K8sClient not set")
	}
	if node == nil {
		return fmt.Errorf("node not set")
	}
	if node.Name == "" {
		return fmt.Errorf("node name not set")
	}
	helper := &drain.Helper{
		Client:              c.clientset,
		Force:               true,
		GracePeriodSeconds:  -1,
		IgnoreAllDaemonSets: true,
		DeleteEmptyDirData:  true,
		Out:                 os.Stdout,
		ErrOut:              os.Stdout,
		Timeout:             time.Duration(120) * time.Second,
	}
	glog.V(4).Infof("Verifying node is cordoned")
	err := drain.RunCordonOrUncordon(helper, node, true)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("RunCordonOrUncordon API not found: %v", err)
		}
		return fmt.Errorf("error cordoning node: %v", err)
	}
	glog.V(4).Infof("Running drain for node %s\n", node.Name)
	err = drain.RunNodeDrain(helper, node.Name)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("RunNodeDrain API not found: %v", err)
		}
		return fmt.Errorf("error draining node: %v", err)
	}
	return nil
}
