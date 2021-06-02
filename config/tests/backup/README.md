This directory contains configuration for testing clusters with backups in a
local development environment.

The backup resource will bring up backup agents and start a backup. Once it
is reconciled, you can run backup commands from the backup agent containers:

```bash
# Check status
kubectl exec deployment/test-cluster-backup-agents -- fdbbackup status

This example uses configuration for a local MinIO instance, which is set up as
part of the local testing environment for the operator. This instance has
certificates stored in Kubernetes secrets, and credentials that are hardcoded
in the YAML. This configuration is for testing purposes only. You will need to
determine the appropriate way of managing certificates and credentials for
your real environment and endpoints, as well as the appropriate backup
solution to use. We use MinIO in our local tests due to its lightweight setup,
but you can backup to any S3-compatible object storage service.

If you are testing this in Docker Desktop, you can browse the local MinIO
instance at https://localhost:9000. Note: This will use a certificate that
is not in your browser's trust store, so you will get a security warning.

If you want to test a restore, you can take the following steps:

1. Apply the base configuration: `kubectl apply -k config/tests/backup/base`.
2. Wait for all resources to be reconciled.
3. Set a test key.
4. Confirm through `fdbbackup status` that the backup is up-to-date. You can
   do this by checking the current time and then waiting for the "Last
   complete log version and timestamp" to be after that time.
5. Apply the configuration for stopping the backup: `kubectl apply -k config/tests/backup/stopped`.
6. Wait for all resources to be reconciled.
7. Confirm through `fdbbackup status` that the backup has been stopped.
9. Open a CLI and run `writemode on; clearrange '' \xff`.
9. Confirm in the CLI that the test key is cleared.
10. Apply the configuration for starting a restore: `kubectl apply -k config/tests/backup/restore`.
11. Wait for all resources to be reconciled.
12. Confirm through `fdbrestore status` that the restore has `State: completed`.
13. Open a CLI and check the test key.

Once that is done, you can clean up the backup by running:

```bash
kubectl exec deployment/test-cluster-backup-agents -- fdbbackup delete -d "blobstore://minio@minio-service:9000/test-cluster?bucket=fdb-backups"
