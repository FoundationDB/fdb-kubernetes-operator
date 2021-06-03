# Common Operations

## Replacing a Process

If you delete a pod, the operator will automatically create a new pod to replace it. If there is a volume available for re-use, we will create a new pod to match that volume. This means that in general you can replace a bad process just by deleting the pod. This may not be desirable in all situations, as it creates a loss of fault tolerance until the replacement pod is created. This also requires that the original volume be available, which may not be possible in some failure scenarios.

As an alternative, you can replace a pod by explicitly placing it in the `instancesToRemove` list:

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.30
  instancesToRemove:
    - storage-1
```

When comparing the desired process count with the current pod count, any pods that are in the pending removal list are not counted. This means that the operator will only consider there to be 2 running storage pods, rather than 3, and will create a new one to fill the gap. Once this is done, it will go through the same removal process described under [Shrinking a Cluster](scaling.md#shrinking-a-cluster). The cluster will remain at full fault tolerance throughout the reconciliation. This allows you to replace an arbitrarily large number of processes in a cluster without any risk of availability loss.

## Adding a Knob

To add a knob, you can change the `customParameters` in the cluster spec:

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.30
  customParameters:
    - "knob_always_causal_read_risky=1"
```

The operator will update the monitor conf to contain the new knob, and will then bounce all of the fdbserver processes. As soon as fdbmonitor detects that the fdbserver process has died, it will create a new fdbserver process with the latest config. The cluster should be fully available within 10 seconds of executing the bounce, though this can vary based on cluster size.

The process for updating the monitor conf can take several minutes, based on the time it takes Kubernetes to update the config map in the pods.

## Upgrading a Cluster

To upgrade a cluster, you can change the version in the cluster spec:

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.3.12
```

This will first update the sidecar image in the pod to match the new version, which will restart that container. On restart, it will copy the new FDB binaries into the config volume for the foundationdb container, which will make it available to run. We will then update the fdbmonitor conf to point to the new binaries and bounce all of the fdbserver processes.

Once all of the processes are running at the new version, we will recreate all of the pods so that the `foundationdb` container uses the new version for its own image. This will use the strategies described in [Pod Update Strategy](customization.md#pod-update-strategy).

## Renaming a Cluster

The name of a cluster is immutable, and it is included in the names of all of the dependent resources, as well as in labels on the resources. If you want to change the name later on, you can do so with the following steps. This example assumes you are renaming the cluster `sample-cluster` to `sample-cluster-2`.

1.  Create a new cluster named `sample-cluster-2`, using the current connection string from `sample-cluster` as its seedConnectionString. You will need to set an `instanceIdPrefix` for `sample-cluster-2` to be different from the `instanceIdPrefix` for `sample-cluster`, to make sure that the instance IDs do not collide. The rest of the spec can match `sample-cluster`, other than any fields that you want to customize based on the new name. Wait for reconciliation on `sample-cluster-2` to complete.
2.  Update the spec for `sample-cluster` to set the process counts for `log`, `stateless`, and `storage` to `-1`. You should omit all other process counts. Wait for the reconciliation for `sample-cluster` to complete.
3.  Check that none of the original pods from `sample-cluster` are running.
4.  Delete the `sample-cluster` resource.

At that point, you will be left with just the resources for `sample-cluster-2`. You can continue performing operations on `sample-cluster-2` as normal. You can also change or remove the `instanceIDPrefix` if you had to set it to a different value earlier in the process.

## Next

You can continue on to the [next section](scaling.md) or go back to the [table of contents](index.md).
