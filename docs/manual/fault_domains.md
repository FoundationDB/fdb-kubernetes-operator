# Controlling Fault Domains

The operator provides multiple options for defining fault domains for your cluster. The fault domain defines how data is replicated and how processes and coordinators are distributed across machines. Choosing a fault domain is an important process of managing your deployments.

Fault domains are controlled through the `faultDomain` field in the cluster spec.

## Option 1: Single-Kubernetes Replication

The default fault domain strategy is to replicate across nodes in a single Kubernetes cluster. If you do not specify any fault domain option, we will replicate across nodes. This is equivalent to the following configuration:

```yaml
apiVersion: apps.foundationdb.org/v1beta2
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 7.1.26
  faultDomain:
    key: kubernetes.io/hostname
    valueFrom: spec.nodeName
```

This will create a pod anti-affinity rule preventing multiple pods of the same process class for the same cluster from being on the same node. This will also set up the monitor conf so that it uses the value from `spec.nodeName` on the pod as the `zoneid` locality field.

You can change the fault domain configuration to use a different field as well:

```yaml
apiVersion: apps.foundationdb.org/v1beta2
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 7.1.26
  faultDomain:
    key: topology.kubernetes.io/zone
    valueFrom: spec.zoneName
```

The example above divides processes across nodes based on the label `topology.kubernetes.io/zone` on the node, and sets the zone locality information in FDB based on the field `spec.zoneName` on the pod. The latter field does not exist, so this configuration cannot work. There is no clear pattern in Kubernetes for allowing pods to access node information other than the host name, which presents challenges using any other kind of fault domain.

If you have some other mechanism to make this information available in your pod's environment, you can tell the operator to use an environment variable as the source for the zone locality:

```yaml
apiVersion: apps.foundationdb.org/v1beta2
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 7.1.26
  faultDomain:
    key: topology.kubernetes.io/zone
    valueFrom: $RACK
```

This will set the `zoneid` locality to whatever is in the `RACK` environment variable for the containers providing the monitor conf, which are `foundationdb-kubernetes-init` and `foundationdb-kubernetes-sidecar`.

## Option 2: Multi-Kubernetes Replication

Our second strategy is to run multiple Kubernetes cluster, each as its own fault domain. This strategy adds significant operational complexity, but may allow you to have stronger fault domains and thus more reliable deployments. You can enable this strategy by using a special key in the fault domain:

```yaml
apiVersion: apps.foundationdb.org/v1beta2
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 7.1.26
  processGroupIDPrefix: zone2
  faultDomain:
    key: foundationdb.org/kubernetes-cluster
    value: zone2
    zoneIndex: 2
    zoneCount: 5
```

This tells the operator to use the value "zone2" as the fault domain for every process it creates. The zoneIndex and zoneCount tell the operator where this fault domain is within the list of Kubernetes clusters (KCs) you are using in this DC. This is used to divide processes across fault domains. For instance, this configuration has 7 stateless processes, which need to be divided across 5 fault domains. The zones with zoneIndex 1 and 2 will allocate 2 stateless processes each. The zones with zoneIndex 3, 4, and 5 will allocate 1 stateless process each.

When running across multiple KCs, you will need to apply more care in managing the configurations to make sure all the KCs converge on the same view of the desired configuration. You will likely need some kind of external, global system to store the canonical configuration and push it out to all of your KCs. You will also need to make sure that the different KCs are not fighting each other to control the database configuration.

You must always specify an `processGroupIDPrefix` when deploying an FDB cluster to multiple Kubernetes clusters.
You must set it to a different value in each Kubernetes cluster.
This will prevent process group ID duplicates in the different Kubernetes clusters.

## Option 3: Fake Replication

In local test environments, you may not have any real fault domains to use, and may not care about availability. You can test in this environment while still having replication enabled by using fake fault domains:

```yaml
apiVersion: apps.foundationdb.org/v1beta2
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 7.1.26
  faultDomain:
    key: foundationdb.org/none
```

This strategy uses the pod name as the fault domain, which allows each process to act as a separate failure domain. Any hardware failure could lead to a complete loss of the cluster. This configuration should not be used in any production environment.

## Multi-Region Replication

The replication strategies above all describe how data is replicated within a data center.
They control the `zoneid` field in the cluster's locality.
If you want to run a cluster across multiple data centers, you can use FoundationDB's multi-region replication.
This can work with any of the replication strategies above.
The data center will be a separate fault domain from whatever you provide for the zone.

```yaml
apiVersion: apps.foundationdb.org/v1beta2
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 7.1.26
  dataCenter: dc1
  databaseConfiguration:
    regions:
      - datacenters:
          - id: dc1
            priority: 1
          - id: dc2
            priority: 1
            satellite: 1
      - datacenters:
          - id: dc3
            priority: 0
          - id: dc4
            priority: 1
            satellite: 1
```

The `dataCenter` field in the top level of the spec specifies what data center these process groups are running in.
This will be used to set the `dcid` locality field.
The `regions` section of the database describes all of the available regions.
See the [FoundationDB documentation](https://apple.github.io/foundationdb/configuration.html#configuring-regions) for more information on how to configure regions.

Replicating across data centers will likely mean running your cluster across multiple Kubernetes clusters, even if you are using a single-Kubernetes replication strategy within each DC.
This will mean taking on the operational challenges described in the "Multi-Kubernetes Replication" section above.

An example on how to setup a multi-region FDB cluster with the operator can be found in [multi-dc](../../config/tests/multi_dc).
If you want to do an experiment locally with Kind you can use the [setup_e2e.sh](../../scripts/setup_e2e.sh) script.
Basically the example is performing the following steps:

Create a single DC FDB cluster:

```yaml
apiVersion: apps.foundationdb.org/v1beta2
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 7.1.26
  dataCenter: dc1
  # Using the processGroupIDPrefix will prevent name conflicts.
  processGroupIDPrefix: dc1
  databaseConfiguration:
    regions:
      - datacenters:
          - id: dc1
            priority: 1
```

Once the cluster is fully reconciled you can create the FDB clusters in the other DCs, now with the full configuration and a `seedConnectionString`:

```yaml
apiVersion: apps.foundationdb.org/v1beta2
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 7.1.26
  dataCenter: dc1
  processGroupIDPrefix: dc1
  seedConnectionString: # Replace with the value from the initial single DC cluster
  databaseConfiguration:
    regions:
      - datacenters:
          - id: dc1
            priority: 1
          - id: dc2
            priority: 1
            satellite: 1
      - datacenters:
          - id: dc3
            priority: 0
          - id: dc4
            priority: 1
            satellite: 1
```

## Coordinating Global Operations

When running a FoundationDB cluster that is deployed across multiple Kubernetes clusters, each Kubernetes cluster will have its own instance of the operator working on the processes in its cluster. There will be some operations that cannot be scoped to a single Kubernetes cluster, such as changing the database configuration. The operator provides a locking system to ensure that only one instance of the operator can perform these operations at a time. You can enable this locking system by setting `lockOptions.disableLocks = false` in the cluster spec. The locking system is automatically enabled by default for any cluster that has multiple regions in its database configuration, or a `zoneCount` greater than 1 in its fault domain configuration.

The locking system uses the `processGroupIDPrefix` from the cluster spec to identify an process group of the operator.
Make sure to set this to a unique value for each Kubernetes cluster, both to support the locking system and to prevent duplicate process group IDs.

This locking system uses the FoundationDB cluster as its data source. This means that if the cluster is unavailable, no instance of the operator will be able to get a lock. If you hit a case where this becomes an issue, you can disable the locking system by setting `lockOptions.disableLocks = true` in the cluster spec.

In most cases, restarts will be done independently in each Kubernetes cluster, and the locking system will be used to ensure a minimum time between the different restarts and avoid multiple recoveries in a short span of time. During upgrades, however, all instances must be restarted at the same time. The operator will use the locking system to coordinate this. Each instance of the operator will store records indicating what processes it is managing and what version they will be running after the restart. Each instance will then try to acquire a lock and confirm that every process reporting to the cluster is ready for the upgrade. If all processes are prepared, the operator will restart all of them at once. If any instance of the operator is stuck and unable to prepare its processes for the upgrade, the restart will not occur.

### Deny List

There are some situations where an instance of the operator is able to get locks but should not be trusted to perform global actions.
For instance, the operator could be partitioned in a way where it cannot access the Kubernetes API but can access the FoundationDB cluster.
To block such an instance from taking locks, you can add it to the `denyList` in the lock options.
You can set this in the cluster spec on any Kubernetes cluster.

```yaml
apiVersion: apps.foundationdb.org/v1beta2
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  processGroupIDPrefix: dc1
  lockOptions:
    denyList:
      - id: dc2
```

This will clear any locks held by `dc2`, and prevent it from taking further locks.
In order to clear this deny list, you must change it to allow that instance again:

```yaml
apiVersion: apps.foundationdb.org/v1beta2
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  processGroupIDPrefix: dc1
  lockOptions:
    denyList:
      - id: dc2
        allow: true
```

Once that change is fully reconciled, you can clear the deny list from the spec.

## Managing Disruption

[Pod disruption budgets](https://kubernetes.io/docs/tasks/run-application/configure-pdb/)
are a good idea to prevent simultaneous disruption to many components in a
cluster, particularly during the upgrade of nodepools in public clouds. The
operator does not yet create these automatically. To aid in creation of PDBs the
operator preferentially selects coordinators from just storage pods, then if
there are not enough storage pods, or the storage pods are not spread across
enough fault domains it also considers log pods, and finally transaction pods as
well.

## Coordinators

Per default the FDB operator will try to select the best fitting processes to be coordinators.
Depending on the requirements the operator can be configured to either prefer or exclude specific processes.
The number of coordinators is currently a hardcoded mechanism based on the [following algorithm](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/v0.49.2/api/v1beta1/foundationdbcluster_types.go#L1500-L1508):

```go
func (cluster *FoundationDBCluster) DesiredCoordinatorCount() int {
	if cluster.Spec.DatabaseConfiguration.UsableRegions > 1 {
		return 9
	}

	return cluster.MinimumFaultDomains() + cluster.DesiredFaultTolerance()
}
```

For all clusters that use more than one region the operator will recruit 9 coordinators.
If the number of regions is `1` the number of recruited coordinators depends on the redundancy mode.
The number of coordinators is chosen based on the fact that the coordinators use a consensus protocol (Paxos) that needs a majority of processes to be up.
The FoundationDB document has more information about [choosing coordination servers](https://apple.github.io/foundationdb/configuration.html#choosing-coordination-servers).

|  Redundancy mode  | # Coordinators |
|---|----------------|
| Single  | 1              |
| Double (default)  | 3              |
| Triple  | 5              |

Every coordinator must be in a different zone.
That means for `Triple` replication you need at least 5 different Kubernetes nodes with the default fault domain.
Losing one Kubernetes node will lead to have only 4 coordinators since the operator can't recruit another 5th coordinator
across different zones.

### Coordinator selection

The operator offers a flexible way to select different process classes to be eligible for coordinator selection.
Per default the operator will choose all `stateful` processes classes e.g. `storage`, `log` and `transaction`.
In order to get a deterministic result the operator will sort the candidate by priority (per default all have the same priority) and then by the `instance-id`.

If you want to modify the selection process you can add a `coordinatorSelection` in the `FoundationDBCluster` spec:

```yaml
spec:
coordinatorSelection:
- priority: 0
  processClass: log
- priority: 10
  processClass: storage
```

Only process classes defined in the `coordinatorSelection` will be considered as possible candidates.
In this example only processes with the class `log` or `storage` will be used for coordinators.
The priority defines if a specific process class should be preferred to another.
In this example the processes with the class `storage` will be preferred over processes with the class `log`.
That means that a `log` process will only be considered a valid coordinator if there are no other `storage` processes that can be selected without hurting the fault domain requirements.
Changing the `coordinatorSelection` can result in new coordinators e.g. if the current preferred class will be removed.

The operator supports the following classes as coordinators:

- `storage`
- `log`
- `transaction`
- `coordinator`

### Known limitations

FoundationDB clusters that are spread across different DC's or Kubernetes clusters only support the same `coordinatorSelection`.
The reason behind this is that the coordinator selection is a global process and different `coordinatorSelection` of the `FoundationDBCluster` resources can lead to an undefined behaviour or in the worst case flapping coordinators.
There are plans to support this feature in the future.

## Next

You can continue on to the [next section](tls.md) or go back to the [table of contents](index.md).
