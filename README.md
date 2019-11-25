[![GoDoc](https://godoc.org/github.com/VEVO/kubernetes-updater?status.svg)](https://godoc.org/github.com/VEVO/kubernetes-updater)
[![Test Status](https://github.com/VEVO/kubernetes-updater/workflows/tests/badge.svg)](https://github.com/VEVO/kubernetes-updater/actions?query=workflow%3Atests)
[![Go Report Card](https://goreportcard.com/badge/github.com/VEVO/kubernetes-updater)](https://goreportcard.com/report/github.com/VEVO/kubernetes-updater)

# kubernetes-updater
Rolling updates of kubernetes clusters

Iterate through all of the etcd, masters and worker nodes in a given cluster terminating nodes and verifying their replacement is healthy before moving on.

## Running

The tool expects that the environment is configured to support AWS named profiles as detailed [here](http://docs.aws.amazon.com/cli/latest/userguide/cli-chap-getting-started.html#cli-multiple-profiles).

The target kubernetes cluster is set by setting the KUBERNETES_SERVER environment variable. Additionally, the following environment variables need to be passed in:

```
CLUSTER=<name of the cluster>
AWS_PROFILE=<aws profile to use>
AWS_REGION=<aws region>
```

Verbose logging can be enabled via:

```
ROLLER_LOG_LEVEL=4
```

Additionally you can control which of the components (etcd, k8s-master and k8s-node) you want to roll. This example would only roll the k8s-master and k8s-node components.

```
ROLLER_COMPONENTS=k8s-master,k8s-node
```

Example usage just specifying the kubernetes server, and rolling all components:

```
KUBERNETES_SERVER=https://kubernetes ./roller
```

Example usage for only rolling the etcd servers:
```
KUBERNETES_SERVER=https://kubernetes ROLLER_COMPONENTS=etcd ./roller
```

## Node Health Checks

A node is considered healthy by the roller when the ec2 instance has the following tags:

```
healthy: True
```

If the tag either does not exist or has a value not equal to `True`, the roller considers the ec2 instance in a bad state and will not continue with the cluster roll.
