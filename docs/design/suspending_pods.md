# Suspending Pods before deletion

## Metadata

* Authors: @johscheuer.
* Created: 2023-08-**
* Updated: -

## Background

The FoundationDB operator is a critical piece to manage FoundationDB clusters running in Kubernetes.
As part of the FoundationDB cluster lifecycle the operator has to perform different operations, like migrating the data from one Pod to another Pod.
Doing a migration requires that the operator is excluding the old process(es) running in the Pod marked for removal.
After the exclusion is started the operator has to verify that the exclusion is done to make sure it is safe to remove the Pod and the PVC.
There could be a bug in the operator or FoundationDB that leads the operator to mistakenly removing a Pod and PVC that still has data on it.
In order to reduce the risk and minimize the recovery time the operator should suspend Pods before deleting the PVC.
The assumption here is that recreating a Pod is much faster than recovering the lost data from a backup (either in FoundationDB or in Kubernetes).

## General Design Goals

The suspension mechanism should be optional and should be turned off per default, this might change in the future if we decide that this feature is stable.
The recovery of a suspended Process Group should be easy and fast.
The suspension duration must be configurable by the user.

## Current Implementation

The operator does not support suspending Pods.

## Proposed Design

We will add a new subreconciler that will perform the same exclusion checks as the `removeProcessGroups` reconciler.
If the suspension logic is disabled the operator will skip all for in the new `suspendProcessGroups` subreconciler.
When all processes of a Process Group that should be removed are identified to be fully excluded, the operator will add a new timestamp in the Process Group Status, called `SuspensionTimestamp`, and will remove the Pod without removing any other resources of this Process Group.
The suspension logic will follow the same mechanism as the `removeProcessGroups` reconciler and only suspend the Process Groups based on the `RemovalMode` of the FoundationDBCluster.
In order to suspend a Pod the operator will make use of `PodLifecycleManager.DeletePod`.

The `removeProcessGroups` reconciler will be changed to only remove Process Groups that have the `SuspensionTimestamp` set and the timestamp is older than the defined wait time., if the wait time is set to `0`, the suspension mechanism is disabled.
If a user defines that a Process Group should be suspended for at least 2h the operator will only remove resources for a Process Group that has a `SuspensionTimestamp` older than 2 hours.
There is no guarantee that the resources are deleted exactly after the defined timespan, there could be a delay of the deletion of the resources.
Basically this means that the reconcile method of the `removeProcessGroups` reconciler is adjust like this:

```go
	// Ensure we only remove process groups that are not blocked to be removed by the buggify config.
	processGroupsToRemove = buggify.FilterBlockedRemovals(cluster, processGroupsToRemove)
	// If all of the process groups are filtered out we can stop doing the next steps.
	if len(processGroupsToRemove) == 0 {
		return nil
	}

    processGroupsToRemove = internal.FilterSuspendedProcessGroups(cluster, processGroupsToRemove)
    // If all of the process groups are filtered out we can stop doing the next steps.
    if len(processGroupsToRemove) == 0 {
        return nil
    }
```

and the filtering of suspended Process Groups will look like this:

```go
func FilterSuspendedProcessGroups(cluster *fdbv1beta2.FoundationDBCluster, processGroupsToRemove []*fdbv1beta2.ProcessGroupStatus) []*fdbv1beta2.ProcessGroupStatus {
	suspensionDuration := cluster.GetMinimumSuspensionDurationInSeconds()

	if suspensionDuration == 0 {
		return processGroupsToRemove
	}

	filteredList := make([]*fdbv1beta2.ProcessGroupStatus, 0, len(processGroupsToRemove))
	for _, processGroup := range processGroupsToRemove {
		if processGroup.SuspensionTimestamp.IsZero() {
			continue
		}

		if time.Since(processGroup.SuspensionTimestamp).Seconds() < suspensionDuration {
			continue
		}

		filteredList = append(filteredList, processGroup)
	}

	return filteredList
}
```

The returned list will only contain Process Groups that have the `SuspensionTimestamp` set for at least the minimum suspension time.
If the SuspensionTimestamp is 0 the whole input list will be returned.

The actual `suspendProcessGroup` reconcile function will look similar to the `removeProcessGroups` with the difference, that the reconciler will call the `suspendProcessGroups` method and ignore all Process Groups that are already have an `SuspensionTimestamp` set:

```go
// reconcile runs the reconciler's work.
func (u suspendProcessGroup) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, logger logr.Logger) *requeue {
   	// Same as the removeProcessGroups reconciler, but we will filter out all process groups that already have an SuspensionTimestamp

	// If the operator is allowed to suspend all process groups at the same time we don't enforce any safety checks.
	if cluster.GetRemovalMode() != fdbv1beta2.PodUpdateModeAll {
		// To ensure we are not suspending zones faster than Kubernetes actually removes Pods we are adding a wait time
		// if we have resources in the terminating state. We will only block if the terminating state was recently (in the
		// last minute).
		waitTime, allowed := removals.RemovalAllowed(lastDeletion, time.Now().Unix(), cluster.GetWaitBetweenRemovalsSeconds())
		if !allowed {
			return &requeue{message: fmt.Sprintf("not allowed to suspend process groups, waiting: %v", waitTime), delay: time.Duration(waitTime) * time.Second}
		}
	}

	zone, zoneRemovals, err := removals.GetProcessGroupsToRemove(cluster.GetRemovalMode(), zonedRemovals)
	if err != nil {
		return &requeue{curError: err}
	}

	logger.Info("Suspending process groups", "zone", zone, "count", len(zoneRemovals), "deletionMode", cluster.GetRemovalMode())
	// This method will also update the process group status information.
	r.suspendProcessGroups(ctx, logger, cluster, zoneRemovals, zonedRemovals[removals.TerminatingZone])
	
	return nil
}
```

To make the recovery of a suspended Process Group easy, the `kubectl-fdb` plugin will be extended with a new subcommand called `recover process-groups`.
The subcommand will take a cluster and a list of Process Group IDs.
The implementation of this subcommand will reset the `RemovalTimestamp`, `ExclusionTimestamp` and the `SuspensionTimestamp` for the provided Process Groups and make sure they are removed from the `ProcessGroupsToRemove` list.
Once those timestamps are removed and the Process Group is not present in the `ProcessGroupsToRemove` the operator will recreate the Pod, which will bring back the data.
Depending on the exclusion mechanism used the new process might still be excluded, e.g. if locality-based exclusions are used.
If the newly created Pod gets a new IP address and the operator is using IP based exclusions, the new process will not be excluded and must be excluded again if desired.

## Related Links

-
