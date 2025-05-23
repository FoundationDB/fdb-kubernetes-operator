# Debugging

## Logging

The operator supports json-structured logging and will emit those logs to stdout and if configured to a file.
The operator sets different log keys for easier log filtering in different contexts.
All cluster related logs will contain the following fields:

- `namespace`: Namespace of the `FoundationDBCluster` resource that is currently processed.
- `cluster`: The name of the `FoundationDBCluster` resource that is currently processed.

In addition the operator will set the `reconciler` field to the current sub-reconciler.
If the operator iterates over all or a subset of `ProcessGroups`, the operator will set `processGroupID` to the current process group ID.

## Kubectl FDB Plugin

You can use the [kubectl-fdb](/kubectl-fdb) plugin when investigating issues and running imperative commands.

If a cluster is stuck in a reconciliation you can use the `kubectl-fdb` plugin to analyze the issue:

```bash
$ kubectl fdb analyze sample-cluster
Checking cluster: default/sample-cluster
✔ Cluster is available
✔ Cluster is fully replicated
✖ Cluster is not reconciled
✖ ProcessGroup: stateless-5 has the following condition: MissingProcesses since 2021-04-27 02:57:25 +0100 BST
✖ ProcessGroup: storage-2 has the following condition: MissingProcesses since 2021-03-25 07:31:08 +0000 GMT
✖ ProcessGroup: storage-2 has the following condition: PodFailing since 2021-03-25 18:23:22 +0000 GMT
✖ ProcessGroup: storage-2 has the following condition: IncorrectConfigMap since 2021-03-26 19:20:56 +0000 GMT
✖ ProcessGroup: transaction-5 has the following condition: MissingProcesses since 2021-04-27 02:57:24 +0100 BST
✖ Pod default/sample-cluster-storage-2 has unexpected Phase Pending with Reason:
✖ Pod default/sample-cluster-storage-2 has an unready container: foundationdb
✖ Pod default/sample-cluster-storage-2 has an unready container: foundationdb-kubernetes-sidecar
✖ Pod default/sample-cluster-storage-2 has an unready container: trace-log-forwarder
Error:
found issues for cluster sample-cluster. Please check them
```

The plugin can also resolve most of these issues automatically:

```bash
$ kubectl fdb analyze sample-cluster --auto-fix
Checking cluster: default/sample-cluster
✔ Cluster is available
✔ Cluster is fully replicated
✖ Cluster is not reconciled
✖ ProcessGroup: stateless-5 has the following condition: MissingProcesses since 2021-04-27 02:57:25 +0100 BST
✖ ProcessGroup: storage-2 has the following condition: MissingProcesses since 2021-03-25 07:31:08 +0000 GMT
✖ ProcessGroup: storage-2 has the following condition: PodFailing since 2021-03-25 18:23:22 +0000 GMT
✖ ProcessGroup: storage-2 has the following condition: IncorrectConfigMap since 2021-03-26 19:20:56 +0000 GMT
✖ ProcessGroup: transaction-5 has the following condition: MissingProcesses since 2021-04-27 02:57:24 +0100 BST
✖ Pod default/sample-cluster-storage-2 has unexpected Phase Pending with Reason:
✖ Pod default/sample-cluster-storage-2 has an unready container: foundationdb
✖ Pod default/examsampleple-cluster-storage-2 has an unready container: foundationdb-kubernetes-sidecar
✖ Pod default/sample-cluster-storage-2 has an unready container: trace-log-forwarder
Replace process groups [stateless-5 storage-2 transaction-5] in cluster default/sample-cluster [y/n]: y
Error:
found issues for cluster sample-cluster. Please check them
```

When using this feature, read carefully what the plugin wants to do and only confirm the dialog when you are sure that you want to do these actions.

## Pods stuck in Pending

If you have Pods that are failing to launch, because they are stuck in either a pending or terminating state, you can address that by replacing the failing instance.
You can do that using a [plugin command](#replacing-pods-with-the-kubectl-plugin).

## Replacing Pods with the kubectl plugin

Let's assume we are working with the cluster `sample-cluster`, and the pod `sample-cluster-storage-1` is failing to launch.

```bash
$ kubectl fdb remove process-groups -c sample-cluster sample-cluster-storage-1
Remove [storage-1] from cluster default/sample-cluster with exclude: true and shrink: false [y/n]:
# Confirm with 'y' if the excluded Pod is the correct one
```

Once you confirm this change, the operator begins replacing the process groups, and should be able to complete reconciliation.
If additional pods fail to launch, you can replace them with the same command.

## Exclusions Failing Due to Missing IP

If the pod does not have an IP assigned, the exclusion will not be possible, because the IP address is the only thing we can exclude on.
If this happens, you will see an error of the form `Cannot check the exclusion state of process group storage-1, which has no IP address`.
When this happens, the only way to finish the replacement is to skip the exclusion:

```bash
$ kubectl fdb remove process-groups --exclusion=false -c sample-cluster sample-cluster-storage-1
Remove [storage-1] from cluster default/sample-cluster with exclude: false and shrink: false [y/n]:
# Confirm with 'y' if the excluded Pod is the correct one
```

**NOTE**: This is a very dangerous operation.
This will delete the pod and the PVC without checking that the data has been re-replicated.
You should only due this after checking that the database is available, has not had any data loss, and that the pod is currently not running.
You can confirm the first and second check by looking at the cluster status.

## Exclusions Not Starting Due to Missing Processes

Before the operator excludes a process, it checks that the cluster will have a sufficient number of processes remaining after the exclusion. If there are too many missing processes, you will see reconciliation get requeued with a message of the form: `"Waiting for missing processes: [storage-1 storage-2 storage-3 storage-4]. Addresses to exclude: [10.1.6.69 10.1.6.68]`. When this happens, there are a few options to get reconciliation unstuck:

1. Wait for the missing processes to come online. If they are missing for a temporary reason, such as getting rescheduled or being in an initializing state, then once they come online the operator will be able to move forward with the exclusion.
2. Fix the issue with the missing processes. If the new processes are not coming up due to a configuration error or something else that is not localized to specific processes or machines, you should fix that error before doing any exclusions.
3. Replace the missing processes as well. After triggering a replacement, the operator will bring up replacement processes, and once those processes come on line all of the processes can be excluded.
4. Manually start an exclusion. You can open a CLI and run `exclude 10.1.6.69 10.1.6.68` to start an exclusion without these safety checks. Excluding too many processes can also cause further problems, such as overloading the remaining processes or leaving the database without enough workers to fulfill the required roles.

The operator will prevent excluding a process if the remaining number of processes for that process class is less than 80% of the desired number **and** the remaining number is 2+ fewer processes than the desired number.

## Reconciliation Not Running

If reconciliation is not complete, and there are no recent messages in the operator logs for the cluster, it may be that the reconciliation is backing off due to repeated failures. It should eventually retry the reconciliation. If you want to force it to run reconciliation again immediately, you can edit the cluster metadata. The operator will receive an event about the change and start reconciling. The best no-op change to make is a new annotation.

Example:

```yaml
metadata:
  annotations:
    foundationdb.org/reconcile: now
```

or simply run:

```bash
kubectl annotate fdb cluster foundationdb.org/reconcile="$(date)" --overwrite
```

## Reconciliation Not Completing

If reconciliation encounters an error in one subreconciler, it will generally stop reconciliation and not attempt to run later subreconcilers. This can cause reconciliation to fail to make progress. If you are seeing behavior, you can identify where reconciliation is getting stuck by describing the cluster and looking for events with the name `ReconciliationTerminatedEarly`. These events will have a message explaining what caused reconciliation to end. You can also look in the logs for the message `Reconciliation terminated early`. This message has a field called `subReconciler` that identifies the last subreconciler it ran and a field called `message` containing a message specific to the subreconciler. If you look for the messages preceding this one, you can often find logs from that subreconciler indicating what kind of problem it hit. You may also be able to find problems by looking for messages with the `error` level.

The `UpdatePodConfig` subreconciler can get stuck if it is unable to confirm that a pod has the latest config map contents. If this step is stuck, you can look in the logs for the message `Update dynamic Pod config` to determine what pods it is trying to update. If the pods are failing, you may need to delete them, or replace them.

The `ExcludeProcesses` subreconciler can get stuck if it needs to exclude processes, but there are processes that are not flagged for removal and are not healthy. If this step is stuck, you can look in the logs for the message `Waiting for missing processes` to determine what processes are missing. If the pods are failing, you may need to delete them, or replace them.

Any step that requires a lock can get stuck indefinitely if the locking is blocked. See the section on [Coordinating Global Operations](fault_domains.md#coordinating-global-operations) for more background on the locking system. You can see if the operator is trying to take a lock by looking in the logs for the message `Taking lock on cluster`. This will identify why the operator needs a lock. If another instance of the operator has a lock, you will see a log message `Failed to get lock`, which will have an `owner` field that tells you what instance has the lock, as well as an `endTime` field that tells you when the lock will expire. You can then look in the logs for the instance of the operator that has the lock and see if that operator is stuck in reconciliation, and try to get it unstuck. Once the operator completes reconciliation and the lock expires, your original instance of the operator should able to get the lock for itself.

## Coordinators Getting New IPs

The FDB cluster file contains a list of coordinator IPs, and if the coordinator processes are not listening on those IPs, the database will be unavailable. If you have your processes listening on their pod IPs, and a majority of the coordinator pods are deleted in a short window, the operator will not be able to automatically recover the cluster. You can fix this through a manual recovery process:

1. Identify the coordinator processes based on the IPs in the connection string, and the addresses in the process group status
2. Replace the coordinator IPs in the connection string with the new IPs for those processes
3. Edit the file `/var/fdb/data/fdb.cluster` in the `foundationdb` container in each pod to contain the new connection string, and kill the fdbserver processes.
4. Edit the `connectionString` in the cluster status, or the `seedConnectionString` in the cluster spec, to contain the new connection string.

To simplify this process, the kubectl-fdb plugin has a command that encapsulates these steps. You can run `kubectl fdb fix-coordinator-ips -c example-cluster`, and that should update everything with the modified connection string, bring the cluster back up, and allow the operator to continue with any further reconciliation work.

## Running CLI Commands

If you want to open up a shell or run a CLI, you can use the [plugin](#kubectl-fdb-plugin):

```bash
 kubectl fdb exec -c sample-cluster -- bash
```

Or if you want to check a specific pod:

```bash
kubectl exec -it sample-cluster-storage-1 -c foundationdb -- bash
```

This will open up a shell inside the container. You can use this shell to examine local files, run debug commands, or open a CLI.

When running the CLI on Kubernetes, you can simply run `fdbcli` with no additional arguments. The shell path, cluster file, TLS certificates, and any other required configuration will be supplied through the environment.

## Get the configuration string

The kubectl plugin supports to generate the configuration string from a FoundationDB cluster spec:

```bash
kubectl fdb get configuration sample-cluster
```

When the `--fail-over` flag is provided the resulting configuration string will change the priority for the primary and the remote dc.
You can also update the config directly by providing the `--update` flag to the command.
Per default a diff of the new changes will be shown before updating the cluster spec.
For an HA cluster you have to update all clusters that are managed by the operator with the same command to ensure that all operator instance want to converge to the same configuration. 

## Isolate a faulty Pod

_NOTE_: This feature requires the [unified image](./customization.md#unified-vs-split-images).
_WARNING_: By design this feature can be disruptive as the `fdbserver` process will be directly shutdown by the `fdb-kubernetes-monitor` to keep the state of the Pod.
_WARNING_: Since this mechanism is not set by the operator itself but with an annotation, that annotation will not be present if the Pod gets deleted. In the case that the Pod is not marked to be removed and not yet fully excluded, the operator will recreate the Pod. Otherwise all resources will be removed.

There might be cases where it is useful to isolate a specific Pod or a set of Pods from the cluster, e.g. in cases where a user wants to debug data corruption or networking issues without affecting the actual cluster.
In those cases a user can set the Pod annotation `foundationdb.org/isolate-process-group` to `true`.
The `fdbkubernetesmonitor` will shutdown all instances of the `fdbserver`, but the operator will not delete the resources to allow debugging.
Once the annotations are updated the user should trigger a replacement of those Pods.
The replacement will make sure that the operator is starting new Pods for the isolated Pods.
When the debugging is finished just remove the `foundationdb.org/isolate-process-group` annotation or set it to `false` and the operator will remove the associated resources.

## Operator stuck with old connection string

If more than one operator instance is used to manage a FoundationDB cluster, e.g. in case of a multi-region cluster, there can be cases where an operator instance was partitioned for a long time.
In this case the operator might not be able to connect again to the cluster because the coordinators have changed and none of the old coordinators are still running.
In this case use the [kubectl-fdb plugin](../../kubectl-fdb/Readme.md) to update the connection string in the `FoundationDBCluster` resource status and restart the operator pod.
You can use the `kubectl update connection-string` command to update the connection string for a cluster.

## Next

You can continue on to the [next section](more.md) or go back to the [table of contents](index.md).
