# v0.15.0

*	Remove step to set default values in the cluster spec.
*	Use the status call when updating teh status in replace_misconfigured_pods.

# v0.14.0

*	Avoid trying to make configuration changes when the database is unavailable.
*	Move the pendingRemovals field to the cluster status.
*	Mark the cluster as fully reconciled when we are waiting for pods to be
	deleted.
*	Add an option to configure a headless service to generate DNS records for
	pods.
*	Ensure fault tolerance when recruiting coordinators.

# v0.13.0

*	Sync coordinator changes across KCs.
*	Only check client compatibility when we are doing a protocal-incompatible upgrade.
*	Prevent reconfiguration an already configured database.
*	Adds a cache for the connections from the lock client.

# v0.12.0

*	Improve logging for the operator
*	Improve logging for fdbmonitor
*	Allow customizing pod configuration for different processes based on process class.

# v0.11.1

*	Restore the behavior of writing the connection string into the spec as a temporary bridge.

# v0.11.0

*	Move fields from spec to status.
*	Bump FDB version used in examples.
*	Initial support for restore.

# v0.10.0

*	Adds an option for the backup snapshot time.
*	Add print columns to the backup resource.
*	Remove tests for older versions of FDB.
*	Drop support for older instance ID labels.

# v0.9.0

*	Adds ability to resize volumes.
*	Adds an option for pausing backups.
*	Adds an option for stopping backups.
*	Documents new migration behavior.
*	Remove unused code.
*	Omits TLS CA file when no trusted CA's are given.
*	Adds an option to crash the sidecar when the cluster file is empty.
*	Adds an option to configure version flags.
*	Drops support for old sidecar environment variables in newer versions of FDB.

# v0.8.0

*	Set the WATCH_NAMESPACE field in more example deployments.
*	Avoid overwriting existing annotations that are set outside the operator.
*	Remove unnecessary binaries from the docker image.
*	Prevent unnecesarily rebuilding packages in the Makefile.
*	Skip over instance IDs in the instancesToRemove list when adding new instances.
*	Reduce the default log router count.
*	Allow starting backups through the operator.
*	Use a read-only root file system for the FDB containers.

# v0.7.0

*   Update documentation.
*   Use a pointer for the backup account count to reduce confusion around the zero-value.
*   Add instructions on installing the backup CRD to the manuals.
*   Add a new CRD for managing backups.
*   Add tracking of the backup agent deployment in the cluster status.
*   Add an option in the cluster spec for configuring backup agents.
*   Improve error message when TLS paths not set.

# v0.6.0

*	Add a Helm chart for the operator.
*	Emit Prometheus metrics on reconciliation and cluster health.
*	Fix the name of the volume claim customization in the sample clusters.
*	Fix the logic for checking whether pods are up-to-date when using a custom
	pod lifecycle manager.
*	Fix false positives when checking for processes being up-to-date during
	upgrades.

# v0.5.0

*	Allow replacing pods that are failing to launch.
*	move the removal of pods in a shrink to the end of reconciliation.
*	Drop support for FDB versions < 6.1.12.
*	Allow customizing the name of the config map.
*	Allow customizing the name of the volume claim name.
*	Block cluster downgrades.
*	Add an example of starting a client app connected to the cluster.
*	Remove support for the HAS_STATUS_SUBRESOURCE flag.
*	Add a default user to the images for the operator and the Kubernetes sidecar.
*	Auto-generate API docs
*	Refactor reconciliation to be based entirely on the spec and status of the cluster.
*	Add a list of instances to remove as an alternative to the pendingRemovals map.
*	Adds a shortname for the CRD.
*	Improve logging when we convert a retryable error into a requeue.
*	Fix golint issues.
*	Set up a local MinIO instance for testing backups.
*	Add a cluster controller process to some test cases.
*	Add items to the config map in the cluster spec.
*	Remove the CRD from the sample deployment because it is too large for kubectl apply.
*	Sync coordinator changes when running across Kubernetes clusters.
*	Update our Kubernetes client dependencies.

# v0.4.0

*	Automatically reload certs in the Kubernetes sidecar when they are updated.
*	Upgrade to Kubebuilder 2.
*	Add a field to control the locality_data_hall parameter.
*	Enable new features in the sidecar in FDB 6.2.15 rather than waiting for
	7.0.0.
*	Add additional stateless processes starting in FDB 6.2.0.

# v0.3.0

*	Fix 'user cannot patch resource "events" in API group'.
*	Add documentation on how to access a cluster.
*	Allow enabling and disabling TLS.
*	Break configuration changes into multiple steps when changing region config.
*	Change the way we generate instance IDs to remove the need to track the next 
	instance ID in the spec.
*	Improve customization of the resources created by the operator.
*	Add FDB_INSTANCE_ID as an environment variable that is enabled by default in 
	the sidecar substitutions.
*	Replace sidecar environment variables with command-line flags.
*	Using server from the FoundationDB image when it matches the sidecar version.


# v0.2.0

*	Check that clients are compatible with new versions of FDB before upgrading.
*	Remove comments when parsing the connection string after changing
	coordinators.
*	Use a hash of the pod spec instead of the full pod spec to determine when we
	need to recreate pods.
*	Incorporate satellite logs into the default log counts when the operator is
	running in a satellite DC.
*	Add additional coordinators when the database is using multiple regions.
*	Bring up new pods at the old FDB version when upgrading and expanding in a
	single generation.

# v0.1.0

*	Initial release.