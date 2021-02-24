# Process Groups as CRD

## Metadata

* Authors: @johscheuer
* Created: 2021-02-24
* Updated: 2021-02-24

## Background

In one of our latest design changes we added the process [group status](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/master/docs/design/process_group_status.md).
This design added multiple information about a process group (better known as instance) and the current state into the FDB cluster custom resource.
Adding to much information into the status of an object has several drawbacks and should normally avoided:

- The status can get huge for big clusters and probably exceed the limitation of etcd (`1.5 MiB`).
- We can see some conflicts in our operator because multiple reconcile loops try to modify the status.
- It's not trivial to change the status [319](https://github.com/FoundationDB/fdb-kubernetes-operator/issues/319#issuecomment-705870062).
- We could split up some of the controller work to make each reconcile phase simpler.
- Currently we have to maintain a `instancesToRemove` and `instancesToRemoveWithoutExclusion` list to remove `process groups`.
- In our code we have sometimes rather complex control loops where we need to filter for process class etc. which could be done by the Kubernetes API.

## General Design Goals

- Move the process group information out of the stats in it's own CRD.
- Remove the need of the `instancesToRemove` and `instancesToRemoveWithoutExclusion` list.
- Split up the controller into an additional controller to manage the Kubernetes resource reconcilation.
- Prevent that the FDB cluster custom resource grows to much.

## Proposed Design

Instead of adding to much information into the `status` of the FDB cluter CRD we add an additional intermediate CRD.
The `FDBProcessGroup` CRD will represent a process group in FDB and will maintain the expected state of the process group.
The FDB cluster controller will only create/delete/modify `FDBProcessGroup` and an additional controller (also part of the fdb operator binary) will then trigger more actions.
The `FDBProcessGroup` controller will handle the following operations:

- create/delete/modify PVCs for a process group
- create/delete/modify Pods for a process group
- create/delete/modify Services for a process group

The `FDBProcessGroup` will contain a status subresource to indicate if each resource was sucessfully created and is ready.
Additional to the fields above in the `FDBProcessGroup` status it will contain the public IP address of the `FDBProcessGroup`, this can be the Pod IP or the Service IP.
The `FDBProcessGroup` CRD will get some of fields that are currently represented in the FDB cluster CRD e.g. similar to a `ReplicaSet` that mirrors some of th fields from a `Deployment`:

- The final [ProcessSettings](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/master/docs/cluster_spec.md#processsettings) for the specific process.
- `instanceIDPrefix` for the created resources.
- `updatePodsByReplacement`.
- Part of the [ServiceConfig](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/master/docs/cluster_spec.md#serviceconfig) once we support to override fields in the created services.
- `storageServersPerPod`.
- `logGroup`.
- `sidecarVariables`.

If one of the fields above are changed this will trigger a reconcilation in the `FDBProcessGroup` for the specific sub-controller.
Additional to the fields above that are taken from the `FDB cluster spec` we will add the fields from [ProcessGroupStatus](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/master/docs/cluster_spec.md#processgroupstatus) to the Spec:

- `processClass`
- `remove`
- `excluded`
- `exclusionSkipped`

`addresses` is already included in the status field and the `processGroupID` should be used as the name of the `FDBProcessGroup`.
Currently we have the following `ProcessGroupConditions` as part of the `ProcessGroupStatus`:

```go
const (
	// NotConnecting represents a process group that doesn't connect to the cluster.
	NotConnecting ProcessGroupConditionType = "NotConnecting"
	// IncorrectPodSpec represents a process group that has an incorrect Pod spec.
	IncorrectPodSpec ProcessGroupConditionType = "IncorrectPodSpec"
	// IncorrectConfigMap represents a process group that has an incorrect ConfigMap.
	IncorrectConfigMap ProcessGroupConditionType = "IncorrectConfigMap"
	// IncorrectCommandLine represents a process group that has an incorrect commandline configuration.
	IncorrectCommandLine ProcessGroupConditionType = "IncorrectCommandLine"
	// PodFailing represents a process group which Pod keeps failing.
	PodFailing ProcessGroupConditionType = "PodFailing"
	// MissingPod represents a process group that doesn't have a Pod assigned.
	MissingPod ProcessGroupConditionType = "MissingPod"
	// MissingPVC represents a process group that doesn't have a PVC assigned.
	MissingPVC ProcessGroupConditionType = "MissingPVC"
	// MissingService represents a process group that doesn't have a Service assigned.
	MissingService ProcessGroupConditionType = "MissingService"
	// MissingProcesses represents a process group that misses a process.
	MissingProcesses ProcessGroupConditionType = "MissingProcesses"
)
```

Some of them are currently unused and most of them can simply be replaced by the `FDBProcessGroup` status.
During the reconcilation the `FDB cluster controller` will only evaluate the state of the `FDBProcessGroup` and compare it with the current database state.
Each `FDBProcessGroup` will have the `fdb-process-class` and `fdb-cluster-name`  to make if simpler to filter only for the relevant `FDBProcessGroup`.
Instead of using the `fdb-instance-id` label we will name the `FDBProcessGroup` accordingly.

This change has the benefit that we can separate some of the tasks handled by the controller and prevent some conflicts:

- The `FDB cluster controller` will read-write the `FDBProcessGroup.Spec` and read-only the `FDBProcessGroup.Status`.
- The `FDB ProcessGroup controller` will read-only `FDBProcessGroup.Spec` and read-write the `FDBProcessGroup.Spec`.

In this case we prevent conflicts since each controller takes care of either the `Spec` or the `Status` but not both.
This will be major change and will require some refactoring at multiple places.

## Related Links

This touches on multiple recent areas of work:

* [Tracking Process Groups in the Status](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/master/docs/design/process_group_status.md)
