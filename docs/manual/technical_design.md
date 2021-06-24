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

This document also assumes that you are familiar with the earlier content in the user manual. We especially recommend reading through the section on [Resources Managed by the Operator](resources.md), which describes terminology and concepts that are used heavily in this document.

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

See the [Replacements and Deletions](replacements_and_deletions.md) document for more details on when we do these replacements.

### ReplaceFailedPods

The `ReplaceFailedPods` subreconciler checks for process groups that need to be replaced because they are in an unhealthy state. This only takes action when automatic replacements are enabled.

See the [Replacements and Deletions](replacements_and_deletions.md) document for more details on when we do these replacements.

### DeletePodsForBuggification

The `DeletePodsForBuggification` subreconciler deletes pods that need to be recreated in order to set buggification options. These options are set through the `buggify` section in the cluster spec.

When pods are deleted for buggification, we apply fewer safety checks, and buggification will often put the cluster in an unhealthy state.

### AddProcessGroups

The `AddProcessGroups` subreconciler compares the desired process counts, calculated from the cluster spec, with the number of process groups in the cluster status. If the spec requires any additional process groups, this step will add them to the status. It will not create resources, and will mark the new process groups with conditions that indicate they are missing resources.

### AddServices

The `AddServices` subreconciler creates any services that are required for the cluster. By default, the operator does not create any services. If the `services.headless` flag in the spec is set, we will create a headless service with the same name as the cluster. If the `services.publicIPSource` field is set to `service`, we will create a service for every process group, with the same name as the pod.

### AddPVCs

The `AddPVCs` subreconciler creates any PVCs that are required for the cluster. A PVC will be created if a process group has a stateful process class, has no existing PVC, and has not been flagged for removal.

### AddPods

The `AddPods` subreconciler creates any pods that are required for the cluster. Every process group will have one pod created for it. If a process group is flagged for removal, we will not create a pod for it.

### GenerateInitialClusterFile

The `GenerateInitialClusterFile` creates the cluster file for the cluster. If the cluster already has a cluster file, this will take no action. The cluster file is the service discovery mechanism for the cluster. It includes addresses for coordinator processes, which are chosen statically. The coordinators are used to elect the cluster controller and inform servers and clients about which process is serving as cluster controller. The cluster file is stored in the `connectionString` field in the cluster status. You can manually specify the cluster file in the `seedConnectionString` field in the cluster spec. If both of these are blank, the operator will choose coordinators that satisfy the cluster's fault tolerance requirements. Coordinators cannot be chosen until the pods have been created and the processes have been assigned IP addresses, which by default comes from the pod's IP. Once the initial cluster file has been generated, we store it in the cluster status and requeue reconciliation so we can update the config map with the new cluster file.

### UpdateSidecarVersions

The `UpdateSidecarVersions` subreconciler updates the image for the `foundationdb-kubernetes-sidecar` container in each pod to match the `version` in the cluster spec. Once the sidecar container is upgraded to a version that is different from the main container version, it will copy the `fdbserver` binary from its own image the volume it shares with the main container, and will rewrite the monitor conf file to direct `fdbmonitor` to start an `fdbserver` process using the binary in that shared volume rather than the binary from the image used to start the main container. This is done temporarily in order to enable a simultaneous cluster-wide upgrade of the `fdbserver` processes. Once that upgrade is complete, we will update the image of the main container through a rolling bounce, and the newly updated main container will use the binary that is provided by its own image.

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
