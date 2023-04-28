# Tainted Node Eviction

## Metadata

* Authors: @johscheuer
* Created: 2023-02-14
* Updated: 2023-03-08

## Background

The operator is currently able to detect failures of Pods and replace them if required, the replacement is happening based on the [conditions that need replacement](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/v1.14.0/api/v1beta2/foundationdbcluster_types.go#L65).
One of the missing pieces in failure mitigation is to evict Pods from tainted nodes.
In Kubernetes it's common to use [taints](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration) to indicate potential problems with a node or planned maintenance.
This information could be consumed by the operator to proactively replace Pods that are running on nodes with a given set of taints.

## General Design Goals

The goal of this design is to implement a mechanism that allows the operator to replace Pods that are running on nodes with a given set of taints.
It's expected that the operator will detect the Pods running on tainted nodes in a timely manner, either by receiving an event triggering the reconciliation loop or when running the periodic reconciliation, which will happen every 10h per default.
The actual replacement of all Pods running on tainted nodes, depends on the data that must be replicated and the number of affected Pods.

## Current Implementation

We don't have a current implementation of this and the operator is currently not able to access any node information.
The closest implementation to this is the [Replace Failed ProcessGroups reconciler](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/main/controllers/replace_failed_process_groups.go), which could be extended for this.

## Proposed Design

We have to make sure that this feature can be enabled and disabled, e.g. if someone wants to run the operator inside a cluster without node access.
The `AutomaticReplacementOptions` will be extended to support the configuration of replacements for Pods that run on tainted nodes.
The new field will be called `taints` and will contain a list, the default would be an empty list, of taint configurations.
That configuration will include the taint key, this has to be an exact match or a wildcard `*`, and the duration this taint must be present on the node.
The `durationInSeconds` allow to specify how sensitive the operator should be to taints.
If a wildcard is specified as key this will be used as default value, except there is an exact match defined for a taint key.

```yaml
...
automaticReplacementOptions:
  enabled: true
  failureDetectionTimeSeconds: 7200
  maxConcurrentReplacements: 1
  enableNodeFailureDetection: true
  taints:
  - key: "example.org/maintenance"
    durationInSeconds: 7200 # Ensure the taint is present for at least 2 hours before replacing Pods on a node with this taint
  - key: "*" # The wildcard would allow to define a catch all configuration
    durationInSeconds: 3600 # Ensure the taint is present for at least 1 hour before replacing Pods on a node with this taint
```

We will add a new [ProcessGroupConditionType](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/main/api/v1beta2/foundationdbcluster_types.go#L673), called `RunningOnTaintedNode` to mark process groups that are running on a tainted node.
In addition we will extend the  [conditions that need replacement](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/v1.14.0/api/v1beta2/foundationdbcluster_types.go#L65) by the newly created `RunningOnTaintedNode` condition.
Those two steps will make sure that the marked process groups will be eventually replaced by the operator.
The minimum time until a process group that is running on a newly tainted node will be replaced is `failureDetectionTimeSeconds` (default 2h) + `durationInSeconds` from the taint configuration.
In the example above this will be 4 hours for a node that gets tainted with `example.org/maintenance`.

The [update_status](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/main/controllers/update_status.go) subreconciler will be extended to mark process groups running on a tainted node with the new condition.
This can be achieved by iterating over all Pods and make requests to the node object, one example implementation could look like this:

```go
func checkProcessGroupsRunningOnTaintedNodes(pods []*corev1.Pod, status *fdbv1beta2.FoundationDBClusterStatus) {
    if !cluster.NodeFailureDetectionEnabled() {
        return
    }

    nodeTaintedMap := internal.GetNodes(pods) // This will generate a map containing the node name as key and a boolean value indicating if the node is "tainted"
    for node, _ := range nodeMap {
        // Check if node has a taint for the minimum duration in that case we will set the value to true.
    }

    podMap := internal.CreatePodMap(cluster, pods)
    for _, processGroup := range status.ProcessGroups {
        pod, podExists := podMap[processGroup.ProcessGroupID]
        if !podExists {
            continue
        }

        tainted, nodeFound := nodeTaintedMap[pod.Spec.NodeName]
        if !nodeFound {
            continue
        }

        processGroup.UpdateCondition(fdbv1beta2.RunningOnTaintedNode, tainted, status.ProcessGroups, processGroup.ProcessGroupID)
    }
}
```

The limitation of this approach would be that we only detect Pods running on tainted nodes when a reconciliation is happening (those are not coupled to node events).
To work around this we could extend the operator and let the operator watch node events, that could be potentially a high volume of events, so we have to make sure we get the right signals.
The controller-runtime already supports to let the operator watch resources that are not managed by itself, see [Watching Externally Managed Resources](https://book.kubebuilder.io/reference/watching-resources/externally-managed.html).

## Related Links

- [Kubernetes taint and tolerations](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration)
- [Watching Externally Managed Resources](https://book.kubebuilder.io/reference/watching-resources/externally-managed.html)
