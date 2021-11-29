# Backup and Restore in the Operator

## Metadata

* Authors: @brownleej
* Created: 2021-07-20
* Updated: 2021-07-20

## Background

FoundationDB has built-in support for backup and restore through the `fdbbackup` and `fdbrestore` tools. We want to support exposing this functionality through the operator, with custom resources representing continuous backups and one-time restores.

## General Design Goals

* Allow managing backup lifecycle independently of cluster lifecycle
* Share backup tooling with the open-source community
* Provide an abstraction that can be used to manage different backup methodologies

## Proposed Design

### Backup Resource

We will create a FoundationDBBackup resource with the following spec:

* Source cluster name (optional, defaults to backup name, immutable)
* Desired state (running, stopped, paused) (optional, defaults to running)
* Backup destination:
    * Backup account (optional, defaults to first account in the credentials file)
    * Backup endpoint (optional, defaults to first account in the credentials file)
    * Backup path (optional, defaults to backup name)
    * Timestamp Format (optional, formatted as a Go time format description, defaults to "-2006-01-02T15:04:05Z-07:00")
* Backup tag (optional)
* Key range to back up (optional, defaults to the full keyspace)
* Snapshot duration (optional, defaults to 7 days)
* Backup agent template (optional)
* Parameters for backup URL (optional)
* Expiry:
    * Maximum retention time (optional, defaults to no expiry)
    * Minimum restorable time (optional, defaults to no restorability protection in expiry)
    * Enable Deletion Finalizer (optional, default is true)

The resource will have the following status:

* Observed generation
* Conditions (list)
    * Condition Type
    * Timestamp
* Backups (list)
    * Tag
    * Destination
    * Oldest Data
        * Version
        * Timestamp
    * Latest Restorable Point
        * Version
        * Timestamp

In the output for `kubectl get`, we will include the name of the backup, the generation, whether it is fully reconciled, the oldest data timestamp, and the latest restorable timestamp. For this purpose a backup will be considered fully reconciled if it has no conditions in its status.

The possible conditions on the backup will be:

* IncorrectDeployment
* DeploymentRollingOut
* IncorrectBackupState
* MisconfiguredBackup

Wherever we have timestamp formats in the resource specs, they will be interpreted as [Go time formats](https://pkg.go.dev/time#Time.Format).

### Restore Resource

We will create a FoundationDBRestore resource with the following spec:

* Backup URL
* Destination cluster name
* Desired state (running, stopped) (optional, defaults to running)
* Key range to restore (optional, defaults to the full keyspace)
* Version to restore (optional, defaults to latest version)
* Timestamp to restore (optional, defaults to latest version)
* Source cluster name (optional, required if using restore timestamps)
* Restore tag (optional)

All fields in a restore are immutable.

If the restore version and restore timestamp are both specified, the version will take precedence.

### Reconciling Backups

The backup reconciler will run the following stages:

* Check backup status
* Configure backup agents
* Toggle backup running
* Modify backup
* Check backup status

We will use the `fdbbackup` tool to modify the backup state. We will use the backup status that is found within the database status to determine the current state of the backup.

#### Check Backup Status

In the `Check Backup Status` stage, we will compare the running state and validate the following things:

* The state of the backup matches the configured state in the spec
* If the backup is configured to be running, it has completed an initial snapshot
* The backup parameters match the parameters in the spec
* The backup deployment exists, matches the deployment configuration for the spec, and is fully deployed

If any of these checks fail, the backup will be marked as unreconciled, but reconciliation will continue through the later stages whether it is reconciled or not.

#### Configure Backup Agents

In the `Configure Backup Agents` stage, we will create a backup deployment that runs FDB's `backup_agent` process. The agents will be configured through a deployment object in the cluster spec. The deployment will have the following defaults:

* The deployment will have the same name as the backup resource
* The deployment will have 3 replicas
* The deployment will have an init container running the `foundationdb/foundationdb-kubernetes-sidecar` image, configured to copy the cluster file from the same config map that the operator creates for the cluster.
* The deployment will have a main container running the `foundationdb/foundationdb` image, running a `backup_agent` process.

The version of FoundationDB that the backup agents run will be taken from the cluster resource. There must be a cluster resource in the same namespace with the name given in the cluster spec in order to run a backup. This version will also be used for the `fdbbackup` commands. In order to keep these versions in sync, the cluster reconciler will have a stage to check for backups that need to be updated. This stage will find any backup resources that are connected to the cluster and set an annotation, `foundationdb.org/backup-cluster-version` with the version in the cluster spec. During upgrades, the cluster reconciler will set this annotation, which will trigger a reconciliation of the backup that will update the backup agents.

#### Toggle Backup Running

In the `Toggle Backup Running` stage, we will start or stop the backup, if necessary. If the desired state is `stopped`, and there is a backup running on the configured tag, this will run the `fdbbackup abort` command. If the desired state is `running` or `paused`, and there is no backup running on the configured tag, this will run the `fdbbackup start` command.

This will also update the pause state in the backup. If the desired state is `paused` and the backup is not paused, this will run the `fdbbackup pause` command. If the desired state is `running` and the backup is paused, this will run the `fdbbackup resume` command.

In order to support restarting backups when configuration changes, the operator will automatically include a timestamp when it starts a backup. The format of the timestamp will be configurable, and configuring it to an empty string will omit the timestamp. When a change is detected that requires restarting a backup, we will stop the backup and start a new one. We will continue to track both backups in the status of the resource until the old backup is fully deleted.

Doing the initial snapshot for a backup will require more bandwidth than subsequent snapshots, because the initial snapshot tries to complete as fast as it can while subsequent snapshots complete more slowly based on the snapshot interval. If we update all backups at once, it would cause every backup to stop and then start a new backup. This creates a gap in restorability until the new backup completes its initial snapshot. If all of the backups are doing this at once, it could lead to rate limiting on the destination that causes all of the backups to slow down, creating a longer aggregate gap in restorability. To prevent this, we will have a cross-backup throttle to limit how many backups can be in the initial snapshot phase at a time. The default limit will be 1. If this limit has already been reached, then the operator will not start or restart any backups, but it will be able to stop a backup if the backup has a desired state of `stopped`. This limit will only apply within a single KC.

Changing the following fields will require restarting a backup:

* Backup destination
* Tag
* Key Range

#### Modify Backup

In the `Modify Backup` stage, we will update parameters for the backup. If the backup parameters do not match the configured set, this will run the `fdbbackup modify` command.

Changing the following fields will require running the modify command:

* Backup URL parameters
* Snapshot time

### Reconciling Restores

The restore reconciler will run the following stages:

* Toggle restore running
* Check restore status

We will use the `fdbrestore` tool to modify the restore state. We will use the restore status that is found within the database status to determine the current state of the backup.

#### Toggle Restore Running

In the `Toggle Restore Running` stage, we will start or stop the restore, if necessary. If the desired state is `stopped`, and there is a restore running on the configured tag, this will run the `fdbrestore abort` command. If the desired state is `running`, and there is no restore running on the configured tag, this will run the `fdbrestore start` command.

#### Check Restore Status

In the `Check Restore Status` stage, we will compare the running state and validate the following things:

* The state of the restore matches the configured state in the spec
* If the restore is configured to be running, it has completed

If any of these checks fail, the restore will be marked as unreconciled.

### Expiring and Deleting Backups

In order to prevent backups from growing without bound, we have configuration options to expire backup data older than a certain age. This will be done through the `fdbbackup expire` command. Because this command blocks, and requires non-trivial resources to run, this will be run in a separate pod from the main controller. The operator will maintain an in-memory queue of backup expiration jobs, with one job for each backup. On start-up, it will populate the queue, and on reconciliation it will ensure that the backup has an entry in the expiration queue.  The operator will have a limit for the concurrency of backup expiration pods, with a default of 1. The operator will have a dedicated goroutine for monitoring this queue and creating pods. If the number of active backup expiration pods is less than the concurrency limit, the queueing goroutine will pop the first job off the queue, create a pod for running that backup expiration, and put a new entry on the end of the queue. If the number of active backup expiration pods is greater than or equal to the concurrency limit, this goroutine will sleep for 10 minutes and try again. It will also check to make sure that there is only one backup expiration running for a given backup at once. Pods that have started termination will not count against this concurrency limit.

We will have a special case of backup expiration for deleting an entire backup. The operator will add finalizers to the backup resource to indicate that when the backup resource is deleted, we must run an `fdbbackup delete` command. There will be an option for turning off this finalizer in the backup spec. When the backup expiration job identifies that a backup needs to be deleted, it will run an `fdbbackup delete` command rather than an `fdbbackup expire` command, and when that completes it will clear the deletion finalizer. At this point the backup resource will get cleaned up.

The backup expiration job will have a maximum execution time of 1 hour. If the operator detects a pod that has been running for longer than this limit, it will delete the pod and start another one for the next backup in the queue.

If a backup resource has multiple low-level backups that have data, we will run the expiry on the last entry in the list. If that entry is restorable and has the minimum restorable data, any older backups will be fully deleted, and removed from the resource status. Otherwise, they will be left intact.

The retry interval when we hit the concurrency limit for expiration pods will be customizable in the start command for the operator.

### Coordinating Operations

If an FDB cluster is deployed across multiple Kubernetes clusters, users may want to create a backup resource in every Kubernetes cluster, rather than picking one to serve as a leader. To support this, we can use the locking system that the operator already uses when coordinating operations for the FoundationDBCluster resource. The following actions will require a lock in order to make changes:

* Toggle backup running
* Modify backup
* Toggle backup paused
* Toggle restore running

## Alternatives Considered

### Different Strategies for Backup Expiry

We considered using a CronJob for backup expiry. This would allow re-using more Kubernetes constructs for managing the job execution, and avoid the need for custom job queueing. However, it would raise more challenges around limiting the number of concurrent jobs. We do not want to start all of the expiration jobs at one time, because it would cause a large surge of resource usage. This surge would require either autoscaling capacity or keeping a large amount of capacity in reserve. We do not want to put that burden on users of the operator, so we would prefer to take that complexity on ourselves.

We considered having a single job that does backup expiry for all clusters. This would move the work for managing the queue of backup expirations from the main operator loop into this other job. This could be done in a dedicated job or in a separate loop in the operator itself. However, this would give less flexibility in distributing this work across different nodes, which would constrain the opportunities for parallelism.

## Related Links

* [FoundationDB Backup documentation](https://apple.github.io/foundationdb/backups.html)

