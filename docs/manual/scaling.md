# Scaling a Cluster

## Managing Process Counts

You can manage process counts in either the database configuration or in the process counts in the cluster spec. In most of these examples, we will only manage process counts through the database configuration. This is simpler, and it ensures that the number of podNames we launch fits the number of podNames that we are telling the database to recruit.

To explicitly set process counts, you could configure the cluster as follows:

```yaml
apiVersion: apps.foundationdb.org/v1beta2
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.30
  processCounts:
    storage: 6
    log: 5
    stateless: 4
```

This will configure 6 storage podNames, 5 log podNames, and 4 stateless podNames. This is fewer stateless podNames that we had by default, which means that some podNames will be running multiple roles. This is generally something you want to avoid in a production configuration, as it can lead to high activity on one role starving another role of resources.

By default, the operator will provision podNames with the following process types and counts:

1. `storage`. Equal to the storage count in the database configuration. If no storage count is provided, this will be `2*F+1`, where `F` is the desired fault tolerance. For a double replicated cluster, the desired fault tolerance is 1.
2. `log`. Equal to the `F+max(logs, remote_logs)`. The `logs` and `remote_logs` here are the counts specified in the database configuration. By default, `logs` is set to 3 and `remote_logs` is set to either `-1` or `logs`.
3. `stateless`. Equal to the sum of all other roles in the database configuration + `F`. Currently, this is `max(proxies+resolvers+4, log_routers)`. The `4` is for the master, cluster controller, data distributor, and ratekeeper podNames. This may change in the future as we add more roles to the database. By default, `proxies` is set to 3, `grv_proxies` is set 1, `commit_proxies` is set to 2, `resolvers` is set to 1, and `log_routers` is set to -1. If the version of FDB in the cluster is less than 7.0.0, the process counts will use the value of proxies set (or the default) and FDB will apply a ratio to how the individual proxy roles are distributed. If version of FDB is greater than 7.0.0 and the separated proxy counts are set while the old `proxies` are not, we will use `proxies=sum(grv_proxies + commit_proxies)`.

You can also set a process count to -1 to tell the operator not to provision any podNames of that type.

## Growing a Cluster

Instead of setting the process counts directly, let's update the counts of recruited roles in the database configuration:

```yaml
apiVersion: apps.foundationdb.org/v1beta2
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.30
  databaseConfiguration:
    storage: 6
    logs: 4 # default is 3
    proxies: 5 # default is 3
    resolvers: 2 # default is 1
```

This will provision 1 additional log process and 3 additional stateless podNames. After launching those podNames, it will change the database configuration to recruit 1 additional log, 2 additional proxies, and 1 additional resolver.

## Shrinking a Cluster

You can shrink a cluster by changing the database configuration or process count, just like when we grew a cluster:

```yaml
apiVersion: apps.foundationdb.org/v1beta2
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.30
  databaseConfiguration:
    storage: 4
```

The operator will determine which podNames to remove and record them as needing removal in the `processGroups` field in the cluster status. This will make sure the choice of removal stays consistent across repeated runs of the reconciliation loop. Once the podNames are in the removal list, we will exclude them from the database, which moves all of the roles and data off of the process. Once the exclusion is complete, it is safe to remove the podNames, and the operator will delete both the pods and the PVCs. Once the podNames are shut down, the operator will re-include them to make sure the exclusion state doesn't get cluttered. It will also remove the process from the `processGroups` list.

The exclusion can take a long time, and any changes that happen later in the reconciliation process will be blocked until the exclusion completes.

If one of the removed podNames is a coordinator, the operator will recruit a new set of coordinators before shutting down the process.

Any changes to the database configuration will happen before we exclude any podNames.

## Changing Replication Mode

You can change the replication mode in the database by changing the field in the database configuration:

```yaml
apiVersion: apps.foundationdb.org/v1beta2
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.30
  databaseConfiguration:
    redundancy_mode: triple
    storage: 5
```

This will run the configuration command on the database, and may also add or remove podNames to match the new configuration.

## Next

You can continue on to the [next section](customization.md) or go back to the [table of contents](index.md).
