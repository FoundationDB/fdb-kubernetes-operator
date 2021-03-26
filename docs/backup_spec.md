<br>
# API Docs
This Document documents the types introduced by the FoundationDB Operator to be consumed by users.
> Note this document is generated from code comments. When contributing a change to this document please do so by changing the code comments.

## Table of Contents
* [BackupGenerationStatus](#backupgenerationstatus)
* [FoundationDBBackup](#foundationdbbackup)
* [FoundationDBBackupList](#foundationdbbackuplist)
* [FoundationDBBackupSpec](#foundationdbbackupspec)
* [FoundationDBBackupStatus](#foundationdbbackupstatus)
* [FoundationDBBackupStatusBackupDetails](#foundationdbbackupstatusbackupdetails)
* [FoundationDBLiveBackupStatus](#foundationdblivebackupstatus)
* [FoundationDBLiveBackupStatusState](#foundationdblivebackupstatusstate)

## BackupGenerationStatus

BackupGenerationStatus stores information on which generations have reached different stages in reconciliation for the backup.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| reconciled | Reconciled provides the last generation that was fully reconciled. | int64 | false |
| needsBackupAgentUpdate | NeedsBackupAgentUpdate provides the last generation that could not complete reconciliation because the backup agent deployment needs to be updated. | int64 | false |
| needsBackupStart | NeedsBackupStart provides the last generation that could not complete reconciliation because we need to start a backup. | int64 | false |
| needsBackupStop | NeedsBackupStart provides the last generation that could not complete reconciliation because we need to stop a backup. | int64 | false |
| needsBackupPauseToggle | NeedsBackupPauseToggle provides the last generation that needs to have a backup paused or resumed. | int64 | false |
| needsBackupModification | NeedsBackupReconfiguration provides the last generation that could not complete reconciliation because we need to modify backup parameters. | int64 | false |

[Back to TOC](#table-of-contents)

## FoundationDBBackup

FoundationDBBackup is the Schema for the FoundationDB Backup API

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#objectmeta-v1-meta) | false |
| spec |  | [FoundationDBBackupSpec](#foundationdbbackupspec) | false |
| status |  | [FoundationDBBackupStatus](#foundationdbbackupstatus) | false |

[Back to TOC](#table-of-contents)

## FoundationDBBackupList

FoundationDBBackupList contains a list of FoundationDBBackup

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#listmeta-v1-meta) | false |
| items |  | [][FoundationDBBackup](#foundationdbbackup) | true |

[Back to TOC](#table-of-contents)

## FoundationDBBackupSpec

FoundationDBBackupSpec describes the desired state of the backup for a cluster.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| version | The version of FoundationDB that the backup agents should run. | string | true |
| clusterName | The cluster this backup is for. | string | true |
| backupState | The desired state of the backup. The default is Running. | string | false |
| backupName | The name for the backup. The default is to use the name from the backup metadata. | string | false |
| accountName | The account name to use with the backup destination. | string | true |
| bucket | The backup bucket to write to. The default is to use \"fdb-backups\". | string | false |
| agentCount | AgentCount defines the number of backup agents to run. The default is run 2 agents. | *int | false |
| snapshotPeriodSeconds | The time window between new snapshots. This is measured in seconds. The default is 864,000, or 10 days. | *int | false |
| backupDeploymentMetadata | BackupDeploymentMetadata allows customizing labels and annotations on the deployment for the backup agents. | *[metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#objectmeta-v1-meta) | false |
| podTemplateSpec | PodTemplateSpec allows customizing the pod template for the backup agents. | *[corev1.PodTemplateSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#podtemplatespec-v1-core) | false |
| customParameters | CustomParameters defines additional parameters to pass to the backup agents. | []string | false |
| allowTagOverride | This setting defines if a user provided image can have it's own tag rather than getting the provided version appended. You have to ensure that the specified version in the Spec is compatible with the given version in your custom image. | *bool | false |

[Back to TOC](#table-of-contents)

## FoundationDBBackupStatus

FoundationDBBackupStatus describes the current status of the backup for a cluster.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| agentCount | AgentCount provides the number of agents that are up-to-date, ready, and not terminated. | int | false |
| deploymentConfigured | DeploymentConfigured indicates whether the deployment is correctly configured. | bool | false |
| backupDetails | BackupDetails provides information about the state of the backup in the cluster. | *[FoundationDBBackupStatusBackupDetails](#foundationdbbackupstatusbackupdetails) | false |
| generations | Generations provides information about the latest generation to be reconciled, or to reach other stages in reconciliation. | [BackupGenerationStatus](#backupgenerationstatus) | false |

[Back to TOC](#table-of-contents)

## FoundationDBBackupStatusBackupDetails

FoundationDBBackupStatusBackupDetails provides information about the state of the backup in the cluster.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| url |  | string | false |
| running |  | bool | false |
| paused |  | bool | false |
| snapshotTime |  | int | false |

[Back to TOC](#table-of-contents)

## FoundationDBLiveBackupStatus

FoundationDBLiveBackupStatus describes the live status of the backup for a cluster, as provided by the backup status command.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| DestinationURL | DestinationURL provides the URL that the backup is being written to. | string | false |
| SnapshotIntervalSeconds | SnapshotIntervalSeconds provides the interval of the snapshots. | int | false |
| Status | Status provides the current state of the backup. | [FoundationDBLiveBackupStatusState](#foundationdblivebackupstatusstate) | false |
| BackupAgentsPaused | BackupAgentsPaused describes whether the backup agents are paused. | bool | false |

[Back to TOC](#table-of-contents)

## FoundationDBLiveBackupStatusState

FoundationDBLiveBackupStatusState provides the state of a backup in the backup status.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| Running | Running determines whether the backup is currently running. | bool | false |

[Back to TOC](#table-of-contents)
