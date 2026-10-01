# Backup expiration through the backup resource

## Decision

`FoundationDBBackup.spec.expiration.beforeTimestamp` exposes a fixed expiration cutoff. The operator runs `fdbbackup expire` without `--force` in an owned Kubernetes Job. FoundationDB decides whether the remaining snapshots and logs are restorable. The API does not expose version cutoffs, restorability overrides, or recurring retention.

Expiration can take substantial time and resources. A Job keeps that work outside reconciliation workers and supplies execution deadlines, retries, and logs. The Job reuses the backup pod configuration, with only the command container and init containers, so it receives the same storage credentials and cluster-file setup as backup agents.

The controller persists the cutoff, destination, source cluster, and deterministic Job name before starting work. It polls the Job until a terminal condition is observed. Successful requests remain recorded even if the Job is deleted, and unrelated backup changes do not repeat them. A new cutoff waits for an existing Job to finish. Failed Jobs are retained; deleting a failed Job explicitly retries the pinned request. Deletion cleanup waits for an active expiration Job.

## Alternatives

- Running the command directly in the controller would block reconciliation workers for large backups and tie command lifetime to the operator process.
- A custom in-memory expiration queue, as described in the original backup design, would duplicate Kubernetes Job lifecycle management and require recovery after operator restarts.
- A separate expiration CRD would add another resource and controller for an operation already scoped to a managed backup. A field on the backup resource reuses its destination and runtime configuration.
- A rolling retention policy would require scheduling and retention semantics beyond a user-selected cutoff. A fixed cutoff provides the requested operation without introducing that behavior.
