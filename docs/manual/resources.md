# Resources Managed by the Operator

The operator creates the following resources for a FoundationDB cluster:

* `ConfigMap`: The operator creates one config map for each cluster that holds configuration like the cluster file and the fdbmonitor conf files.
* `Service`: By default, the operator creates no services. You can configure a cluster-wide headless service for DNS lookup, and you can configure a per-process-group service that can be used to provide the public IP for the processes, as an alternative to the default behavior of using the pod IP as the public IP.
* `PersistentVolumeClaim (PVC)`:  We create one persistent volume claim for every stateful process group.
* `Pod`: We create one pod for every process group, with one container for starting fdbmonitor and one container for starting a helper sidecar.

## Process Groups

Inside the cluster status, we track an object called `ProcessGroup` which loosely corresponds to a pod in Kubernetes. A process group represents a set of processes that will run inside a single container. In the default case we run one `fdbserver` process in each process group, but if you configure your cluster to run multiple storage servers per disk then we will have multiple storage server processes inside a single process group, with each process group having its own disk. We use the process group to track information about processes that lives outside the lifecycle of any other Kubernetes object, such as an intention to remove the process or adverse conditions that the operator needs to remediate.

The following conditions can appear on process groups to indicate a problem with those processes:

* `IncorrectPodSpec`: A process group that has an incorrect Pod spec.
* `IncorrectConfigMap`: A process group that has outdated configuration in its local copy of the ConfigMap.
* `IncorrectCommandLine`: A process that has an incorrect command-line for its process.
* `PodFailing`: A process group which has Pod that is not in a ready state.
* `MissingPod`: A process group that doesn't have a Pod assigned.
* `MissingPVC`: A process group that doesn't have a PVC assigned.
* `MissingService`: A process group that doesn't have a Service assigned.
* `MissingProcesses`: A process group that has a process that is not reporting to the database.

## Process Classes

FoundationDB processes can have several process classes, which determine what roles a process is capable of taking on. The [ProcessCounts](../cluster_spec.md#ProcessCounts) section in the cluster spec provides a list of all of the supported process classes. The most common process classes are `storage`, `log`, and `stateless`. `storage` is a stateful role that is responsible for long-term storage of data. `log` is a stateful role that accepts and stores committed mutations until they can be made durable on the storage servers. `stateless` is a stateless class that can serve multiple roles in the cluster, such as proxies, resolvers, and the cluster controller.

The only stateful process classes are `storage`, `log`, and `transaction`. Pods for these process classes will have persistent volume claims associated with them, and pods for other process classes will not have persistent volume claims.

## Resource Names

Process groups have a naming convention that is different from the pod name. Process group IDs are intended to be unique within an FDB cluster, across all Kubernetes Clusters. Process groups for different FDB clusters can have the same process group ID. These IDs have the form `$prefix-$class-$number`. `$prefix` is set by the `instanceIDPrefix` field, and if this field is omitted the IDs will have no prefix and will begin with the `$class`. The `$class` field is set to the process class, e.g. `storage` or `log`. `$number` is set to the lowest number that is greater than `0` and is not used for any existing process group with the same process class. This convention is similar to the convention used by `StatefulSet`, but there is an important distinction: process groups are not guaranteed to be numbered from `1-N`. When a process group is replaced, we will add a new process group and then remove the old process group, and this will create a gap in the process group numbers. If the operator later has to create another process group, it will re-use the number in that gap before trying to use a new, higher number. If a process class has an underscore in it, such as `cluster_controller`, that underscore will be retained inside the process group ID.

Pod names are intended to be unique within a namespace and Kubernetes Cluster. Pods for a single FDB cluster that run in different Kubernetes Clusters can have the same pod name. Pod names have the format `$cluster-$class-$number`. `$cluster` is the name of the cluster. `$class` and `$number` have the same meaning and value as they have in the process group name. If the process class has an underscore, it will be replaced with a dash.

Volume claims have the same name as a pod, with a suffix taken from the name in the `processes.volumeClaimTemplate` field in the cluster spec. If this field is not set, we will use the suffix `data`.

Per-pod services have the same name as the pod.

The process group ID will be put in a label called `fdb-instance-id` on any resource the operator creates for that process group.

The table below provides examples of different resource names for a process group. These examples assume that your cluster is called `example`.

| Description            | Process Group        | Pod                          | PVC                     |
| ---------------------- | -------------------- | ---------------------------- | ----------------------- |
| Storage                | storage-1            | example-storage-1            | example-storage-1-data  |
| Cluster Controller     | cluster_controller-1 | example-cluster-controller-1 | N/A                     |
| instanceIDPrefix = dc1 | dc1-storage-1        | example-storage-1            | example-storage-1-data  |
| PVC name = state       | storage-1            | example-storage-1            | example-storage-1-state |

## Built-in Metadata

The operator sets some built-in fields in the metadata for the resources it creates. The operator sets the following labels:

* `fdb-cluster-name`: The name of the FoundationDBCluster object the resource is for.
* `fdb-process-class`: The process class that the associated process is running.
* `fdb-instance-id`: The ID of the process group that the resource is related to.

The operator sets the following annotations on pods:

* `foundationdb.org/last-applied-spec`: A hash of the spec that was used to create the resource.
* `foundationdb.org/public-ip`: The value for the `services.publicIPSource` field in the cluster spec when the pod was created.

## Next

You can continue on to the [next section](operations.md) or go back to the [table of contents](index.md).
