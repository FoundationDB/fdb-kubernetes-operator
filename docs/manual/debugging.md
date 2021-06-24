# Debugging

## Kubectl FDB Plugin

You can use the [kubectl-fdb](/tree/master/kubectl-fdb) plugin when investigating issues and running imperative commands.

If a cluster is stuck in a reconciliation you can use the `kubectl-fdb` plugin to analyze the issue:

```
$ kubectl fdb analyze example-cluster
Checking cluster: default/example-cluster
✔ Cluster is available
✔ Cluster is fully replicated
✖ Cluster is not reconciled
✖ ProcessGroup: stateless-5 has the following condition: MissingProcesses since 2021-04-27 02:57:25 +0100 BST
✖ ProcessGroup: storage-2 has the following condition: MissingProcesses since 2021-03-25 07:31:08 +0000 GMT
✖ ProcessGroup: storage-2 has the following condition: PodFailing since 2021-03-25 18:23:22 +0000 GMT
✖ ProcessGroup: storage-2 has the following condition: IncorrectConfigMap since 2021-03-26 19:20:56 +0000 GMT
✖ ProcessGroup: transaction-5 has the following condition: MissingProcesses since 2021-04-27 02:57:24 +0100 BST
✖ Pod default/example-cluster-storage-2 has unexpected Phase Pending with Reason:
✖ Pod default/example-cluster-storage-2 has an unready container: foundationdb
✖ Pod default/example-cluster-storage-2 has an unready container: foundationdb-kubernetes-sidecar
✖ Pod default/example-cluster-storage-2 has an unready container: trace-log-forwarder
Error:
found issues for cluster example-cluster. Please check them
```

The plugin can also resolve these issues automatically:

```
$ kubectl fdb analyze example-cluster --auto-fix
Checking cluster: default/example-cluster
✔ Cluster is available
✔ Cluster is fully replicated
✖ Cluster is not reconciled
✖ ProcessGroup: stateless-5 has the following condition: MissingProcesses since 2021-04-27 02:57:25 +0100 BST
✖ ProcessGroup: storage-2 has the following condition: MissingProcesses since 2021-03-25 07:31:08 +0000 GMT
✖ ProcessGroup: storage-2 has the following condition: PodFailing since 2021-03-25 18:23:22 +0000 GMT
✖ ProcessGroup: storage-2 has the following condition: IncorrectConfigMap since 2021-03-26 19:20:56 +0000 GMT
✖ ProcessGroup: transaction-5 has the following condition: MissingProcesses since 2021-04-27 02:57:24 +0100 BST
✖ Pod default/example-cluster-storage-2 has unexpected Phase Pending with Reason:
✖ Pod default/example-cluster-storage-2 has an unready container: foundationdb
✖ Pod default/example-cluster-storage-2 has an unready container: foundationdb-kubernetes-sidecar
✖ Pod default/example-cluster-storage-2 has an unready container: trace-log-forwarder
Replace instances [stateless-5 storage-2 transaction-5] in cluster default/example-cluster [y/n]: y
Error:
found issues for cluster example-cluster. Please check them
```

When using this feature, read carefully what the plugin wants to do and only confirm the dialog when you are sure that you want to do these actions.

## Pods stuck in Pending

If you have Pods that are failing to launch, because they are stuck in either a pending or terminating state, you can address that by replacing the failing instance. You can do that using a [plugin command](#replacing-pods-with-the-kubectl-plugin).

## Replacing Pods with the kubectl plugin

Let's assume we are working with the cluster `example-cluster`, and the pod `example-cluster-storage-1` is failing to launch.

```
$ kubectl fdb remove instances -c example-cluster example-cluster-storage-1
Remove [storage-1] from cluster default/example-cluster with exclude: true and shrink: false [y/n]:
# Confirm with 'y' if the excluded Pod is the correct one
```

Once you confirm this change, the operator begins replacing the instance, and should be able to complete reconciliation.
If additional pods fail to launch, you can replace them with the same command.

## Exclusions Failing Due to Missing IP

If the pod does not have an IP assigned, the exclusion will not be possible, because the IP address is the only thing we can exclude on.
If this happens, you will see an error of the form `Cannot check the exclusion state of instance storage-1, which has no IP address`.
When this happens, the only way to finish the replacement is to skip the exclusion:

```yaml
$ kubectl fdb remove instances --exclusion=false -c example-cluster example-cluster-storage-1
Remove [storage-1] from cluster default/example-cluster with exclude: false and shrink: false [y/n]:
# Confirm with 'y' if the excluded Pod is the correct one
```

**NOTE**: This is a very dangerous operation.
This will delete the pod and the PVC without checking that the data has been re-replicated.
You should only due this after checking that the database is available, that the database has not had any data loss, and that the pod is not currently running. You can confirm the first and second check by looking in the cluster status.

## Reconciliation Not Running

If reconciliation is not complete, and there are no recent messages in the operator logs for the cluster, it may be that the reconciliation is backing off due to repeated failures. It should eventually retry the reconciliation. If you want to force it to run reconciliation again immediately, you can edit the cluster metadata. The operator will receive an event about the change and start reconciling. The best no-op change to make is a new annotation.

Example:

```yaml
metadata:
  annotations:
    touch: touch1
```

or simply run:

```bash
kubectl annotate fdb cluster force-reconcile="$(date)" --overwrite
```

### Running CLI Commands

If you want to open up a shell or run a CLI, you can use the [plugin](#kubectl-fdb-plugin):

```bash
 kubectl fdb exec -c example-cluster -- bash
```

Or if you want to check a specific pod:

```bash
kubectl exec -it sample-cluster-storage-1 -c foundationdb -- bash
```

This will open up a shell inside the container. You can use this shell to examine local files, run debug commands, or open a CLI.

When running the CLI on Kubernetes, you can simply run `fdbcli` with no additional arguments. The shell path, cluster file, TLS certificates, and any other required configuration will be supplied through the environment.

## Next

You can continue on to the [next section](more.md) or go back to the [table of contents](index.md).
