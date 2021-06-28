# Managing Backups through the Operator

FoundationDB has out-of-the-box support for backing up to an S3-compatible object store, and the operator supports this through a special resource type for backup and restore. These backups run continuously, and simultaneously build new snapshots while backing up new mutations. You can restore to any point in time after the end of the first snapshot.

You can find more information about the backup feature in the [FoundationDB Backup documentation](https://apple.github.io/foundationdb/backups.html).

**Warning**: Support for backups in the operator is still in development, and there are significant missing features.

## Example Backup

This is a sample configuration for running a continuous backup of a cluster.

This sample assumes some configuration that you will need to fill in based on the details of your environment. Those assumptions are explained in comments below.

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBBackup
metadata:
  name: sample-cluster
spec:
  version: 6.2.30
  clusterName: sample-cluster
  accountName: account@object-store.example:443
  podTemplateSpec:
    spec:
      volumes:
        - name: backup-credentials
          secret:
            secretName: backup-credentials
        - name: fdb-certs
          secret:
            secretName: fdb-certs
      containers:
        - name: foundationdb
          env:
            - name: FDB_BLOB_CREDENTIALS
              value: /var/backup-credentials/credentials
            - name: FDB_TLS_CERTIFICATE_FILE
              value: /var/fdb-certs/cert.pem
            - name: FDB_TLS_CA_FILE
              value: /var/fdb-certs/ca.pem
            - name: FDB_TLS_KEY_FILE
              value: /var/fdb-certs/key.pem
          volumeMounts:
            - name: fdb-certs
              mountPath: /var/fdb-certs
            - name: backup-credentials
              mountPath: /var/backup-credentials
---
apiVersion: v1
kind: Secret
metadata:
  name: backup-credentials
type: Opaque
stringData:
  credentials: |
    {
        "accounts": {
            "account@object-store.example": {
                "secret" : "YOUR_ACCOUNT_KEY"
            }
        }
    }
---
apiVersion: v1
kind: Secret
metadata:
  name: fdb-certs
stringData:
  cert.pem: |
    # Put your certificate here.
  key.pem: |
    # Put your key here.
  ca.pem: |
    # Put your CA file here.
```

Creating this resource will tell the operator to do the following things:

1. Create a `sample-cluster-backup-agents` deployment running FoundationDB backup agent processes connecting to the cluster.
2. Run an `fdbbackup start` command to start a backup at `https://object-store.example:443/sample-cluster` using the bucket name `fdb-backups`.

## Using Secure Connections to the Object Store

By default, the operator assumes you want to use secure connections to your object store. In order to do this, you must provide a certificate, key, and CA file to the backup agents. The CA file must contain the root CA for your object store. The certificate and key must be parseable in order to initialize the TLS subsystem in the backup agents, but the agents will not use the certificate and key to communicate with the object store. You can configure the paths to these files through the environment variables `FDB_TLS_CERTIFICATE_FILE`, `FDB_TLS_KEY_FILE`, and `FDB_TLS_CA_FILE`. In the example above, we have all three of these defined in a secret called `fdb-certs`.

If you are configuring your cluster to use TLS for connections within the cluster, the backup agents will use the same certificate, key, and CA file for the connections to the cluster, so you must make sure the configuration is valid for this purpose as well.

## Configuring Your Account

Before you start a backup, you will need to configure an account in your object store. Depending on the implementation details of your object store, you may also need to configure a bucket in advance, but the FDB backup process will attempt to automatically create one. You can specify the bucket name in the `bucket` field of the backup spec. In the example above, we have an account called `account` at the object store `https://object-store.example`, and it has a bucket called `fdb-backups`.

You will need to expose the password or account key for the object store account through a credentials file. The format of the credentials file is defined in the FoundationDB backup documentation. You need to expose this credentials file to the backup agents, as shown in the example above. You can configure the path to the credentials file through the `FDB_BLOB_CREDENTIALS` environment variable.

## Configuring the Operator

The operator will run `fdbbackup` commands to manage the backup, so the operator needs to have access to the object store as well. You can configure that access the same way as you do for the backup agents, by defining the environment variables `FDB_BLOB_CREDENTIALS`, `FDB_TLS_CERTIFICATE_FILE`, `FDB_TLS_KEY_FILE`, and `FDB_TLS_CA_FILE`.

## Restoring a Backup

You can start a restore by creating a restore object. Here is an example restore, using the same account as the backup example above:

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBRestore
metadata:
  name: sample-cluster
spec:
  destinationClusterName: sample-cluster
  backupURL: "blobstore://account@object-store.example:443/sample-cluster?bucket=fdb-backups"
```

This will tell the operator to run an `fdbrestore` command targeting the cluster `sample-cluster`. The cluster must be empty before this command can be run. This will restore to the last restorable point in the backup you are using, and will restore the entire keyspace.

You can track the progress of the restore through the `fdbrestore status` command. The destination cluster will be locked until the restore completes.

## Next

You can continue on to the [next section](technical_design.md) or go back to the [table of contents](index.md).
