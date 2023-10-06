# Replacements and Deletions

The operator has two different strategies it can take on process groups that are in an undesired state: replacement and deletion.
In the case of replacement, we will create a brand new process group, move data off the old process group, and delete the resources for the old process group as well as the records of the process group itself.
In the case of deletion, we will delete some or all of the resources for the process group and then create new objects with the same names.
We will cover details of when these different strategies are used in later sections.

A process group is marked for replacement by setting the `remove` flag on the process group.
This flag is used during both replacements and shrinks, and a replacement is modeled as a grow followed by a shrink.
Process groups that are marked for removal are not counted in the number of active process groups when doing a grow, so flagging a process group for removal with no other changes will cause a replacement process to be added.
Flagging a process group for removal when decreasing the desired process count will cause that process group specifically to be removed to accomplish that decrease in process count.
Decreasing the desired process count without marking anything for removal will cause the operator to choose process groups that should be removed to accomplish that decrease in process count.

In general, when we need to update a pod's spec we will do that by deleting and recreating the pod.
There are some changes that we will roll out by replacing the process group instead, such as changing a volume size.
There is also a flag in the cluster spec called `podUpdateStrategy` that will cause the operator to always roll out changes to Pod specs by replacement instead of deletion, either for all Pods or only for transaction system Pods.

The following changes can only be rolled out through replacement:

* Changing the process group ID prefix
* Changing the public IP source
* Changing the number of storage servers per pod
* Changing the node selector
* Changing any part of the PVC spec
* Increasing the resource requirements, when the `replaceInstancesWhenResourcesChange` flag is set.

The number of inflight replacements can be configured by setting `maxConcurrentReplacements`, per default the operator will replace all misconfigured process groups.
Depending on the cluster size this can require a quota that is has double the capacity of the actual required resources.

## Using The Maintenance Mode

The FoundationDB Kubernetes operator supports to make use of the [maintenance mode](https://github.com/apple/foundationdb/wiki/Maintenance-mode) in FoundationDB.
Using the maintenance mode in FoundationDB will reduce the data distribution and disruption when Storage Pods must be updated.
The following addition to the `FoundationDBCluster` resource will enable the maintenance mode for this cluster:

```yaml
spec:
    automationOptions:
      maintenanceModeOptions:
        UseMaintenanceModeChecker: true
```

Only Pods that are updated (deleted and recreated) will be considered during the maintenance mode.

**NOTE** The maintenance mode feature is relatively new and has limited e2e test coverage and should therefore used with care.

## Automatic Replacements for ProcessGroups in Undesired State

The operator has an option to automatically replace pods that are in a bad state. This behavior is disabled by default, but you can enable it by setting the field `automationOptions.replacements.enabled` in the cluster spec.
This will replace any pods that meet the following criteria:

* The process group has a condition that is eligible for replacement, and has been in that condition for 7200 seconds. This time window is configurable through `automationOptions.replacements.failureDetectionTimeSeconds`.
* The number of process groups that are marked for removal and not fully excluded, counting the process group that is being evaluated for replacement, is less than or equal to 1. This limit is configurable through `automationOptions.replacements.maxConcurrentReplacements`.

The following conditions are currently eligible for replacement:

* `MissingProcesses`: This indicates that a process is not reporting to the database.
* `PodFailing`: This indicates that one of the containers is not ready.
* `MissingPod`: This indicates a process group that doesn't have a Pod assigned.
* `MissingPVC`: This indicates that a process group that doesn't have a PVC assigned.
* `MissingService`: This indicates that a process group that doesn't have a Service assigned.
* `PodPending`: This indicates that a process group where the Pod is in a pending state.
* `NodeTaintReplacing`: This indicates a process group where the Pod has been running on a tainted Node for at least the configured duration. If a ProcessGroup has the `NodeTaintReplacing` condition, the replacement cannot be stopped, even after the Node taint was removed.
* `ProcessIsMarkedAsExcluded`: This indicates a process group where at least on process is excluded. If the process group is not marked as removal, the operator will replace this process group to make sure the cluster runs at the right capacity.

Process groups that are set into the crash loop state with the `Buggify` setting won't be replaced by the operator.
If the `cluster.Spec.Buggify.EmptyMonitorConf` setting is active the operator won't replace any process groups.

## Automatic Replacements for ProcessGroups on Tainted Nodes

The operator has an option to automatically replace ProcessGroups where the associated Pod is running on a tainted Node.
This feature is disabled by default, but can be enabled by setting `automationOptions.replacements.taintReplacementOptions`.
If you want to enable this feature you should set the `--enable-node-index` command line flag to allow the operator to access the nodes with an index.

We use three examples below to illustrate how to set up the feature.

### Example Setup 1

The following YAML setup lets the operator detect Pods running on Nodes with taint key `example.com/maintenance`, set the ProcessGroup' condition to `NodeTaintReplacing`, if their Nodes have been tainted for 3600 seconds, and replace the Pods after 1800 seconds.

```yaml
spec:
    automationOptions:
      replacements:
        taintReplacementOptions:
        - Key: example.com/maintenance
          DurationInSeconds: 3600
        taintReplacementTimeSeconds: 1800
        enabled: true
```

If there are multiple Pods on tainted Nodes, the operator will simultaneously replace at most `automationOptions.replacements.maxConcurrentReplacements` Pods.

### Example Setup 2

We can enable the taint feature on all taint keys except one taint key with the following  configuration:

```yaml
spec:
    automationOptions:
      replacements:
        taintReplacementOptions:
        - Key: "*"
          DurationInSeconds: 3600
        - Key: example.com/taint-key-to-ignore
          DurationInSeconds: 9223372036854775807
        enabled: true
```

The operator will detect and mark all Pods on tainted Nodes with `NodeTaintDetected` condition. But the operator will ignore the taint key `example.com/taint-key-to-ignore` when it adds `NodeTaintReplacing` condition to Pods, because the key's `DurationInSeconds` is set to max of int64. For example, if a Node has only the taint key `example.com/taint-key-to-ignore`, its Pods will only be marked with  `NodeTaintDetected` condition. When the Node has another taint key, say `example.com/any-other-key`, its Pods will be added `NodeTaintReplacing` condition when the other taint key has been on the Node for 3600 seconds.

### Example Setup 3

We can disable the taint feature by resetting `automationOptions.replacements.taintReplacementOptions = {}`. The following example YAML config deletes the `taintReplacementOptions` section.

```yaml
spec:
    automationOptions:
      replacements:
        enabled: true
```

## Enforce Full Replication

The operator only removes ProcessGroups when the cluster has the desired fault tolerance and is available. This is enforced by default in 1.0.0.

## Exclusion strategy of the Operator

See: [Technical Design: Exclude Processes](technical_design.md#excludeprocesses)

## Deletion mode

The operator supports different deletion modes (`All`, `Zone`, `ProcessGroup`).
The default deletion mode is `Zone`.

* `All` will delete all pods at once.
* `Zone` deletes all Pods in fault domain at once.
* `ProcessGroup` delete one Pod at a time.

Depending on your requirements and the underlying Kubernetes cluster you might choose a different deletion mode than the default.

## Limit Zones (fault domains) with Unavailable Pods

The operator allows to limit the number of zones with unavailable pods during deletions. This is configurable through `maxZonesWithUnavailablePods` in the cluster spec. Which is disabled by default. When enabled the operator will wait before deleting pods if the number of zones with unavailable pods is higher than the configured value and the pods to update do not belong to any of the zones with unavailable pods. This is useful to avoid deleting too many pods from different zones at once when recreating pods is not fast enough.

## Next

You can continue on to the [next section](fault_domains.md) or go back to the [table of contents](index.md).
