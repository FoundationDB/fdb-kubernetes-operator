# Common Operations

## Common localities

The operator sets per default a predefined set of [localities](https://apple.github.io/foundationdb/configuration.html#fdbserver-section) based on the FoundationDBCluster configuration.
The following localities will be set by the operator:

- `--locality_instance_id`: This will get the value from `FDB_INSTANCE_ID` and will represent the process group ID, e.g. `storage-1`.
- `--locality_process_id`: This will only be set if `storageServersPerPod` is set to a value larger than `1`. The format will be `$FDB_INSTANCE_ID` with the process counter as suffix and a `-` as a separator, e.g. `storage-1-1`.
- `--locality_machineid`: The value will be set depending on the fault domain key. For `foundationdb.org/none`, this will be the Pod's name, for all other cases this will be the node name on which the pod is running.
- `--locality_zoneid`: The value will be set depending on the fault domain key. For `foundationdb.org/none`, this will be the Pod's name, otherwise this will be the node name per default where the Pod is running. If `ValueFrom` is defined in the fault domain this value will be used. If `foundationdb.org/kubernetes-cluster` is specified as fault domain key the predefined `value` will be used.
- `--locality_dcid`: This value will be set to the value defined in `cluster.Spec.DataCenter`, if this value is not set the locality will not be set. This locality is used for FoundationDB deployments in multiple datacenters/Kubernetes clusters.
- `--locality_data_hall`: This value will be set to the value defined in `cluster.Spec.DataHall`, if this value is not set the locality will not be set. Currently this locality doesn't have any affect, but will be used in the future for `three_data_hall` replication.
- `--locality_dns_name`: This value will only be set if `cluster.Spec.Routing.DefineDNSLocalityFields` is set to true. The value will be set to the `FDB_DNS_NAME` environment variable, which is set by the operator.

The operator uses the `locality_instance_id` to identify the process from the [machine-readable status](https://apple.github.io/foundationdb/mr-status.html) and match it to the according process group managed by the operator.

## Replacing a Process

If you delete a pod, the operator will automatically create a new pod to replace it. If there is a volume available for re-use, we will create a new pod to match that volume. This means that in general you can replace a bad process just by deleting the pod. This may not be desirable in all situations, as it creates a loss of fault tolerance until the replacement pod is created. This also requires that the original volume be available, which may not be possible in some failure scenarios.

As an alternative, you can replace a pod by explicitly placing it in the `processGroupsToRemove` list:

```yaml
apiVersion: apps.foundationdb.org/v1beta2
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 7.1.26
  processGroupsToRemove:
    - storage-1
```

When comparing the desired process count with the current pod count, any pods that are in the pending removal list are not counted.
This means that the operator will only consider there to be 2 running storage pods, rather than 3, and will create a new one to fill the gap.
Once this is done, it will go through the same removal process described under [Shrinking a Cluster](scaling.md#shrinking-a-cluster).
The cluster will remain at full fault tolerance throughout the reconciliation.
This allows you to replace an arbitrarily large number of processes in a cluster without any risk of availability loss.

## Adding a Knob

To add a knob, you can change the `customParameters` in the cluster spec:

```yaml
apiVersion: apps.foundationdb.org/v1beta2
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 7.1.26
  processes:
    general:
      customParameters:
      - "knob_always_causal_read_risky=1"
```

The operator will update the monitor conf to contain the new knob, and will then bounce all of the `fdbserver` processes.
As soon as `fdbmonitor` detects that the `fdbserver` process has died, it will create a new `fdbserver` process with the latest config.
The cluster should be fully available within 10 seconds of executing the bounce, though this can vary based on cluster size and the resources provided to the `fdbserver` processes.

The process for updating the monitor conf can take several minutes, based on the time it takes Kubernetes to update the config map in the pods.

_NOTE_:

- The custom parameters must be unique and duplicate entries for the same process class will lead to a failure.
- The custom parameters will not be merged together. You have to define the full list of all custom parameters for all process classes.
- Only custom parameters from the `[fdbserver]` section are support. The operator doesn't support changes to the [[fdbmonitor] and [general] section](https://apple.github.io/foundationdb/configuration.html#general-section).

## Upgrading a Cluster

To upgrade a cluster, you can change the version in the cluster spec:

```yaml
apiVersion: apps.foundationdb.org/v1beta2
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 7.3.33
```

The supported versions of the operator are documented in the [compatibility](../compatibility.md) guide.
Downgrades of patch versions are supported, but not for major or minor versions.

For version upgrades from 7.1+ to another major or minor versions the client compatibility check might requires some additional configuration:

```yaml
apiVersion: apps.foundationdb.org/v1beta2
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 7.3.33
  automationOptions:
    ignoreLogGroupsForUpgrade:
      - fdb-kubernetes-operator
```

The default value for the log group of the operator is `fdb-kubernetes-operator` but can be changed by setting the environment variable `FDB_NETWORK_OPTION_TRACE_LOG_GROUP`.
The operator version `v1.19.0` and never sets this value as a default and those changes are not required.

The upgrade process is described in more detail in [upgrades](./upgrades.md).

## Renaming a Cluster

The name of a cluster is immutable, and it is included in the names of all of the dependent resources, as well as in labels on the resources.
If you want to change the name later on, you can do so with the following steps.
This example assumes you are renaming the cluster `sample-cluster` to `sample-cluster-2`.

1.  Create a new cluster named `sample-cluster-2`, using the current connection string from `sample-cluster` as its seedConnectionString. You will need to set a `processGroupIdPrefix` for `sample-cluster-2` to be different from the `processGroupIdPrefix` for `sample-cluster`, to make sure that the process group IDs do not collide. The rest of the spec can match `sample-cluster`, other than any fields that you want to customize based on the new name. Wait for reconciliation on `sample-cluster-2` to complete.
2.  Update the spec for `sample-cluster` to set the process counts for `log`, `stateless`, and `storage` to `-1`. You should omit all other process counts. Wait for the reconciliation for `sample-cluster` to complete.
3.  Check that none of the original pods from `sample-cluster` are running.
4.  Delete the `sample-cluster` resource.

At that point, you will be left with just the resources for `sample-cluster-2`.
You can continue performing operations on `sample-cluster-2` as normal.
You can also change or remove the `processGroupIdPrefix` if you had to set it to a different value earlier in the process.

_NOTE_: This will double the size of the cluster for some time, as this performs a migration from the old pods to the new desired pods.

## Sharding for the operator

The operator supports the `--label-selector` flag to select only a subset of clusters to manage.
The label selector can be useful for sharding multiple operators in addition to add more concurrent reconcile loops with the `--max-concurrent-reconciles` flag.
A human operator can operate multiple FDB operator in the same namespace with that approach or multiple global FDB operators.
When using a label selector you must ensure that your FDB custom resources like the `FoundationDBCluster` has the required label.
In addition to that you must ensure that you add the required labels in the `resourceLabels` of the `labels` section in the `FoundationDBCluster` otherwise the operator will ignore events from the created resources.
For more information how to add additional labels to the resources managed by the operator refer to the [Resource Labeling](customization.md#resource-labeling) section.

## Maintenance

FDB has a feature called [maintenance mode](https://github.com/apple/foundationdb/wiki/Maintenance-mode), which allows the user to let FDB know that a set of storage servers are expected to be taken offline.
Using the maintenance mode brings the benefit that the data movement is not triggered for the zone under maintenance.
During upgrades or rollouts, this can reduce the unnecessary data movement.

The operator supports two integration modes for the maintenance mode:

1. The operator will set the maintenance mode when at least one storage Pod is taken down to be recreated and the operator will reset the maintenance mode once all processes (Pods) have been restarted.
2. The operator will only reset the maintenance mode when all processes have been restarted.

The 2. case can make sense if you have another system managing the Kubernetes node upgrades or if you have another component that takes care of the recreation of Pods.
In most cases a user wants to make use of 1. as this offers the same integrations as 2. but also makes sure that the operator sets the maintenance mode before recreating storage Pods.

```yaml
spec:
  automationOptions:
    maintenanceModeOptions:
      # Enables option 1
      UseMaintenanceModeChecker: true
      # Will enable option 2, is implicitly true if UseMaintenanceModeChecker is true
      resetMaintenanceMode: true
```

### Internals

Before the operator recreates a storage Pod it will first update the list of process groups under maintenance in the FDB cluster by adding the following values:

```text
\xff\x02/org.foundationdb.kubernetes-operator/maintenance/<process-groupd-id> <unix-timestamp>
```

For every process group that will be taken down the operator will add an entry.
The `unix-timestamp` will be the current time, or the time when the maintenance is expected to happen.
A user can modify the prefix by setting a different value as [lockKeyPrefix](../cluster_spec.md#lockoptions), this value will be appended by `/maintenance/`.
After this the operator sets the maintenance mode for the zone of the storage Pods.

In the [maintenance mode checker](../../controllers/maintenance_mode_checker.go) the operator will evaluate the current list of processes under maintenance, based a range read over the maintenance key space.
The [GetMaintenanceInformation method](../../internal/maintenance/maintenance.go) will check the status of those entries, there are the possible outcomes for an entry:

1. The process has not yet restarted, either the maintenance action is still pending or is currently in progress (process has not be restarted).
2. The maintenance on the process is done, this is discovered by a reporting process in the machine-readable status that was restarted after the maintenance timestamp.
3. The entry is stale, all entries that are in the list for a longer time, default is 4h, will be removed to make sure old entries are removed.

All processes that have finished the maintenance and the stale entries will be removed from that key space.
Processes that have not yet finished their maintenance will stay untouched.
If a maintenance zone is active and some processes have not yet finished the operator will requeue a reconciliation and wait until all processes are done.
If all processes have finished their maintenance and a maintenance zone is active the operator will reset the maintenance zone.

### External integration

Depending on your Kubernetes setup, you might be able to use this integration during Kubernetes node upgrades.
You can either use the [SetProcessesUnderMaintenance](../../fdbclient/admin_client.go) implementation or do the according fdbcli call.
If you want to perform the fdbcli call programmatically you can take a look at the [the operator is allowed to reset the maintenance zone](../../e2e/test_operator/operator_test.go) test case.

For the non-storage processes, you should consider to cordon the node before taking it down for maintenance.
You can use the [kubectl-fdb cordon](../../kubectl-fdb/Readme.md) for that.
This will make sure that the processes are proactively excluded, instead of waiting for the FDB failure monitor to discover the failure.

_NOTE_: You should always set the processes under maintenance before setting the maintenance mode. See [Internals](#internals) for more details.

## Delaying the shutdown of the Pod

When using the [unified image](./customization.md#unified-vs-split-images) the `fdb-kubernetes-monitor` supports to delay the shutdown of itself.
This feature can help to reduce the risk of race conditions in the different checks of the operator and FDB itself.
Right now the operator performs a different set of checks before updating (deleting and creating) Pods, like a fault tolerance check.
In some rare cases there could be a risk where a Pod is already deleted and the FDB cluster haven't detected the failed servers yet.
Per default the failure detection mechanism of FDB takes 60 seconds to mark a storage server as failed.
The idea of the delayed shutdown feature is to shutdown the fdbserver processes, but keep the `fdb-kubernetes-monitor` process running for the specified `foundationdb.org/delay-shutdown`.
This will add some additional delay to the Pod deletion and will reduce the risk of race conditions, e.g. by detecting the failed fdbserver process(es) and reduce the current fault tolerance.
The value of the `foundationdb.org/delay-shutdown` annotation must be a valid [golang duration](https://pkg.go.dev/time#ParseDuration).
Setting the `foundationdb.org/delay-shutdown` annotation to a value greater than 60 seconds will give the FDB cluster additional time to detect the failure before shutting down the actual Pod.
The value for `foundationdb.org/delay-shutdown` and `terminationGracePeriodSeconds` in the [Pod spec](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-termination) should either match or the `terminationGracePeriodSeconds` should be slightly higher than the delay shutdown.
The value for `foundationdb.org/delay-shutdown` should also not to high as this will slow down the rollout process and might be prevented by the Kubernetes cluster.

### Risks and limitations

There are a few risks and limitations to the current implementation:

1. If a process is crashing/restarting during the maintenance operation it could lead to a case where the operator is releasing the maintenance mode earlier than it should be.

The current risks are limited to releasing the maintenance mode earlier than it should be.
In this case data-movement will be triggered for the down processes after 60 seconds, the data-movement shouldn't cause any operational issues.

## Recover Lost Quorum Of Coordinators

If the coordinators Pods are still running but they got new IP addresses, read [Coordinators Getting New IPs](./debugging.md#coordinators-getting-new-ips).
In case you lost the quorum of coordinators and you are not able to restore the coordinator state, you have two options:

1. Recover the cluster from the latest backup.
1. Recover the coordinator state with the risk of data loss.

This section will describe the procedure for case 2.

**NOTE** The assumption here is that at least one coordinator is still available with its coordinator state.
**NOTE** This action can cause data loss. Perform those actions with care.

- Set all the `FoundationDBCluster` resources for this FDB cluster to `spec.Skip = true` to make sure the operator is not changing the manual changed state. 
- Fetch the last connection string from the `FoundationDBCluster` status, e.g. `kubectl get fdb ${cluster} -o jsonpath='{ .status.connectionString }'`. 
- Copy the coordinator state from one of the running coordinators to your local machine:

```bash
kubectl cp ${coordinator-pod}:/var/fdb/data/1/coordination-0.fdq ./coordination-0.fdq 
kubectl cp ${coordinator-pod}:/var/fdb/data/1/coordination-1.fdq ./coordination-1.fdq
```
 
- Select a number of Pods you want to use as new coordinators and copy the files to those pods:

```bash
kubectl cp ./coordination-0.fdq ${new-coordinator-pod}:/var/fdb/data/1/coordination-0.fdq 
kubectl cp ./coordination-1.fdq ${new-coordinator-pod}:/var/fdb/data/1/coordination-1.fdq 
```

- Update the `ConfigMap` to contain the new connection string, the new connection string must contain the still existing coordinators and the new coordinators. The old entries must be removed.
- Wait ~1 min until the `ConfigMap` is synced to all Pods, you can check the `/var/dynamic-conf/fdb.cluster` inside a Pod if you are unsure.
- Now all Pods must be restarted and the previous local cluster file must be deleted to make sure the fdbserver is picking the connection string from the seed cluster file (`/var/dynamic-conf/fdb.cluster`).

```bash
for pod in $(kubectl get po -l foundationdb.org/fdb-cluster-name=${cluster} -o name --no-headers);
do
    echo $pod
    kubectl $pod -- bash -c 'pkill fdbserver && rm -f /var/fdb/data/fdb.cluster && pkill fdbserver'
done
```

If the cluster is a multi-region cluster, perform this step for all running regions.

- Now you can exec into a container and use `fdbcli` to connect to the cluster.
- If you use a multi-region cluster you have to issue `force_recovery_with_data_loss`.
- Update the `FoundationDBCluster` `seedConnectionString` under `spec.seedConnectionString` with the new connection string.
- Now you can set `spec.Skip = false` to let the operator take over again.
- Depending on the state of the multi-region cluster, you probably want to change the desired database configuration to drop ha.

## Next

You can continue on to the [next section](scaling.md) or go back to the [table of contents](index.md).
