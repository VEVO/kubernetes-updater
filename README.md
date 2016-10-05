# kubernetes-updater
Rolling updates of kubernetes clusters

Iterate through all of the etcd, masters and worker nodes in a given cluster terminating nodes and verifying their replacement is healthy before moving on.

## Running

The tool expects that the environment is configured to support AWS named profiles as detailed [here](http://docs.aws.amazon.com/cli/latest/userguide/cli-chap-getting-started.html#cli-multiple-profiles).

The target kubernetes cluster is set by using both of these environmental variables

```
KUBERNETES_CLUSTER=dev-us-east-1-infra
AWS_PROFILE=dev
```

Verbose logging can be enabled via

```
ROLLER_VERBOSE_MODE=true
```

Additionally you can control which of the components (etcd, k8s-master and k8s-node) you want to roll.   This example would only roll the k8s-master and k8s-node components.

```
ROLLER_COMPONENTS=k8s-master,k8s-node
```
