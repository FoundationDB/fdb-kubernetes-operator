# Backup expiration through the backup resource

## Decision

`FoundationDBBackup.spec.expiration` defines recurring retention using `expireAfterSeconds`, optional `restorableForSeconds` or `restorableAfterVersion`, and `intervalSeconds`. Each run converts the relative durations to fixed timestamps and executes `fdbbackup expire` without `--force` in an owned Kubernetes Job. FoundationDB checks that the remaining snapshots and logs satisfy the requested restore window or preserve the explicitly protected version. The default check alone does not protect an older external checkpoint merely because a newer snapshot remains. The checkpoint owner must supply its oldest required database version; the operator does not discover checkpoints.

Jobs keep long-running deletion outside reconciliation workers and provide deadlines, Pod retries, and logs. The Job reuses the backup pod configuration with only the main container and init containers. Expiration does not need encryption flags or decryption keys.

The controller uses the destination populated by `updateBackupStatus`, then persists it together with the source cluster, policy, cutoffs, and deterministic Job name before creating the Job. These fields pin an accepted run across restarts and spec changes; they do not support managing an inventory of historical destinations.

Completion records the next run time. Failed Jobs use exponential backoff from the configured interval, capped at one day, and success resets the backoff. The schedule survives spec changes, terminal Job deletion, and operator restarts. The latest Job is retained for diagnosis until the next run, then removed with foreground propagation before another Job is accepted. Removing the policy finishes any active Job and removes it before clearing status. Backup deletion cleanup uses the same foreground deletion barrier.

Expiration progress is reported in `status.expiration` and does not gate backup generation reconciliation. The operator polls active Jobs at a configurable interval, defaulting to one minute. The next reconciliation uses the earlier of the backup retry and expiration deadlines. Idle backups requeue at the next run time. Kubernetes API errors use the controller's existing error rate limiter.

## Alternatives

- A fixed timestamp requires users to build a separate scheduler and adds a permanent API field for a one-time operation.
- Running expiration directly in the controller blocks reconciliation workers and ties command lifetime to the operator process.
- An in-memory queue duplicates Kubernetes Job lifecycle handling and loses scheduling state across restarts.
- A separate expiration CRD adds another resource and controller for an operation already scoped to a managed backup.
