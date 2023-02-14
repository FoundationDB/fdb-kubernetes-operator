# Tainted Node Eviction

## Metadata

* Authors: @johscheuer
* Created: 2023-02-14
* Updated: 2023-02-14

## Background

The operator is currently able to detect failures of Pods and replace them if required, but there is still a gap between an issue that manifests in issue that are detected by the operator and issues that could potentially happen.
In Kubernetes it's common to use [taints](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration) to indicate potential problems with a node or planned maintenance.
This information could be consumed by the operator to proactively replace Pods that are running on node with a given set of taints.

## General Design Goals

The goal of this design is to implement a mechanism that allows the operator to replace Pods that are running on nodes with a taint.

## Current Implementation

We don't have a current implementation of this and the operator is currently not able to access any node information.
The closest implementation to this is the [Replace Failed ProcessGroups reconciler](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/main/controllers/replace_failed_process_groups.go), which could be extended for this.

## Proposed Design

We have to make sure that this feature can be enabled and disabled, e.g. if someone wants to run the operator inside a cluster without node access.
The `AutomaticReplacementOptions` will be extended to support configuration of replacements for Pods that run on tainted nodes.
The new field will be called `taints` and will contain a list, default would be empty, of taint configurations.
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

The actual implementation will extend the [ReplaceFailedProcessGroups](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/main/internal/replacements/replace_failed_process_groups.go) to handle those cases.

```go
    if !cluster.NodeFailureDetectionEnabled() {
		return hasReplacements
    }

	// Only replace process groups if we can do a replacement. We could also track this as an independent metric.
	if maxReplacements <= 0 {
		return hasReplacements
    }

	pods, err := r.PodLifecycleManager.GetPods(ctx, r, cluster, internal.GetPodListOptions(cluster, "", "")...)
    if err != nil {
        return &requeue{curError: err}
    }

	nodeMap := internal.GetNodeMap(pods) // This will generate a map containing the node name as key and the pods as value.
	for node, pods := range nodeMap {
		// Check if node has taint for the expected duration
		// in that case we will replace all Pods of that node.
    }
```

The limitation of this approach would be that we only detect Pods running on tainted nodes when a reconciliation is happening (those are not coupled to node events).
To work around this we could extend the operator and let the operator watch node events, that could be potentially a high volume of events, so we have to make sure we get the right signals.
The controller-runtime already supports to let the operator watch resources that are not managed by itself, see [Watching Externally Managed Resources](https://book.kubebuilder.io/reference/watching-resources/externally-managed.html).

## Related Links

- [Kubernetes taint and tolerations](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration)
- [Watching Externally Managed Resources](https://book.kubebuilder.io/reference/watching-resources/externally-managed.html)
