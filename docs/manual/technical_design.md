# Technical Design

## Overview

This document aims to provide more technical details about how the operator works to help people who are using the operator to understand its operations and debug problems that they experience.

The operator is built using [Kubebuilder](https://book.kubebuilder.io), and this document will refer to concepts from Kubebuilder as well as the [Kubernetes core](https://kubernetes.io/docs/home/).

There are two main pieces to the operator: the **Custom Resource Definition** and the **Controller**. The Custom Resource Definition provides a schema for defining objects that represent a FoundationDB cluster. Users of the operator create Custom Resources using this schema. The Controller watches for events on these Custom Resources and performs operations to reconcile the running state with the desired state that is expressed in that resource.

Our operator currently uses the following custom resource definitions:

* [FoundationDBCluster](../cluster_spec.md)
* [FoundationDBBackup](../backup_spec.md)
* [FoundationDBRestore](../restore_spec.md)

The documents linked above contain the full specification of these resource definitions, so they may be a useful reference for the fields that we refer to in this document.

All of these resources are managed by a single controller, with a single deployment. Within the controller, we have separate reconciliation logic for each resource type.

When we use the term "cluster" in this document with no other qualifiers, we are referring to a FoundationDB cluster. We will refer to a Kubernetes Cluster as a "KC" for brevity, and to avoid overloading the word "cluster".

When we use the term "cluster status" in this document, it refers to the status of the `FoundationDBCluster` resource in Kubernetes. When we use the term "database status" in this document, it refers to the output of the `status json` command in `fdbcli`.

## Reconciliation Loops

The operations of our controller are structured as a reconciliation loop. At a high level the reconciliation loop works as follows:

1. The controller receives an update to the spec for a custom resource.
1. The controller identifies what needs to change in the running state to match that spec.
1. The controller makes whatever changes it can to converge toward the desired state.
1. If the controller encounters an error or needs to wait, it requeues the reconciliation and tries again later.
1. Once the running state matches the desired state, the controller marks the reconciliation as complete.

There are important constraints on how reconciliation has to work within this model:

* We have to be able to start reconciliation over from the beginning, which means changes must be idempotent.
* All Kubernetes resources are fetched through a local cache, which means all reads are potentially stale.
* Any local state that is not saved in a Kubernetes resource or the database may be lost at any time.

In our operator, we add an additional abstraction to help structure the reconciliation loop, which we call a **Subreconciler**. A subreconciler represents a self-contained chunk of work that brings the running state closer to the spec. Each subreconciler receives the latest custom resource, and is responsible for determining what actions if any need to be run for the activity in its scope. We run every subreconciler for every reconciliation, with the subreconcilers taking care of logic to exit early if they do not have any work to do.

## Cluster Reconciliation

The cluster reconciler runs the following subreconcilers:

1. UpdateStatus
1. UpdateLockConfiguration
1. UpdateConfigMap
1. CheckClientCompatibility
1. ReplaceMisconfiguredPods
1. ReplaceFailedPods
1. DeletePodsForBuggification
1. AddProcessGroups
1. AddServices
1. AddPVCs
1. AddPods
1. GenerateInitialClusterFile
1. UpdateSidecarVersions
1. UpdatePodConfig
1. UpdateLabels
1. UpdateDatabaseConfiguration
1. ChooseRemovals
1. ExcludeInstances
1. ChangeCoordinators
1. BounceProcesses
1. UpdatePods
1. RemoveServices
1. RemovePods
1. UpdateStatus (again)

### Tracking Reconciliation Stages

We track the progress of reconciliation through a `GenerationStatus` object, in the `status.generationStatus` field in the cluster object. The generation status has fields within it that indicate how far reconciliation has gotten, with an integer for each field indicating the generation that was seen for that reconciliation. The most important field to track here is the `reconciled` field, which is set when we consider reconciliation _mostly_ complete. If you want to track a rollout, you can check for whether the generation number in `status.generationStatus.reconciled` is equal to the generation number in `metadata.generation`.

There are some cases where we set the `reconciled` field to the current generation even though we are requeuing reconciliation and continuing to due more work. These cases are listed below:

1. Pods are in terminating. If we have fully excluded processes and have started the termination of the pods, we set both `reconciled` and `hasPendingRemoval` to the current generation. Termination cannot complete until the kubelet confirms the processes has been shut down, which can take an arbitrary long period of time if the kubelet is in a broken state. The processes will remain excluded until the termination completes, at which point the operator will include the processes again and the `hasPendingRemoval` field will be cleared. In general it should be fine for the cluster to stay in this state indefinitely, and you can continue to make other changes to the cluster. However, you may encounter issues with the stuck pods taking up resource quota until they are fully terminated.

### Process Groups

Inside the cluster status, we track an object called `ProcessGroup` which loosely correponds to a pod in Kubernetes. A process group represents a set of processes that will run inside a single container. In the default case we run one `fdbserver` process in each process group, but if you configure your cluster to run multiple storage servers per disk then we will have multiple storage server processes inside a single process group, with each process group having its own disk. We use the process group to track information about processes that lives outside the lifecycle of any other Kubernetes object, such as an intention to remove the process or adverse conditions that the operator needs to remediate.

### Resources Created

The operator creates the following resources for a FoundationDB cluster:

* `ConfigMap`: The operator creates one config map for each cluster that holds configuration like the cluster file and the fdbmonitor conf files.
* `Service`: By default, the operator creates no services. You can configure a cluster-wide headless service for DNS lookup, and you can configure a per-process-group service that can be used to provide the public IP for the processes, as an alternative to the default behavior of using the pod IP as the public IP.
* `PersistentVolumeClaim (PVC)`:  We create one persistent volume claim for every stateful process group.
* `Pod`: We create one pod for every process group, with one container for starting fdbmonitor and one container for starting a helper sidecar.

### Replacement and Deletion

The operator has two different strategies it can take on process groups that are in an undesired state: replacement and deletion. In the case of replacement, we will create a brand new process group, move data off the old process group, and delete the resources for the old process group as well as the records of the process group itself. In the case of deletion, we will delete some or all of the resources for the process group and then create new objects with the same names. We will cover details of when these different strategies are used in later sections.

A process group is marked for replacement by setting the `remove` flag on the process group. This flag is used during both replacements and shrinks, and a replacement is modeled as a grow followed by a shrink. Process groups that are marked for removal are not counted in the number of active process groups when doing a grow, so flagging a process group for removal with no other changes will cause a replacement process to be added. Flagging a process group for removal when decreasing the desired process count will cause that process group specifically to be removed to accomplish that decrease in process count. Decreasing the desired process count without marking anything for removal will cause the operator to choose process groups that should be removed to accomplish that decrease in process count.

In general, when we need to update a pod's spec we will do that by deleting and recreating the pod. There are some changes that we will roll out by replacing the process group instead, such as changing a volume size. There is also a flag in the cluster spec called `updatePodsByReplacement` that will cause the operator to always roll out changes to pod specs by replacement instead of deletion.

### UpdateStatus

The `UpdateStatus` subreconciler is responsible for updating the `status` field on the cluster to reflect the running state. This is used to give early feedback of what needs to change to fulfill the latest generation and to front-load analysis that can be used in later stages. We run this twice in the reconciliation loop, at the very beginning and the very end. The `UpdateStatus` subreconciler is responsible for updating the generation status and the ProcessGroup conditions.

### UpdateLockConfiguration

The `UpdateLockConfiguration` subreconciler sets fields in the database to manage the deny list for the cluster locking system. See the [Fault Domains](fault_domains.md#coordinating-global-operations) page for more information about this locking system.

### UpdateConfigMap

The `UpdateConfigMap` subreconciler creates a `ConfigMap` object for the cluster's configuration, and updates it as necessary. It is responsible for updating the labels and annotations on the `ConfigMap` in addition to the data.

### CheckClientCompatibility

The `CheckClientCompatibility` subreconciler is used during upgrades to ensure that every client is compatible with the new version of FoundationDB. When it detects that the `version` in the cluster spec is protocol-compatible with the `runningVersion` in the cluster status, this will do nothing. When these are different, it means there is a pending upgrade. This subreconciler will check the `connected_clients` field in the database status, and if it finds any clients whose max supported protocol version is not the same as the `version` from the cluster spec, it will fail reconciliation. This prevents upgrading a database until all clients have been updated with a compatible client library.

You can skip this check by setting the `ignoreUpgradabilityChecks` flag in the cluster spec.

### ReplaceMisconfiguredPods

The `ReplaceMisconfiguredPods` subreconciler checks for process groups that need to be replaced in order to safely bring them up on a new configuration. The core action this subreconciler takes is setting the `remove` field on the `ProcessGroup` in the cluster status. Later subreconcilers will do the work for handling the replacement, whether processes are marked for replacement through this subreconciler or another mechanism.

### ReplaceFailedPods
### DeletePodsForBuggification
### AddProcessGroups
### AddServices
### AddPVCs
### AddPods
### GenerateInitialClusterFile
### UpdateSidecarVersions
### UpdatePodConfig
### UpdateLabels
### UpdateDatabaseConfiguration
### ChooseRemovals
### ExcludeInstances
### ChangeCoordinators
### BounceProcesses
### UpdatePods
### RemoveServices
### RemovePods
## Backup Reconciliation
## Restore Reconciliation

## Next

You can continue on to the [next section](more.md) or go back to the [table of contents](index.md).
