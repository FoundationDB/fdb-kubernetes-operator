# Monitoring

The FoundationDB Kubernetes operator provides a metrics endpoint in the [Prometheus format](https://prometheus.io/docs/concepts/data_model).
The collected metrics can be used to understand the current state of the operator and state of the cluster from the operators view.
The operator metrics are not a replacement for the collection of the [FoundationDB metrics](https://forums.foundationdb.org/t/what-do-you-monitor/184).
Per default the operator expose the metrics under `$POD_IP:8080/metrics`.

## Metrics

The operator exposes the `controller-runtime` and `golang` metrics.
Besides, these metrics the operator also exposes operator specific metrics, the metric prefix is `fdb_operator`.
The operator specific metrics contain information about:

 - The process groups
 - The reconciliation status
 - The cluster status
 - How many `processGroupsToRemove` are currently in the list
 - The versions of the cluster, see [Version metric](#version-metric)

 This list is not complete and will be extended over time.

### Version metric

The metric `fdb_operator_cluster_version_info` exposes the versions of a cluster as labels, the value is always `1`:

| Label | Description |
|---|---|
| `namespace`, `name` | The namespace and the name of the `FoundationDBCluster` resource |
| `desired_version` | The version defined in `spec.version` |
| `running_version` | The version the `fdbserver` processes are running, `status.runningVersion` |
| `monitor_version` | The *desired* version of the main container image, which runs the `fdb-kubernetes-monitor`, `status.monitorVersion`. This is the `monitorVersion` of the main container if it is [pinned](manual/upgrades.md#pinned-main-container-image), otherwise the `desired_version`. The value is derived from the spec and does not reflect the images of the running Pods, which can still use the previous image during a rolling update |

The series changes when one of the versions changes, which means old label combinations disappear.
The metric can be used to find clusters with a pinned main container image that lags behind the version of the cluster, for example by alerting on clusters where `monitor_version` differs from `desired_version` for a long time (the versions are equal for clusters without a pinned image).
To find Pods that still run an old image during a rolling update, compare the images of the Pods, for example with the `kube-state-metrics` container image information.
