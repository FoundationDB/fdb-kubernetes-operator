# Coordinator selection

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

## Known limitations

FoundationDB clusters that are spread across different DC's or Kubernetes clusters only support the same `coordinatorSelection`.
The reason behind this is that the coordinator selection is a global process and different `coordinatorSelection` of the `FoundationDBCluster` resources can lead to an undefined behaviour or in the worst case flapping coordinators.
There are plans to support this feature in the future.
