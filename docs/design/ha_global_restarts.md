# Global restarts for HA clusters

## Metadata

* Authors: @johscheuer
* Created: 2022-10-03
* Updated: 2022-10-03

## Background

The current implementation of normal restarts (not version upgrades) are not synchronized between different operators for HA FDB clusters.
This has multiple drawbacks if a user runs a HA FDB cluster, independent if those are managed by the same operator or not.
One example of the underlying issue is [that a restart in one cluster will delay the restart in another cluster](https://github.com/FoundationDB/fdb-kubernetes-operator/issues/1361).

The current limitations of this decoupled process are:

- Rolling out a new knob to an HA cluster takes at least `minimumUptime * (number of clusters - 1)` seconds.
- Coordinator changes that are initiated by other operator, e.g. because an fdbserver will be unavailable for a short moment during th restart, can delay the rollout further (the new cluster file must be synced).
- We will see at least `number of clusters` recoveries to rollout a knob instead of `1`.

## General Design Goals

In this design we want to propose a new mechanism to rollout knob changes for HA clusters to reduce the number of restarts to one and have the restart coordinated across different operators.

## Current Implementation

The current implementation only waits until all process groups in the current cluster configuration are "ready" to restart.
Before the operator tries to restart processes it waits until the new configuration is synced to all affected processes that have the `IncorrectCommandLine` condition.
Once all processes have the new configuration the operator triggers a `fdbcli --exec 'kill; kill ...'` command to restart those fdbserver process managed by this operator instance.
After the restart the fdbserver processes will pickup the new configuration and the `IncorrectCommandLine` will be removed.

## Proposed Design

In order to allow the different operator instances to synchronize we have to change some part of the logic inside the bounce controller.
Every operator will add the processes that needs to be restarted under a special key e.g. `$lockKeyPrefix/pendingRestart/$processGroupID`.
In this example the `$lockKeyPrefix` and `$processGroupID` will be replaced with the actual values during runtime.
This ensures that all operators are aware of which processes must be restarted.
Those changes alone are not enough to ensure that the coordination between the operators work.
We have to signal all operators which processes have the correct configuration and can be restarted.
To solve this each operator will add all process that are ready to be restarted under `$lockKeyPrefix/readyRestart/$processGroupID`.
A process will be ready to be restarted if the current desired configuration is available, this will be validated by the operator (this step is already implemented in the current behaviour).
This ensures that an operator only triggers the restart once all processes are ready to be restarted.
Before doing the restart the operator must try to get a lock to ensure only one operator is restarting the processes.

`$lockKeyPrefix/pendingRestart/$processGroupID` will contain all processes that meet the following conditions:

- `IncorrectCommandLine` is set to true.
- `SidecarUnreachable` is set to false.
- `MissingProcesses` is set to false.
- And the process group is not marked for removal.

`$lockKeyPrefix/readyRestart/$processGroupID` will contain all processes that meet the following conditions:

- `IncorrectCommandLine` is set to true.
- `IncorrectPodSpec` is set to false.
- `SidecarUnreachable` is set to false.
- `MissingProcesses` is set to false.
- And the process group is not marked for removal.

The operator logic would change to add those keys when locking is enabled and the cluster is not being upgraded.
The restart trigger logic would change like this:

```go
// Get all processes that have to be restarted, this method will return (map[string]struct{}, error)
pendingRestart, err := readPendingRestart()
...
// Get all processes that are ready to restart, this method will return (map[string]struct{}, error)
readyRestart, err := readReadyRestart(), this method will return (map[string]struct{}, error)
...

// We have to ensure that we only compare processes that are currently part of the cluster to ensure we ignore processes
// that were deleted between the restart request.
for _, process := range databaseStatus.Cluster.Processes {
    processID := process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]

    _, hasPendingRestart := pendingRestart[processID]
    _, readyRestart := pendingRestart[processID]
	
	if hasPendingRestart != readyRestart {
	    // wait until this process is ready for restart
		return ...
    }
}

// Ensure we clean the pending and the ready list before restarting the processes to ensure we have an empty list after the restart.
err := clearPendingRestart()
err := clearReadReadyRestart()

// Issue the restart command
```

This logic will ensure that an HA FDB clusters will be restarted only once instead of multiple times.

## Related Links

- [Knob rollout can be delayed by coordinator changes](https://github.com/FoundationDB/fdb-kubernetes-operator/issues/1361)
