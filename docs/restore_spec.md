<br>
# API Docs
This Document documents the types introduced by the FoundationDB Operator to be consumed by users.
> Note this document is generated from code comments. When contributing a change to this document please do so by changing the code comments.

## Table of Contents
* [FoundationDBKeyRange](#foundationdbkeyrange)
* [FoundationDBRestore](#foundationdbrestore)
* [FoundationDBRestoreList](#foundationdbrestorelist)
* [FoundationDBRestoreSpec](#foundationdbrestorespec)
* [FoundationDBRestoreStatus](#foundationdbrestorestatus)

## FoundationDBKeyRange

FoundationDBKeyRange describes a range of keys for a command.  The keys in the key range must match the following pattern: `^[A-Za-z0-9\/\\-]+$`. All other characters can be escaped with `\xBB`, where `BB` is the hexadecimal value of the byte.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| start | Start provides the beginning of the key range. | string | true |
| end | End provides the end of the key range. | string | true |

[Back to TOC](#table-of-contents)

## FoundationDBRestore

FoundationDBRestore is the Schema for the FoundationDB Restore API

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#objectmeta-v1-meta) | false |
| spec |  | [FoundationDBRestoreSpec](#foundationdbrestorespec) | false |
| status |  | [FoundationDBRestoreStatus](#foundationdbrestorestatus) | false |

[Back to TOC](#table-of-contents)

## FoundationDBRestoreList

FoundationDBRestoreList contains a list of FoundationDBRestore objects.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#listmeta-v1-meta) | false |
| items |  | [][FoundationDBRestore](#foundationdbrestore) | true |

[Back to TOC](#table-of-contents)

## FoundationDBRestoreSpec

FoundationDBRestoreSpec describes the desired state of the backup for a cluster.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| destinationClusterName | DestinationClusterName provides the name of the cluster that the data is being restored into. | string | true |
| backupURL | BackupURL provides the URL for the backup. | string | true |
| keyRanges | The key ranges to restore. | [][FoundationDBKeyRange](#foundationdbkeyrange) | false |

[Back to TOC](#table-of-contents)

## FoundationDBRestoreStatus

FoundationDBRestoreStatus describes the current status of the restore for a cluster.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| running | Running describes whether the restore is currently running. | bool | false |

[Back to TOC](#table-of-contents)
