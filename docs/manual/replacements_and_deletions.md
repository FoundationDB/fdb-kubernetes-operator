# Replacements and Deletions

The operator has two different strategies it can take on process groups that are in an undesired state: replacement and deletion. In the case of replacement, we will create a brand new process group, move data off the old process group, and delete the resources for the old process group as well as the records of the process group itself. In the case of deletion, we will delete some or all of the resources for the process group and then create new objects with the same names. We will cover details of when these different strategies are used in later sections.

A process group is marked for replacement by setting the `remove` flag on the process group. This flag is used during both replacements and shrinks, and a replacement is modeled as a grow followed by a shrink. Process groups that are marked for removal are not counted in the number of active process groups when doing a grow, so flagging a process group for removal with no other changes will cause a replacement process to be added. Flagging a process group for removal when decreasing the desired process count will cause that process group specifically to be removed to accomplish that decrease in process count. Decreasing the desired process count without marking anything for removal will cause the operator to choose process groups that should be removed to accomplish that decrease in process count.

In general, when we need to update a pod's spec we will do that by deleting and recreating the pod. There are some changes that we will roll out by replacing the process group instead, such as changing a volume size. There is also a flag in the cluster spec called `updatePodsByReplacement` that will cause the operator to always roll out changes to pod specs by replacement instead of deletion.

The following changes can only be rolled out through replacement:

* Changing the instance ID prefix
* Changing the public IP source
* Changing the number of storage servers per pod
* Changing the node selector
* Changing any part of the PVC spec
* Increasing the resource requirements, when the `replaceInstancesWhenResourcesChange` flag is set.

## Automatic Replacements

The operator has an option to automatically replace pods that are in a bad state. This behavior is disabled by default, but you can enable it by setting the field `automationOptions.replacements.enabled` in the cluster spec. This will replace any pods that meet the following criteria:

* The process group has a condition that is eligible for replacement, and has been in that condition for 1800 seconds. This time window is configurable through `automationOptions.replacements.failureDetectionTimeSeconds`.
* The number of process groups that are marked for removal and not fully excluded, counting the process group that is being evaluated for replacement, is less than or equal to 1. This limit is configurable through `automationOptions.replacements.maxConcurrentReplacements`.

The only condition that is currently eligible for replacement is when the process is not reporting to the database.

## Next

You can continue on to the [next section](fault_domains.md) or go back to the [table of contents](index.md).
