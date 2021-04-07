# Process Groups as CRD

## Metadata

* Authors: @johscheuer
* Created: 2021-02-24
* Updated: 2021-03-29

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
The `FoundationDBProcessGroup` CRD will represent a process group in FDB and will maintain the expected state of the process group.
The FDB cluster controller will only create/delete/modify `FoundationDBProcessGroup` and an additional controller (also part of the fdb operator binary) will then trigger more actions.
The `FoundationDBProcessGroup` controller will handle the following operations:

- create/delete/modify PVCs for a process group
- create/delete/modify Pods for a process group
- create/delete/modify Services for a process group

The `FoundationDBProcessGroup` will contain a status subresource to indicate if each resource was sucessfully created and is ready.
Additional to the fields above in the `FoundationDBProcessGroup` status it will contain the public IP address of the `FoundationDBProcessGroup`, this can be the Pod IP or the Service IP.
The `FoundationDBProcessGroup` CRD will get some of fields that are currently represented in the FDB cluster CRD e.g. similar to a `ReplicaSet` that mirrors some of th fields from a `Deployment`:

- The final [ProcessSettings](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/master/docs/cluster_spec.md#processsettings) for the specific process.
- `instanceIDPrefix` for the created resources.
- Part of the [ServiceConfig](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/master/docs/cluster_spec.md#serviceconfig) once we support to override fields in the created services.
- `storageServersPerPod`.
- `logGroup`.
- `sidecarVariables`.

If one of the fields above are changed this will trigger a reconcilation in the `FoundationDBProcessGroup` for the specific sub-controller.
Additional to the fields above that are taken from the `FDB cluster spec` we will add the fields from [ProcessGroupStatus](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/master/docs/cluster_spec.md#processgroupstatus) to the Spec:

- `processClass`
- `remove`
- `excluded`
- `exclusionSkipped`

`addresses` is already included in the status field and the `processGroupID` will be set as a label.
The name of the `FoundationDBProcessGroup` will be analogous to the current `Pod` name.
We have the following `ProcessGroupConditions` as part of the `ProcessGroupStatus`:

```go
const (
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

The following `ProcessGroupConditions` will be replaced by the `FoundationDBProcessGroup` status.
During the reconcilation the `FDB cluster controller` will only evaluate the state of the `FoundationDBProcessGroup` and compare it with the current database state.
Each `FoundationDBProcessGroup` will have the `fdb-process-class` and `fdb-cluster-name`  to make if easier to filter only for the relevant `FoundationDBProcessGroup`.
Instead of using the `fdb-instance-id` label we will name the `FoundationDBProcessGroup` accordingly.
We have to evaluate during the implementation if the load on the FDB side will significantly increase if we reconcile for each `FoundationDBProcessGroup`.
If we see a significant increase of the load we can add a cache to the controller to prevent calling the FoundationDB status multiple times.
As an optional feature the cluster controller could annotate all `FoundationDBProcessGroup` of the current cluster to enforce reconcilation.

This change has the benefit that we can separate the tasks handled by the different controllers and prevent some conflicts:

- The `FDB cluster controller` will read-write the `FoundationDBProcessGroup.Spec` and read-only the `FoundationDBProcessGroup.Status`.
- The `FDB ProcessGroup controller` will read-only `FoundationDBProcessGroup.Spec` and read-write the `FoundationDBProcessGroup.Spec`.

In this case we prevent conflicts since each controller takes care of either the `Spec` or the `Status` but not both.
This will be major change and will require some refactoring at multiple places.

The `FDB cluster controller` will be still in charge of `excluding`, `including` and `removeing` `ProcessGroups`. 
Similar to the current implementation the `FDB cluster controller`  will exclude processes that should be removed.
When the `processes` are successfully removed the `FoundationDBProcessGroup` will be removed and once the `FoundationDBProcessGroup` is removed it will be included.

## Related Links

This touches on multiple recent areas of work:

* [Tracking Process Groups in the Status](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/master/docs/design/process_group_status.md)
