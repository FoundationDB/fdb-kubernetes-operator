# Automatic Replacements

## Metadata

* Authors: @brownleej
* Created: 2020-11-04
* Updated: 2020-12-11

## Background

Replacing failed processes can be a major source of operational toil when running FoundationDB. This is especially true when using local disks, which expose the service to a wide variety of failure modes both of the disks themselves and the hosts they run on. FoundationDB generally aims to be self-healing, but it will eventually be necessary to replace failing processes to regain capacity. People running FoundationDB may want to define restrictions on other operations while a cluster is in a degraded state, such as having a pod disruption budget to prevent voluntary deletion of pods while another pod is failing to schedule.

## General Design Goals

* Ensure that the system is converging on a fully healthy state whenever possible
* Limit the need for people running FoundationDB to manually replace failed instances in common scenarios
* Protect the system from runaway behavior from the new automation

## Current Implementation

When the operator determines that a process is not reporting to the cluster, it updates the field `removalTimestamp` in `processGroupStatus` status. This includes a timestamp of the first time the operator noticed the process failing. Once the operator sees the process in the cluster again, it removes it from the removalTimestamp field.

Users can manually replace an instance by adding it to the `processGroupsToRemove` field. The operator will also sometimes replace instances by its own logic, such as when changing volume sizes. The source of truth for what instances are going to be removed or replaced is the `pendingRemovals` field in the status.

The operator treats replacements and removals as closely related operations. The spec defines the desired process counts for every process class. If there are currently more processes than is desired, the operator will choose to remove them, by placing them in the `pendingRemovals` field. Processes in that field are not counted in the current process counts. Conversely, if a process is placed in that field and the result would reduce the process counts before the desired value, the operator will add a new process to compensate. That logic is what results in a replacement.

Replacements through the operator currently have a number of sharp edges that become sharper with more automation.

The only way to safely remove a process is to exclude it from the database, which requires an IP address. There are common failure scenarios where a process will not have an IP address, such as when it is failing to schedule because it is bound to a failed disk. In this scenario, the operator refuses to proceed with the exclusion until the IP address is available. The only way around this is to add the instance to the `ProcessGroupsToRemoveWithoutExclusion` list, which deletes the pod and its volume without any safety checks. This is a dangerous operation that can lead to data loss.

In some failure scenarios, pods can get stuck in the Terminating state. For instance, this can happen when the kubelet is partitioned from the rest of the control plane. The operator does not consider the removal complete until the pod is gone, because when it is in a Terminating state we cannot determine if the process is still running, and if it will rejoin the cluster unexpectedly at a later date. While it is in this state, we leave the process excluded, and note the pending removals in the status, but consider reconciliation complete.

If the user tries to replace too many pods at once, it can exhaust their resource quota, or the total resources in their Kubernetes cluster. If this is happening across multiple FDB clusters, it can lead to a deadlock where no replacements are able to complete, because all of them have pods that are pending scheduling.

## Proposed Design

### Initiating Replacements

We will add an option to replace processes that have been marked as missing for a configurable duration of time. The default will be to disable this feature. When it is enabled, the default duration to wait before the replacement will be 30 minutes. Once a process has been selected for replacement, it will be added to the pending removals list, and reconciliation will continue with the normal replacement process. The selection of instances for replacement will be done in the ReplaceMisconfiguredPods action, which already has a similar responsibility.

In order to make sure we consistently detect failures, we will add a new cronjob that runs when automatic replacements are enabled. There will be one instance of the cronjob for every cluster. By default, the frequency of the job will be the same as the replacement wait time. It will use the same service account as the operator by default, and it will need access to the cluster spec, to the list of pods, and to the cluster itself. It will detect missing instances using the same logic that we already use, and if it detects any missing instances it will annotate the cluster resource with a custom annotation set to the current time. This will trigger reconciliation. The cluster spec will offer full customization of the cronjob.

### Making Replacements Smooth

In order for replacements to succeed reliably, we need to handle the case where an instance does not have an IP address and is pending removal. The design on [improving process tracking in the cluster status](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/master/docs/design/implemented/process_group_status.md) will improve this considerably by tracking the previously known IP addresses and allowing us to exclude those addresses when the pod is still pending.

### Additional Safety Measures

To limit the risk of exclusions causing undesired side-effects or exhausting quota, automatic replacements will be limited to one instance at a time. If there is already an instance pending removal that has not been fully excluded, the operator will not do any automatic replacements. If there is an instance that is fully excluded but is stuck waiting for the pod to terminate, we will allow automatic replacements for other instances to go forward.

We will also build a rollback mechanism for replacements that were not intended. We will add a new field to the spec called `cancelReplacementsForInstance`. This will take a list of instance IDs. Any instance in this list will be re-included in the database, and removed from the `pendingRemovals` list. If an instance ID is in this list, the instance will never be added to the pendingRemovals list again. If this produces a conflict with some other part of the spec, the operator will issue an error and requeue reconciliation. We will need to check for this case when adding instances to `pendingRemovals` in `CheckInstancesToRemove` and `ChooseRemovals`. This rollback mechanism should allow a recovery from a scenario where multiple replacements are deadlocking each other.

## Related Links

* [Problems related to removing processes](https://github.com/FoundationDB/fdb-kubernetes-operator/issues?q=is%3Aissue+is%3Aopen+label%3Aremoval-problems)
