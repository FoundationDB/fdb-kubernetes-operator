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
 - How many `instancesToRemove` are currently in the list

 This list is not complete and will be extended over time.
