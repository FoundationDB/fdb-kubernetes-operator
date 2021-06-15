# Multi FDB cluster support in the kubectl FDB plugin

## Metadata

* Authors: @johscheuer
* Created: 2021-03-06
* Updated: 2021-03-29

## Background

The FDB Kubernetes operator supports to setup and operate FDB clusters span across
multiple Kubernetes clusters. The setup is more or less manual with the
following steps:

- Bootstrap the seed cluster and wait until it's reconciled
- Adjust the database configuration and bootstrap additional
FDB clusters managed by the Operator. Updating the cluster means adjusting all manifests
across the Kubernetes clusters at the same time (or in a short time span).

The tooling for managing FDB clusters (the kubectl FDB plugin) is currently not aware of
FDB clusters spread across multiple Kubernetes clusters or even multiple manifests.

## General Design Goals

* Propose a way how to define the cluster location.
* Bootstrap HA FDB clusters with `kubectl fdb`.
* Make `kubectl fdb` aware of multiple FDB clusters forming an HA cluster.

## Proposed Design

The idea is to use two additional fields in the CRD that will be ignored by the operator but used by the `kubectl fdb plugin`.
The fields are `seedCluster` and `targetClusters`, both contain the following `cluster objects`:

```yaml
name: sample-cluster-dc3 # Optional: Will overwrite the cluster name in the resulting manifest otherwise uses the metadata
dcID: dc3 # Must match with the data center ID specified under datacenters
context: my-kube-ctx 3 # Optional: the kubconfig context to use otherwise the current context will be used
namespace: dc3 # Optional: Otherwise the namespace from the metadata will be used
# Instead of the dcID we can also specify the zoneID and zoneIdx to create a cluster across multiple Kubernetes clusters.
zoneID: zone1 # defines the name of the zone
zoneIdx: 1 # defines the zone index
```

For more information about the fault domains see the according [docs](../manual/fault_domains.md).
It's not allowed to specify both the `dcID` and the `zoneID` the user has to choose one of them.
The `targetClusters` will contain a list of all `cluster objects` and the `seedCluster` will contain the `cluster object` to bootstrap the initial
FDB cluster.
An example manifest for the `kubectl fdbq plugin` that bootstraps a multi-region cluster, could look like this:

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  labels:
    cluster-group: sample-cluster
  name: sample-cluster
spec:
  version: 6.2.29
  databaseConfiguration:
    redundancy_mode: "double"
    usable_regions: 2
    regions:
      - datacenters:
          - id: dc1
            priority: 1
          - id: dc3
            satellite: 1
            priority: 1
        satellite_logs: 3
      - datacenters:
          - id: dc2
            priority: 0
          - id: dc3
            satellite: 1
            priority: 1
        satellite_logs: 3
  seedCluster:
    name: sample-cluster-dc1
    dcID: dc1
    context: my-kube-ctx
    namespace: dc1
  targetClusters:
  - name: sample-cluster-dc2
    dcID: dc2
    context: my-kube-ctx
    namespace: dc2
  - name: sample-cluster-dc3
    dcID: dc3
    context: my-kube-ctx
    namespace: dc3
```

The `databaseConfiguration` must be configured how it should be when all FDB clusters are up and running.
The following fields will be automatically set by the `kubectl fdb plugin` when creating the object in Kubernetes:

```yaml
instanceIDPrefix: $dcID
dataCenter: $dcID
seedConnectionString: $connectionString # The seed connection string will be set once the initial cluster is bootstrapped
```

For a `multi-Kubernetes` cluster the configuration could look like this:

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  labels:
    cluster-group: sample-cluster
  name: sample-cluster
spec:
  version: 6.2.29
  seedCluster:
    zoneID: zone1
    zoneIdx: 1
    context: my-kube-ctx-1
    namespace: sample
  targetClusters:
  - zoneID: zone2
    zoneIdx: 2
    context: my-kube-ctx-2
    namespace: sample
  - zoneID: zone3
    zoneIdx: 3
    context: my-kube-ctx-3
    namespace: sample
```

The following fields will be automatically set by the `kubectl fdb plugin` when creating the object in Kubernetes:

```yaml
instanceIDPrefix: $zoneID
zoneID: $zoneID
zoneIdx: $zoneIdx
seedConnectionString: $connectionString # The seed connection string will be set once the initial cluster is bootstrapped
```

The Operator will be configured to ignore the fields `seedCluster` and `targetClusters` which means that these fields
are only used by the `kubectl fdb plugin` and the human operator.
These fields are optional, if a FDB cluster is limited to a single namespace in a single Kubernetes cluster these fields can be omitted.

The `kubectl fdb plugin` will take the following steps when creating a FDB cluster across multiple Kubernetes clusters (or namespaces):

1. Create the seed cluster in the `seedcluster` with a single region configuration.
1. Wait until the cluster has fully reconciled.
1. Fetch the connection string from the seed cluster.
1. Update the seed cluster with the database configuration.
1. Create all additional clusters withe the fetched connection string as `seedConnectionString`.
1. (optional) wait until all clusters have reconciled.

The implementation will be split up in the following issues:

1. Support for creating multi dc/kc FDB clusters.
1. Support for extending an existing FDB cluster into a multi dc/kc FDB cluster.
1. Support for deleting multi dc/kc FDB clusters.
1. Support for updating multi dc/kc FDB clusters.
1. Add multi dc/kc support for existing plugin commands.

We won't support in the initial implementation:

* Different settings on the cluster Spec besides `processCounts` and on the metadata `name` and `namespace`.

## Related Links

This touches on multiple recent areas of work:

* [The FDB plugin support FDB clusters that span across multiple Kubernetes clusters](https://github.com/FoundationDB/fdb-kubernetes-operator/issues/482)
* [Multi Kubernetes example](https://github.com/FoundationDB/fdb-kubernetes-operator/tree/master/config/samples/multi_kc)
* [Multi namespace example](https://github.com/FoundationDB/fdb-kubernetes-operator/tree/master/config/samples/multi_dc)
