# Tracking Process Groups in the Status

## Metadata

* Authors: @brownleej
* Created: 2020-11-16
* Updated: 2020-11-16

## Background

We have multiple places in the status where we track information about
podNames: `pendingRemovals`, `processCounts`, `incorrectProcesses`,
`incorrectPods`, `failingPods`, `missingProcesses`. None of these lists contains
a full accounting of podNames, and most of them are empty in the stable state.
The operator infers the full list of podNames by fetching pods, but does not
persist that information or pass it between actions. This creates complex
reconciliation behavior when handling PVCs without matching pods, or pods
without matching PVCs. It gets even more complex when handling per-pod services,
because there is now a third dimension of per-process resources that we need to
manage.

In addition to the challenges of reconciling process lists, we have areas where
we lose track of important information once a pod is deleted. A particularly
challenging area is the tracking of IP addresses. IP addresses are the only
mechanism we have to safely exclude podNames during shrinks and replacements.
If a pod is deleted, and the operator recreates it, it will go into a pending
state while it waits to be scheduled. If the pod cannot be scheduled, it will
never receive an IP address. This means the operator has no way of knowing if it
is safe to remove the process. If we could track all of the IP addresses a
process has had independently of the pod lifecycle, we could safely replace
podNames that are stuck in a pending state.

The scope of this design is restricted to managing the FoundationDBCluster
object, and resources that are downstream of that object. Managing backups,
restores, or any other top-level resources in the operator is out of scope.

## General Design Goals

*	Create a centralized place in the status for per-process information
*	Allow replacing pods that do not currently have IP addresses
*	Avoid the word "instance", which has become ambiguous.
*	Allow recovering almost all process information from the running state.

## Proposed Design

The term "process group" will gradually replace the term "instance". This design
will introduce the term, but will not require the removal of all uses of the
word "instance", which has additional design challenges. There will be a 1:1
relationship between a process group and a pod, in the healthy state, but there
will be times where a process group does not have a pod, in which case the
operator will create one. The term "process" will refer to a single fdbserver
process managed by the operator. In configurations where we are running multiple
storage servers on a single disk, the pod will be represented by a single
process group, with multiple fdbserver podNames within it.

We will have a map of `processGroups` in the status. The keys in the map will
be the instance ID, which will be renamed to process group ID in the future. The
values in the map will be `processGroupStatus` entries, which will contain the
following fields:

*	`processClass`: The process class the process group has.
*	`addresses`: A list of addresses the process group has been known to have.
*	`removalTimestamp`:  When the process group was marked for removal.
*	`exclusionTimestamp`: When the operator observes that process group has been fully excluded.
*	`processGroupConditions`: A list of degraded conditions that the process group is in.
	This can include  `incorrectPodSpec`, `incorrectConfigMap`,
	`incorrectCommandLine`, `podFailing`, `missingPod`, `missingPvc`, and
	`missingService`. Each entry will include the condition, and the timestamp
	when we first observed the condition.

The `UpdateStatus` action will fetch the existing resources and add any missing
entries into the `processGroups` map. Any entries in the current map that do not
have existing resources will be left as-is. If the current address is different
from the last known address, and the process group is marked for removal, we
will add the current address to the `addresses` list. If the current address is
different from the last known address, and the process group is not marked for
removal, we will replace the address list with the current address. The
`UpdateStatus` method will also set the conditions on the process group based
on the database status and the results of the calls to the resource list APIs.
If a pod does not currently have an IP address, we will leave the address list
unmodified, allowing us to continue to take actions that require an IP address,
such as excluding podNames, by optimistically re-using the previous address.

The logic for adding new process groups will be extracted into its own action,
which will compare the current process group map with the desired counts and
add new entries to the map. After that action, we will run separate actions for
creating new services, new PVCs, and new pods, in that order. Each of those
actions will target a single resource and will update the status to reflect the
new conditions.

The exclusion action will use the `processGroups` as its source of truth, and
will exclude all of the addresses for process groups marked for removal. If a
process group has no addresses, then we will not be able to complete the
exclusion action. The inclusion action will include all of the addresses for the
process groups, and will then remove those entries from the map.

This new map will replace multiple fields from the status: `pendingRemovals`,
`processCounts`, `incorrectProcesses`, `incorrectPods`, `failingPods`, and
`missingProcesses`.

## Related Links

This touches on multiple recent areas of work:

* [Unifying naming](https://github.com/FoundationDB/fdb-kubernetes-operator/issues/379)
* [Using service IPs](https://github.com/FoundationDB/fdb-kubernetes-operator/issues/283)
* [Replacing pods stuck in pending](https://github.com/FoundationDB/fdb-kubernetes-operator/issues/367)
* [Automating replacements](https://github.com/FoundationDB/fdb-kubernetes-operator/wiki/Design-for-Automating-Replacements-through-the-Operator)
