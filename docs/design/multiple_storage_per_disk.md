# Running Multiple Storage Servers per Disk

## Background

It is common to provision FoundationDB with multiple storage servers on a single disk. This allows more efficient use of IOPS, since a storage server can easily saturate its CPUs before saturating the IOPS of the SSD. In the Kubernetes operator, we only provision a single storage server per disk. This decision was made in order to simplify the implementation, and the management of the processes, and provide more granular resource usage by limiting the standard pod size. We've received feedback from the community telling us that people are interested in exploring denser storage server packing, and we believe it is time to support these configurations.

## General Design Goals

* Allow specifying however many storage servers per disk a user needs for their use case.
* Minimize disruption to existing workloads.
* Minimize additional burdens on use cases that use 1 storage server per disk.

## Current Implementation Details

Every FDB pod consists of a single main container, running fdbmonitor and a single fdbserver process, and a sidecar container providing tools for managing dynamic conf. The stateful pods are provisioned with a single persistent volume claim. The resource requirements for the volume and the rest of the pod can be customized by the user.

Each pod is assigned a single IP address, which is used as both the listen address and public address for the fdbserver process inside the main container. The process listens on a fixed port, which is 4500 for TLS processes and 4501 for non-TLS process. During the conversion between TLS and non-TLS configurations, the process uses two listeners, one on each port. 

The public address, consisting of an IP-and-port combination, is used to target processes in multiple places in the operator. It is used when excluding processes, when killing processes, and when choosing coordinators.

Every pod also has a unique instance ID, which is used throughout the operator to identify the pod. This features in the pending removal list, the parts of the spec for removing processes, and the tracking of processes with incorrect configuration.

## Proposed Design

Throughout this design, we will use 2 storage servers as a motivating case, but the techniques described should generalize to larger numbers of storage servers per disk.

In general, we will be distinguishing here between an "instance" and a "process". The term instance is used in various places in our codebase, with a 1:1 relationship with a pod. That relationship will be preserved in this design. The "process" currently has a 1:1 relationship with a pod, and that will change to a many:1 relationship.

We will add a new field to the cluster spec for storageServersPerPod. The default behavior will be to assume 1 storage server per pod. Each storage pod will have an environment variable specifying how many storage servers it runs. When this value is changed, any storage pods with an incorrect number of storage servers will be marked for removal, which will trigger a replacement that will create new pods with the desired number of storage servers.

When a pod is running with 2 storage servers, we will configure it to use a different fdbmonitor conf template, called, "fdbmonitor-conf-storage-density-2". This template will include multiple fdbserver processes, numbered from "1" to "2". Process `N` will use the port `4498+2*N` for TLS listeners, and `4499+2*N` for non-TLS listeners. Thus the first process for a TLS cluster will use `4500`, and the second will use `4502`. Each process will also be assigned a process ID, which will be stored in the locality. The process ID will have the form `${instance_id}-N` where `instance_id` is the instance ID assigned to the pod as a whole. Thus, for a pod with 2 storage servers and the instance ID `storage-1`, there will be two processes with the process IDs `storage-1-1` and `storage-1-2`, respectively. The instance ID and the process ID will be used in different contexts in the operator, depending on what was being targeted.

In the special case where we have 1 storage server per disk, the process ID will be identical to the instance ID, and the pod will use a monitor conf template called "fdbmonitor-conf-storage". This allows seamless compatibility with existing configurations. TODO: Determine how to control the rollout of the locality-process-id in the command lines.

We will use a new field in the cluster status to track the distinct storage-server-per-disk counts that are currently in use by the cluster. This will include the count currently specified in the spec, as well as all storage-server counts specified on all current pods for the cluster. Based on the current storage-server counts, we will update the config map to ensure that we always have the correct monitor conf templates available until we converge on a single count, at which point we can remove unused templates from the config map.

The process count for storage will be interpreted as defining the number of pods, regardless of the number of storage server per disk.

When removing or replacing storage servers, we will always target whole pods. Thus, the instancesToRemove list and the pendingRemovals map will still use instance IDs to identify the set of things being targeted. During exclusions, we will exclude the entire IP address for the instance. This is a small change from our current practice, but it should be compatible with all existing usage. 

In the incorrectProcesses map in the status, we will track the processes by process ID. This will reflect that the kills must be done on individual server, so we will need to track the full IP and port for each process we are killing. It should also simplify the implementation, since the validations that populate the incorrectProcesses map are done based on per-process information from the database status. The bounce action will use this map to pull the list of addresses for the kill command.

The conversion flow requires that the monitor conf be updated before adding the new pods. Currently the monitor conf update is at a later stage in reconciliation, but it should be possible to move it to an earlier stage.

## Example Conversion Flow

This cluster deals with a cluster called `sample-cluster`. This cluster has 3 storage pods, with instance IDs `storage-1` through `storage-3`. It also has 4 log pods, with instance IDs `log-1` through `log-4`. The pod names will follow from that convention.

1. User changes storageServersPerDisk in cluster spec from 1 to 2.
2. The operator marks storage instances `storage-1` through `storage-3` for removal. We will call these `storage-set-1` for convenience.
3. The operator updates the config map to add a new monitor conf template for `fdbmonitor-conf-storage-density-2`.
4. The operator adds three new storage instances, `storage-4` through `storage-6`.  We will call these `storage-set-2` for convenience.
5. The operator changes coordinators checks if any the instances in `storage-set-1` is a coordinator. If any is, the operator will change coordinators to a new set that includes none of those instances, and will update the config map accordingly.
6. The operator excludes the IPs for `storage-set-1`, and waits for that exclusion to complete.
7. The operator removes the pods for `storage-set-1`.
8. The operator detects that the `fdbmonitor-conf-storage` template is no longer required, and updates the config map accordingly.
9. Reconciliation is complete.

This will leave the cluster with six storage server processes, spread across three disks. The total usable disk space of the cluster will remain the same. Each container will have the same resource allocation as it did when we had 1 storage server per disk.

If the user wants to expand the disk size or other resource requirements to reflect the new storage server density, it will be safe to make those changes at the same time that the user updates the storageServerPerDisk setting. As long as those settings are scoped at the `storage` instance type in the spec, they will only affect the newly created storage servers, and the conversion flow will be the same as the flow outlined above. If the resource settings are scoped at the `general` instance type in the spec, they will impact the log processes and any other processes in the cluster.

## Related Links

[Forum Discussion](https://forums.foundationdb.org/t/design-discussion-running-multiple-storage-servers-per-disk-on-kubernetes/2320)