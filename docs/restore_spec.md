<br>
# API Docs
This Document documents the types introduced by the FoundationDB Operator to be consumed by users.
> Note this document is generated from code comments. When contributing a change to this document please do so by changing the code comments.

## Table of Contents
* [FoundationDBRestore](#foundationdbrestore)
* [FoundationDBRestoreList](#foundationdbrestorelist)
* [FoundationDBRestoreSpec](#foundationdbrestorespec)
* [FoundationDBRestoreStatus](#foundationdbrestorestatus)

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

[Back to TOC](#table-of-contents)

## FoundationDBRestoreStatus

FoundationDBRestoreStatus describes the current status of the restore for a cluster.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| running | Running describes whether the restore is currently running. | bool | false |

[Back to TOC](#table-of-contents)
