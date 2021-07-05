# Three datahall and three datacenter redundancy mode

## Metadata

* Authors: @johscheuer
* Created: 2020-07-05
* Updated: 2020-07-05

## Background

Many Cloud Provder offer regional Kubernetes clusters that are span across multiple avalability zones (AZ).
An avalability zone is an isolated zone in a region and offers users the ability ro create systems that run atop of these failure domains.
For on-premise clusters this could be a rack, a datahall or a data center.
The current implementation of the operator only supports either single FoundationDB clusters with the redundancy modes `single`, `dobule`, `triple` or HA clusters span across multiple Kubernetes clusters.
FoundationDB offers the redundnacy mode [three_data_hall](https://apple.github.io/foundationdb/configuration.html#single-datacenter-modes) or [three_datacenter](https://apple.github.io/foundationdb/configuration.html#datacenter-aware-mode) that fits well in these enviornments without the overhead of the HA solution.
The current requirement for `three_data_hall` and `three_datacenter` is to have 3 AZs and at least two different failure zones per `data_hall`/`datacenter`.

## General Design Goals

The goal of this design is to support the redundancy modes `three_data_hall` and `three_datacenter` in different deployment models:

* Single Kubernetes cluster across multiple AZs.
* One Kubernetes cluster per AZ.

The implemenation should be flexible enough to give the user a choice of the used AZ.

## Current Implementation

The current version of the operator doesn't support this redundancy mode.
The locality is currently a mix of some constants, a configurable zone and two settings at the cluster level [locality configuration](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/master/controllers/cluster_controller.go#L631-L642).

## Proposed Design

The design is split into the two main parts of the deployment: configure the locality and how to deploy the Pods across the cluster.

### Configure locality

The `three_data_hall` deployment requires the `locality_data_hall` locality to be set and the `three_datacenter` requires the `locality_dcid` to be set.
There must be at least and at most 3 different values for these localities.
Additionaly to these localties at least two different `locality_zone` per `data_hall`/`datacenter` is required to fullfill the requirements.
The `locality_zone` can be configured with [FoundationDBClusterFaultDomain](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/master/docs/cluster_spec.md#foundationdbclusterfaultdomain).
In addition to that the user can configure additional variables that the sidecar will use for substition in the [FoundationDBClusterSpec](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/master/docs/cluster_spec.md#foundationdbclusterspec) with the `sidecarVariables` list.
The current implementation would allow to set `dataHall: $AZ` and in `sidecarVariables` we would list `AZ` to define the `locality_data_hall` based on an environment variable that will be replaced by the sidecar with the actual value.
Instead of having these different mechanisims spread across different settings I propose to have a new `localities` setting in the `FoundationDBClusterSpec`:

```yaml
localities:
- key: "locality_data_hall"
  value: ""
  valueFromEnv: ""
  valueFromNode: "topology.kubernetes.io/zone"
  topologyKey: ""
- key: "locality_zone"
  value: ""
  valueFromEnv: "MyFancyZone"
  valueFromNode: ""
  topologyKey: "kubernetes.io/hostname"
```

The `topologyKey` will be used for `topologySpreadConstraints` and for `PodAntiAffinity` and only has to be set if `valueFromNode` is empty, otherwise it will default to that value.
The `FoundationDBClusterFaultDomain` would then be reduced to only have `key`, `zoneCount` and `zoneIndex` since the locality will be configured in `localities`.
For all `localities` that define a `valueFromEnv` we would add the key to the `--substitute-variable` flag.
For `valueFromNode` we have to modify the sidecar to allow it to read labels from Kubernetes nodes (see [Related Links](#related-links)) and pass that information to the according new flag.
This change should provide the most flexibility to the user to define the required/wanted localities.
We would set `locality_instance_id` and `locality_machineid` to the current defaults but also allow the user to define custom localities.
After that change we should deprecate `cluster.Spec.DataCenter` and `cluster.Spec.DataHall`.

### Deployment model

#### Single regional Kubernetes cluster

The assumption here is that the user has a regional Kubernetes cluster that spreads across at least 3 different AZs.
To ensure that all Pods are spread evenly across the AZs the operator should use [Pod Topology Spread Constraints](https://kubernetes.io/docs/concepts/workloads/pods/pod-topology-spread-constraints).

```yaml
  topologySpreadConstraints:
  - maxSkew: 1
    # the topologyKey will be read from the locality
    topologyKey: topology.kubernetes.io/zone 
    whenUnsatisfiable: DoNotSchedule
    labelSelector:
      matchLabels:
        foundationdb.org/fdb-cluster-name: test-cluster
```

The `topologySpreadConstraints` above ensures that all Pods are evenly spread across the different `topology.kubernetes.io/zone` and must be set by the operator.
If a regional cluster contains more than 3 AZ and a user only want to use 3 specific AZs the user has to define a `NodeAffinity`:

```yaml
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
        - matchExpressions:
          - key:  topology.kubernetes.io/zone 
            operator: In
            values:
            - az1
            - az2
            - az3
```

The `NodeAffinity` must be set by the user and the operator doesn't take any action to automatically set the value.
The user would only require to create one `FoundationDBCluster` and set the `redundancy_mode` to `three_data_hall` or `three_datacenter`.

#### Multiple Kubernetes clusters

In this deployment scenario a user would have 3 different Kubernetes clusters where each cluster spans across a different AZ.
One requirement is that all Pods in the different AZs are able to communicate.
The deployment pattern is similar to the [multi Kubernetes deployment](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/master/docs/manual/fault_domains.md#option-2-multi-kubernetes-replication) but we can't use the special key here since that would mean that the `locality_zone` field would be set to the Kubernetes clusters zone ID and for `three_data_hall` or `three_datacenter` we need at least 2 zones per `data_hall`/`data_center`.
The initial deployment would be splitt into two phases:

1. Deploy the seed cluster into one of the Kubernetes clusters and wait until it's fully reconciled.
1. Copy the connection string and use it as `seedConnectionString` for the other two clusters and set `redundancy_mode: "three_data_hall"`.

The initial cluster spec could look like this:

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  labels:
    cluster-group: test-cluster
  name: test-cluster
spec:
  version: 6.2.30
  instanceIDPrefix: az1
  databaseConfiguration:
    redundancy_mode: "triple"
  localities:
  - key: "locality_data_hall"
    valueFromNode: "topology.kubernetes.io/zone"
```

With this configuration we will use the default fault domain the Kubernetes nodes as `locality_zone`.
When the clutser is reconciled we can create the `FoundationDBCluster` spec in the other two Kubernetes clusters and set the `redundancy_mode: "three_data_hall"`:

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  labels:
    cluster-group: test-cluster
  name: test-cluster
spec:
  version: 6.2.30
  instanceIDPrefix: $az
  seedConnectionString: $connectionString
  databaseConfiguration:
    redundancy_mode: "three_data_hall"
  localities:
  - key: "locality_data_hall"
    valueFromNode: "topology.kubernetes.io/zone"
```

The processes in the other Kubernetes clusters will join the current `FoundationDBCluster`.
Once enough processes joined the cluster one of the operator will select 5 Coordinators span across 3 `data_halls`.

### Coordinator selection

The coordinator selecion must be adjusted to select at most 2 coordinators in the same `data_hall` or `datacenter` depending on the redundancy mode.
Currently the selection happens in the [cluster_controller](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/master/controllers/cluster_controller.go#L1283-L1292) with a small extension we can support the coordinator selection:

```go
func getHardLimits(cluster *fdbtypes.FoundationDBCluster) map[string]int {
    // Multi region cluster (HA)
    if cluster.Spec.UsableRegions > 1 {
        return map[string]int{
            fdbtypes.FDBLocalityZoneIDKey: 1, 
            fdbtypes.FDBLocalityDCIDKey: int(math.Floor(float64(cluster.DesiredCoordinatorCount()) / 2.0))
        }
    }

    // Single region cluster (non-HA)
    req := map[string]int{fdbtypes.FDBLocalityZoneIDKey: 1}

    if cluster.Spec.RedundancyMode == fdbtypes.RedundancyModeThreeDataHall {
        req[fdbtypes.FDBLocalityDataHallKey] = 2
    }

    if cluster.Spec.RedundancyMode == fdbtypes.RedundancyModeThreeDatacenter {
        req[fdbtypes.FDBLocalityDatacenterKey] = 2
    }

    return req
}
```

## Related Links

Links to other pages that inform or relate to this design.

* [Allow the sidecar to read labels from nodes](https://github.com/FoundationDB/fdb-kubernetes-operator/issues/817)
* [Support three_data_hall redundancy](https://github.com/FoundationDB/fdb-kubernetes-operator/issues/348)
* [Supporting topologySpreadConstraints in the operator](https://github.com/FoundationDB/fdb-kubernetes-operator/issues/361)
* [Pod Topology Spread Constraints](https://kubernetes.io/docs/concepts/workloads/pods/pod-topology-spread-constraints)
* [Multi Kubernetes deployment](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/master/docs/manual/fault_domains.md#option-2-multi-kubernetes-replication)
