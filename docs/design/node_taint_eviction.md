# Tainted Node Eviction

## Metadata

* Authors: @johscheuer, @xumengpanda
* Created: 2023-02-14
* Updated: 2023-05-11

## Background

The operator is currently able to detect failures of Pods and replace them if required, the replacement is happening based on the [conditions that need replacement](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/v1.14.0/api/v1beta2/foundationdbcluster_types.go#L65).
One of the missing pieces in failure mitigation is to evict Pods from tainted Nodes.
In Kubernetes it's common to use [taints](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration) to indicate potential problems with a node or planned maintenance.
This information could be consumed by the operator to proactively replace Pods that are running on Nodes with a given set of taints.

## General Design Goals

The goal of this design is to implement a mechanism that allows the operator to replace Pods that are running on Nodes with a given set of taints.
It's expected that the operator will detect the Pods running on tainted Nodes in a timely manner, either by receiving an event triggering the reconciliation loop or by periodic reconciliation events that happen every 10h by default. Once the operator marks Pods on tainted Nodes as `NodeTaintReplacing`, the operator will re-use the replacement logic to replace those Pods. The actual replacement speed depends on the speed of excluding processes, the number of Pods to replace, and the allowed maximum number of inflight replacements (defined by `automationOptions.replacements.maxConcurrentReplacements`).

## Current Implementation
The operator does not have this feature. It does not have access to list Node information. It does not consider taint-related events in Pod's condition.

The operator does replace failed ProcessGroups in the [Replace Failed ProcessGroups reconciler](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/main/controllers/replace_failed_process_groups.go). It can extend it to support this feature.

## Proposed Design

### Design Requirements
**Requirement 1.** The feature can be enabled and disabled by users, because (1) when someone wants to run the operator inside a k8s cluster without Node access, they should be able to bypass this feature; (2) when we notice problems during roll-out of this feature, we need to disable it.

The feature can be disabled by option `automationOptions.replacements.taintReplacementOptions = {}`. YAML example is as follows. Please note that `taintReplacementOptions` section is deleted.

```yaml
...
automaticReplacementOptions:
  enabled: true
  failureDetectionTimeSeconds: 7200
  maxConcurrentReplacements: 1
  enableNodeFailureDetection: true
```

**Requirement 2.** The feature allows users to configure a set of specific taint keys to detect and replace.

Users can configure the taint feature in the new field `taints` inside `automaticReplacementOptions`. The `taints` field is a list of `(key, durationInSeconds)` pairs, which defines operator's behavior on the taint key.

Users can use `automaticReplacementOptions.taintReplacementTimeSeconds` to control how fast operator should replace Pods on tainted Nodes. Users can configure operator to replace Pods on tainted Nodes much faster than the normal replacement by setting a smaller value of `taintReplacementTimeSeconds`.

Example configuration:

```yaml
...
automaticReplacementOptions:
  enabled: true
  failureDetectionTimeSeconds: 7200
  maxConcurrentReplacements: 1
  enableNodeFailureDetection: true
  taintReplacementOptions:
  - key: "example.org/maintenance"
    durationInSeconds: 7200 # Mark Pods to be replaced after this taint key has been on the Node for at least 7200 seconds (i.e., 2 hours).
  - key: "example.org/disconnected"
    durationInSeconds: 1200 # Mark Pods to be replaced after this taint key has been on the Node for at least 1200 seconds (i.e., 20 mins).
  - key: "*" # The wildcard will match all taint keys
    durationInSeconds: 3600 # Mark Pods to be replaced after a taint key, which does not have exact match in the taints list, has been on the Node for at least 3600 seconds.
  taintReplacementTimeSeconds: 1800 # A Pod marked to be replaced due to taints must stay in the to-be-replaced state for 1800 seconds before it is automatically replaced.
```

In the above configuration, it takes at least 3000 seconds (1200 + 1800) for the operator to replace a Pod on a Node with taint key `example.com/disconnected`, because it takes `durationInSeconds`, i.e., 1200 seconds, to mark the Pod to be replaced and it takes another `taintReplacementTimeSeconds`, i.e., 1800 seconds, to start replacing the Pod.

**Requirement 3.** The feature lists the taint status for process groups. This allows users to have a holistic view of which Pods in a FDB cluster are running on tainted Nodes and being replaced.

### Implementation

We need to make the following four types of changes:

**CRD change.** We need to extend the `automaticReplacementOptions` with the new fields `taints` and `taintReplacementTimeSeconds`

**Detect Node taint and change ProcessGroup condition.**  We can extend the existing `validateProcessGroup()` function in `update_status` reconciler to detect if a Pod is running on a tainted Node and mark the ProcessGroup's taint-related condition accordingly.

**Status of ProcessGroup taint-related condition.** We will add two [ProcessGroupConditionType](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/main/api/v1beta2/foundationdbcluster_types.go#L673):
- `NodeTaintDetected` represents a ProcessGroup Node is tainted, but not long enough to replace it.
- `NodeTaintReplacing` represents a ProcessGroup whose Node has been tainted for longer-than-configured time and the ProcessGroup should be replaced.

**Replace ProcessGroups with `NodeTaintReplacing` condition.** We can extend the existing replacement logic to replace ProcessGroups with `NodeTaintReplacing` condition. We can do this by adding the `NodeTaintReplacing` condition to [`conditionsThatNeedReplacement` list](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/d38d6bc7abf7764215976b75d581c933280389f8/api/v1beta2/foundationdbcluster_types.go#L65-L67)


## Limitations
The operator will only detect tainted Nodes when its reconciliation is triggered. This may increase the delay of detecting and replacing Pods on tainted Nodes.

The limitation can be resolved by letting the operator watch node events. The controller-runtime already supports to let the operator watch resources that are not managed by itself, see [Watching Externally Managed Resources](https://book.kubebuilder.io/reference/watching-resources/externally-managed.html). [Tracked issue#1629](https://github.com/FoundationDB/fdb-kubernetes-operator/issues/1629)

## Related Links

- [Kubernetes taint and tolerations](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration)
- [Watching Externally Managed Resources](https://book.kubebuilder.io/reference/watching-resources/externally-managed.html)
