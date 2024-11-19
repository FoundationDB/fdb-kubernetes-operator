# Better coordination for multiple operator instances

## Metadata

* Authors: @johscheuer
* Created: 2024-11-19
* Updated: 2024-11-19

## Background

The current way to run multi-region FoundationDB deployments with the operator is to run multiple operator instances in different namespaces and/or in different Kubernetes clusters.
This brings a few synchronization challenges as those different operator instances are not communication with each other and they are not sharing state.
We already added the `LockClient` to synchronize the upgrade step for minor and major upgrades, where all processes must be restarted at once.
The `LockClient` is also used to get a lease that allows the operator instance to perform certain actions, like exclusions, that should only be execute by one instance at a time.

## General Design Goals

This design will show a possible way to synchronize the `exclusion`, `inclusion` and `removal` of process groups across different operator instances for the multi-region setup.

## Current Implementation

Right now only the restart (bounce) of the processes during a minor or major version upgrade is coordinated.
All other bounces, exclusions, inclusions or removals are not coordinated with the locking mechanism only one operator instance will perform the according action to it's "local" processes.
In most cases this works fine but causes more disruptions than required as every operator instance is perform the same set of operations on the local set of processes.
The missing synchronization between the operator instance can result in multiple coordinator changes during the rollout of a setting or during an upgrade.

## Proposed Design

The idea is to extend the existing `AdminClient` with some similar functionality from the `LockClient` to coordinate the following actions:

- coordinator selection
- exclusions
- inclusions
- bounces

The following methods will be added:

- `AddPendingForRemoval`: Adds the process group ID to a set of process groups that are marked for removal.
- `AddPendingForExclusion`: Adds the process group ID to a set of process groups that should be excluded.
- `AddPendingForInclusion`: Adds the process group ID to a set of process groups that should be included.
- `AddPendingForRestart`: Adds the process group ID to a set of process groups that should be restarted.
- `RemoveFromPendingForRemoval`: Removes the process group ID from the set of process groups that should be removed.
- `RemoveFromPendingForExclusion`: Removes the process group ID from the set of process groups that should be excluded.
- `RemoveFromPendingForInclusion`: Removes the process group ID from the set of process groups that should be included.
- `RemoveFromPendingForRestart`: Removes the process group ID from the set of process groups that should be restarted.
- `AddReadyForExclusion`: Adds the process group ID to a set of process groups that are ready to be excluded.
- `AddReadyForInlusion`: Adds the process group ID to a set of process groups that are ready to be included.
- `AddReadyForRestart`: Adds the process group ID to a set of process groups that are ready to be restarted.
- `GetPendingForRemoval`: Gets the process group IDs for all process groups that are marked for removal.
- `GetPendingForExclusion`: Gets the process group IDs for all process groups that should be excluded.
- `GetPendingForInclusion`: Gets the process group IDs for all the process groups that should be included.
- `GetPendingForRestart`: Gets the process group IDs for all the process groups that should be restarted.
- `GetReadyForExclusion`: Gets the process group IDs for all the process groups that are ready to be excluded.
- `GetReadyForInlusion`: Gets the process group IDs for all the process groups that are ready to be included.
- `GetReadyForRestart`: Gets the process group IDs fir akk tge process groups that are ready to be restarted.


The information will be stored in FDB itself, so it will be available for all operators and will get the same benefits from the transaction guarantees.
The keys will be prefixed with the `client.cluster.GetLockPrefix()` (default `\xff\xff/org.foundationdb.kubernetes-operator`) and the according path, e.g. `readyForExclusion`.
The value will be empty, except for the `readyForExclusion` and `readyForInclusion` case, in those cases the value will be the process group address(es).

The following sub-reconciler will be modified:

- `updateStatus`: Will add process groups that are marked for removal to the key range and removes them from the key range if they are not being present anymore in the `FoundationDBCluster` status and the process group prefix matches. This sub-reconciler will also remove process groups from the `readyForExclusion`, `readyForInclusion` and `readyForRestart` (and the same for pending).
- `excludeProcesses`: Will add process groups that are pending for exclusions and add process groups that are ready for exclusion.
- `changeCoordinators`: Will read the `pendingForRemoval` set and will not take any process group from this set as a candidate for the new coordinators.
- `removeProcessGroups`: Will add process groups that can be included to `readyForInclusion`.

We will add a new setting under the `FoundationDBClusterAutomationOptions` with the name `synronizationMode`.
The default will be `local`, which will represent the current state and the new mechanism can be enabled by setting the value to `global`.
Those changes will add some additional load to the cluster, but the additional load should be fairly limited compared to the customer load on those clusters and the request pattern will be mostly reading the data.

Pseudo-Code changes for the `excludeProcesses` sub-reconciler:

```go
func (e excludeProcesses) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, logger logr.Logger) *requeue {
	pendingForExclusions, err := adminClient.GetPendingForExclusion()
	// error handling
	// Filter missing process groups from pendingForExclusions to only add new process groups if they are currently not present.
	// Ensure that pendingForExclusions includes all the process groups (also the newly added once).
	if len(missingFdbProcessesToExclude) > 0 {
		err := adminClient.AddPendingForExclusion(missingFdbProcessesToExclude)
		// error handling
	}

	// Check which processes can be excluded and if it's safe to exclude processes.
	// ...
	// In case that there are processes from different transaction process classes, we expect that the operator is allowed
	// to exclude processes from all the different process classes. If not the operator will delay the exclusion.
	if !transactionSystemExclusionAllowed {
		return &requeue{
			message:        "more exclusions needed but not allowed, have to wait until new processes for the transaction system are up to reduce number of recoveries.",
			delayedRequeue: true,
		}
	}

	readyForExclusion, err := adminClient.GetReadyForExclusion()
	// error handling
	// Ensure that pendingForExclusions includes all the process groups (also the newly added once).
	// Add the new process groups that are ready to be excluded.
	err := adminClient.AddReadyForExclusion(missingFdbProcessesReadyToExclude)
	// error handling

	// check if readyForExclusion contains all the process groups from pendingForExclusions if so proceed with the exclusion, if not
	// do a delayed requeue but don't issue the exclusion
	if !equality.Semantic.DeepEqual(readyForExclusion, pendingForExclusions) {
		return &requeue{
			message:        fmt.Sprintf("more processes are pending exclusions, will wait until they are ready to be excluded %d/%d", len(readyForExclusion), len(pendingForExclusions)),
			delayedRequeue: true,
		}
	}

	// If a coordinator should be excluded, we will change the coordinators before doing the exclusion. This should reduce the
	// observed recoveries, see: https://github.com/FoundationDB/fdb-kubernetes-operator/issues/2018.
	if coordinatorExcluded {
		coordinatorErr = coordinator.ChangeCoordinators(logger, adminClient, cluster, status)
	}

	// Perform exclusion for all the readyForExclusion addresses

	return nil
}
```

For the new setting we will be adding a new dedicated e2e test suite to ensure all changes are properly tested.

## Related Links

-
