# Getting Started

## Introduction

The core of the operator is a reconciliation loop. In this loop, the operator reads the latest cluster spec, compares it to the running state of the cluster, and carries out whatever tasks need to be done to make the running state of the cluster match the desired state as expressed in the cluster spec. If the operator cannot fully reconcile the cluster in a single pass, it will try the reconciliation again. This can occur for a number of reasons: operations that require asynchronous work, error conditions, operations that are disabled, and so on.

When you make a change to the cluster spec, it will increment the `generation` field in the cluster metadata. Once reconciliation completes, the `generations.reconciled` field in the cluster status will be updated to reflect the last generation that we have reconciled. You can compare these two fields to determine whether your changes have been fully applied. You can also see the current generation and reconciled generation in the output of `kubectl get foundationdbcluster`.

To run the operator in your environment, you need to install the controller and the CRDs:

```bash
kubectl apply -f https://raw.githubusercontent.com/FoundationDB/fdb-kubernetes-operator/master/config/crd/bases/apps.foundationdb.org_foundationdbclusters.yaml
kubectl apply -f https://raw.githubusercontent.com/FoundationDB/fdb-kubernetes-operator/master/config/crd/bases/apps.foundationdb.org_foundationdbbackups.yaml
kubectl apply -f https://raw.githubusercontent.com/FoundationDB/fdb-kubernetes-operator/master/config/crd/bases/apps.foundationdb.org_foundationdbrestores.yaml
kubectl apply -f https://raw.githubusercontent.com/foundationdb/fdb-kubernetes-operator/master/config/samples/deployment.yaml
```

You can see logs from the operator by running `kubectl logs -f -l app=fdb-kubernetes-operator-controller-manager --container=manager`. You will likely want to watch these logs as you make changes to get a better understanding of what the operator is doing.

The example below will cover creating a cluster. All subsequent examples will assume that you have just created this cluster, and will cover an operation on this cluster.

For more information on the fields you can define on the cluster resource, see the [go docs](https://godoc.org/github.com/FoundationDB/fdb-kubernetes-operator/pkg/apis/apps/v1beta1#FoundationDBCluster).

For more information on version compatibility, see our [compatibility guide](/docs/compatibility.md).

## Creating a Cluster

To start with, we are going to be creating a cluster with the following configuration:

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.30
```

This will create a cluster with 3 storage processes, 4 log processes, and 7 stateless processes. Each fdbserver process will be in a separate pod, and the pods will have names of the form `sample-cluster-$role-$n`, where `$n` is the instance ID and `$role` is the role for the process.

You can run `kubectl get foundationdbcluster sample-cluster` to check the progress of reconciliation. Once the reconciled generation appears in this output, the cluster should be up and ready. After creating the cluster, you can connect to the cluster by running `kubectl exec -it sample-cluster-log-1 -- fdbcli`.

This example requires non-trivial resources, based on what a process will need in a production environment. This means that is too large to run in a local testing environment. It also requires disk I/O features that are not present in Docker for Mac. If you want to run these tests in that kind of environment, you can try bringing in the resource requirements, knobs, and fault domain information from a [local testing example](../config/samples/cluster_local.yaml).

In addition to the pods, the operator will create a Persistent Volume Claim for any stateful
processes in the cluster. In this example, each volume will be 128 GB.

By default each pod will have two containers and one init container. The `foundationdb` container will run fdbmonitor and fdbserver, and is the main container for the pod. The `foundationdb-kubernetes-sidecar` container will run a sidecar image designed to help run FDB on Kubernetes. It is responsible for managing the fdbmonitor conf files and providing FDB binaries to the `foundationdb` container. The operator will create a config map that contains a template for the monitor conf file, and the sidecar will interpolate instance-specific fields into the conf and make it available to the fdbmonitor process through a shared volume. The "Upgrading a Cluster" has more detail on we manage binaries. The init container will run the same sidecar image, and will ensure that the initial binaries and dynamic conf are ready before the fdbmonitor process starts.

## Accessing a Cluster

Now that your cluster is deployed, you can easily access the cluster. As an example, we are going to deploy a [Kubernetes Job](https://kubernetes.io/docs/tasks/job/) that will check the status of the cluster every minute. The `cluster file` is available through the exposed `config map` that can be mounted as follows:

```yaml
apiVersion: batch/v1beta1
kind: CronJob
metadata:
  name: fdbcli-status-cronjob
spec:
  schedule: "*/1 * * * *" # every minute
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: OnFailure
          containers:
          - name: fdbcli-status-cronjob
            image: foundationdb/foundationdb:6.2.30
            args:
            - /usr/bin/fdbcli
            - --exec
            - 'status'
            env:
            - name: FDB_CLUSTER_FILE
              value: /mnt/config-volume/cluster-file
            volumeMounts:
            - name: config-volume
              mountPath: /mnt/config-volume
          volumes:
          - name: config-volume
            configMap:
              name: sample-cluster-config
```

Note that:

* The name of the config map will depend on the name of your cluster.
* For long-running applications you should ensure that your cluster file is writeable by your application.

## Next

You can continue on to the [next section](warnings.md) or go back to the [table of contents](index.md).
