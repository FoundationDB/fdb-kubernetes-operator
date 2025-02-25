# Better coordination for multiple operator instances

## Metadata

* Authors: @johscheuer
* Created: 2024-11-19
* Updated: 2024-11-19

## Background

The current way to run multi-region FoundationDB deployments with the operator is to run multiple operator instances in different namespaces and/or in different Kubernetes clusters.
This brings a few synchronization challenges as those different operator instances are not communicating with each other and they are not sharing state.
We already added the `LockClient` to synchronize the upgrade step for minor and major upgrades, where all processes must be restarted at once.
The `LockClient` is also used to get a lease that allows the operator instance to perform certain actions, like exclusions, that should only be executed by one instance at a time.

The difference between the upgrades and the other operations that should be coordinated is that the upgrades have a "global" source of truth.
During the upgrades the each operator instance can check that all processes in the cluster are ready for being upgraded.
In contrast the other operations are lacking such a "global" source of truth and only have their own local state based on th `FoundationDBCluster` resource.
Adding an optimistic coordination mechanism doesn't guarantee that the operations are executed once but it will increase the probability that the operator instance have enough time to coordinate.
Most operations have some requirements before they can be executed, e.g. in case of a knob rollout the `ConfigMap` must be updated and synced to all affected Pods or in the case of a replacement a new pod and the according resources must be created.
The waiting time until the prerequisites are satisfied should be enough time to allow the individual operator instances to update the pending list for the coordination.
This is still an imperfect solution which will not guarantee that the operator instances will coordinate, but the solution is a good enough solution until we have a multi-cluster operator.
In order to increase the probability that the different operator instances have time to update the pending list, we could add a dedicated wait window until an operation will be executed.
Adding a wait window will have the side effect that the rollout of a change might take longer than required.

One assumption for the coordination is, that all operations that could use coordination will update the `FoundationDBCluster` resources in a short time window, e.g. by the same deploy pipeline.


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

- `UpdatePendingForRemoval`: Updates the set of process groups that are marked for removal, an update can be eiter the addition or removal of a process group.
- `UpdatePendingForExclusion`: Updates the set of process groups that should be excluded, an update can be eiter the addition or removal of a process group.
- `UpdatePendingForInclusion`: Updates the set of process groups that should be included, an update can be eiter the addition or removal of a process group.
- `UpdatePendingForRestart`: Updates the set of process groups that should be restarted, an update can be eiter the addition or removal of a process group.
- `UpdateReadyForExclusion`: Updates the set of process groups that are ready to be excluded, an update can be eiter the addition or removal of a process group
- `UpdateReadyForInclusion`: Updates the set of process groups that are ready to be included, an update can be eiter the addition or removal of a process group.
- `UpdateReadyForRestart`: Updates the set of process groups that are ready to be restarted, an update can be eiter the addition or removal of a process group
- `GetPendingForRemoval`: Gets the process group IDs for all process groups that are marked for removal.
- `GetPendingForExclusion`: Gets the process group IDs for all process groups that should be excluded.
- `GetPendingForInclusion`: Gets the process group IDs for all the process groups that should be included.
- `GetPendingForRestart`: Gets the process group IDs for all the process groups that should be restarted.
- `GetReadyForExclusion`: Gets the process group IDs for all the process groups that are ready to be excluded.
- `GetReadyForInclusion`: Gets the process group IDs for all the process groups that are ready to be included.
- `GetReadyForRestart`: Gets the process group IDs fir akk tge process groups that are ready to be restarted.

The following sub-reconciler will be added or modified:

- `operatorCoordination`: Will add process groups that are marked for removal to the key range and removes them from the key range if they are not being present anymore in the `FoundationDBCluster` status and the process group prefix matches. This sub-reconciler will also remove process groups from the `readyForExclusion`, `readyForInclusion` and `readyForRestart` (and the same for pending).
- `excludeProcesses`: Will add process groups that are pending for exclusions and add process groups that are ready for exclusion.
- `changeCoordinators`: Will read the `pendingForRemoval` set and will not take any process group from this set as a candidate for the new coordinators.
- `removeProcessGroups`: Will add process groups that can be included to `readyForInclusion`.

We will add a new setting under the `FoundationDBClusterAutomationOptions` with the name `synronizationMode`.
The default will be `local`, which will represent the current state and the new mechanism can be enabled by setting the value to `global`.
Those changes will add some additional load to the cluster, but the additional load should be fairly limited compared to the customer load on those clusters and the request pattern will be mostly reading the data.

### FoundationDB Key Space

The information about pending and ready processes will be stored in FDB itself, so it will be available for all operators and will get the same benefits from the transaction guarantees.
The keys will be prefixed with the `client.cluster.GetLockPrefix()` (default `\xff\xff/org.foundationdb.kubernetes-operator`) and the according path, e.g. `readyForExclusion`.
The value will be empty, except for the `readyForExclusion` and `readyForInclusion` case, in those cases the value will be the process group address(es) that should be used for exclusion or inclusion.

For efficient scans we will add a sub-path between the prefix and the process group id with the process group id prefix.

Example: The resulting key-value mappings for the process group id `kube-cluster-1-storage-1` (`kube-cluster-1` is the process group id prefix) with locality based exclusions enabled will look like this for the exclusion case:

```text
# This key will be directly added when the process group is marked for removal.
\xff\xff/org.foundationdb.kubernetes-operator/pendingForExclusion/kube-cluster-1/kube-cluster-1-storage-1 - {}
# This key will be added by the exclusion sub-reconciler, as soon as the exclusion could be executed.
\xff\xff/org.foundationdb.kubernetes-operator/readyForExclusion/kube-cluster-1/kube-cluster-1-storage-1 - locality_instance_id:kube-cluster-1-storage-1
```

Each operator instance will only write or delete the "local" process groups.
"Local" means in this context that the operator instance only writes into the sub-path that has the `processGroupIDPrefix` which is defined in the `FoundationDBCluster` resource.
The operator instance will read the data from all process groups to.
The reading and writing will be handled in a transaction to ensure either all keys are updated or none.

In the case that the `processGroupIDPrefix` should change, the operator will migrate all the "old" keys to the desired keys in a single transaction.

Example: The above `processGroupIDPrefix` changes from `kube-cluster-1` to `unicorn`:

```text
\xff\xff/org.foundationdb.kubernetes-operator/pendingForExclusion/kube-cluster-1/kube-cluster-1-storage-1 --> \xff\xff/org.foundationdb.kubernetes-operator/pendingForExclusion/unicorn/kube-cluster-1-storage-1
\xff\xff/org.foundationdb.kubernetes-operator/readyForExclusion/kube-cluster-1/kube-cluster-1-storage-1  --> \xff\xff/org.foundationdb.kubernetes-operator/readyForExclusion/unicorn/kube-cluster-1-storage-1
```

Once the required action was executed or the process groups are not being part of the cluster anymore the entries will be removed.
The `UpdateStatus` sub-reconciler will perform checks to remove old entries.

### Example Exclusion Sub-Reconciler

The following example will show the required modifications for the `excludeProcesses` sub-reconciler.

1. The operator instance fetches all processes that are pending for exclusion from the `\xff\xff/org.foundationdb.kubernetes-operator/pendingForExclusion` key space.
2. The operator instance will filter out all local process groups that must be excluded but are missing in the `pendingForExclusion` key space and adds them.
3. In the next step the operator performs the same steps to check if the exclusions could be done.
4. If the local exclusions are allowed, the operator will add all the processes that can be excluded to the `\xff\xff/org.foundationdb.kubernetes-operator/readyForExclusion` key space.
5. Now the operator checks if all the entries from `pendingForExclusion` are also present in `readyForExclusion`, this check should be done on a process class basis.
6. If the two sets are coherent (contain the exact same elements), the operator will perform the usual exclusion steps, e.g. get a lock before issuing the exclude command.

In the initial implementation we are not adding any dedicated wait steps to increase the probability that each operator instance can add its entries to the key space.
The assumption here is that all operations have some prerequisites before they can be executed and in most cases waiting for the prerequisites should give the operator instances enough time to add the process groups to the pending list(s).

```go
func (e excludeProcesses) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, logger logr.Logger) *requeue {
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

	pendingForExclusions, err := adminClient.GetPendingForExclusion()
	// error handling
	readyForExclusion, err := adminClient.GetReadyForExclusion()
	// error handling
	// Add the new process groups that are ready to be excluded.
	err := adminClient.UpdateReadyForExclusion(missingFdbProcessesReadyToExclude)
	// error handling

	// Ensure that pendingForExclusions includes all the process groups (also the newly added once).
	// Check if readyForExclusion contains all the process groups from pendingForExclusions if so proceed with the exclusion, if not
	// do a delayed requeue but don't issue the exclusion.
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

### Operator Coordination Sub-Reconciler

This new sub-reconciler will run after the first `updateStatus` sub-reconciler and will be responsible for adding and removing process groups from the state in the FoundationDBCluster.
For this the operator will iterate over all process groups from the `FoundationDBCluster` resource.
Since this sub-reconciler runs after the first `updateStatus` sub-reconciler it should have the latest information.

When a process group is marked for removal and not excluded the operator will add this process group to the set of `pendingForRemoval`, `pendingForExclusion` and `pendingForInclusion`.
When a process group has the `IncorrectCommandLine` condition it will be added to `pendingForRestart`.
When a process group has a non-nil `ExclusionTimestamp` it will be removed from `pendingForExclusion` and `readyForExclusion` as the process group was excluded.
When a process group is removed all associated entries will be removed too.

If the `synronizationMode` is set to `local` the operator will skip any work and any data that is still present must be deleted manually.
Otherwise the sub-reconciler would need to always read the key ranges which could have an affect to existing FoundationDB clusters.

The goal of this new sub-reconciler is to manage the state in FoundationDB for the local process groups.
Adding a new sub-reconciler will be easier to maintain instead of adding this logic to the `updateStatus` sub-reconciler.

```go
func (reconciler operatorCoordination) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, _ *fdbv1beta2.FoundationDBStatus, logger logr.Logger) *requeue {
	// If the synchronization mode is local (default) skip all work
	if cluster.GetSynronizationMode() == string(fdbv1beta2.SynronizationModeLocal) {
		return nil
	}

	// Create an admin client to interact with the FoundationDB cluster
	adminClient, err := r.getAdminClient(logger, cluster)
	if err != nil {
		return &requeue{curError: err}
	}

	// Read all data from the lists to get the current state. If a prefix is provided to the get methods, only
	// process groups with the additional sub path will be returned.
	pendingForExclusion, err := adminClient.GetPendingForExclusion(cluster.Spec.ProcessGroupPrefix)
	// error handling
	// repeat for all get methods

	// UpdateAction can be "delete" or "add". If the action is "add" the entry will be added, if
	// the action is "delete" the entry will be deleted.
	updatesPendingForExclusion := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{}

	// Iterate over all process groups to generate the expected state.
	for _, processGroup := range cluster.Status.ProcessGroups {
		// Keep track of the visited process group to remove entries from removed process groups.
		visited[processGroup.ProcessGroupID] = fdbv1beta2.None{}
		if processGroup.IsMarkedForRemoval() && !processGroup.IsExcluded() {
			// Check if process group is present in pendingForRemoval, pendingForExclusion and pendingForInclusion.
			// If not add it to the according set.
			// ...

			// Will be repeated for the other fields.
			if _, ok := pendingForExclusion[processGroup.ProcessGroupID]; !ok {
				updatesPendingForExclusion[processGroup.ProcessGroupID] = fdbv1beta2.UpdateActionAdd
			}
		}

		if processGroup.GetConditionTime(fdbv1beta2.IncorrectCommandLine) != nil {
			// Check if the process group is present in pendingForRestart.
			// If not add it to the according set.
			// ...
		} else {
			// Check if the process group is present in pendingForRestart or readyForRestart.
			// If so, add them to the set to remove those entries as the process has the correct command line.
			// ...
		}

		if processGroup.IsExcluded() {
			// Check if the process group is present in pendingForExclusion or readyForExclusion.
			// If so, add them to the set to remove those entries as the process is already excluded.
			// ...
			if _, ok := pendingForExclusion[processGroup.ProcessGroupID]; ok {
				updatesPendingForExclusion[processGroup.ProcessGroupID] = fdbv1beta2.UpdateActionDelete
			}
		}
	}

	// Iterate over all the sets and mark all entries that are associated with a removed process group to be
	// removed.
	for _, processGroup :- range  updatesPendingForExclusion {
		// If the process group was not visited the process group was removed and all the
		// associated entries should be removed too.
		if _, ok := visited[processGroup.ProcessGroupID]; !ok {
			updatesPendingForExclusion[processGroup.ProcessGroupID] = fdbv1beta2.UpdateActionDelete
		}
	}

	err = adminClient.UpdatePendingForExclusion(updatesPendingForExclusion)
	// error handling

	return nil
}
```

For the new setting we will be adding a new dedicated e2e test suite to ensure all changes are properly tested.

## Related Links

-
