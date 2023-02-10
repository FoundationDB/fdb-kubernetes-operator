# v0.27.0

* Check if instance contains Pod before continuing operation.
* Add a timeout to the transactions for managing locks.
* Add an exec subcommand for the kubectl plugin.
* Move tracking of removal state into the process group status.
* Fix typo in spec.

# v0.26.0

*	Replace some hardcoded strings with named constants.
*	Add security contexts to the sample operator deployments.
*	Fix handling of exclusion output from whole-machine exclusions on FDB 6.3.
*	Fix a bug with calculating the storage servers per pod for a missing pod.
*	Fix handling of the default namespace in the kubectl plugin commands.

# v0.25.0

*	Add docs for StorageClass and multiple storage servers per pod.
*	Add kubectl plugin for fdb.
*	Add liveness probe to helm chart.
*	Allow customization of pod resources in help chart.
*	Add a design for automating replacements.
*	Pin kustomize version.
*	Add option to helm chart for "global mode".
*	Allow override of deployment replicas value.
*	Add unit tests for plugin.
*	Fix bug to update storageServerPerPod.
*	Add custom parameters to backup agents.
*	Enable golint to check for shadowing.

# v0.24.0

*	Use the locking system to coordinate exclusions and bounces.
*	Add a validation to prevent user-submitted tags for core images.
*	Add an option to use service IPs for the public IPs for FDB processes.
*	Use the zone and region configuration to infer whether locks are required by default.
*	Simplify RBAC configuration.
*	Take a lock when bouncing processes.
*	Allow customizing lock durations.
*	Add support for running with multiple storage servers per disk.
*	Use a distroless image as the base image for the operator.

# v0.23.1

*	Fix a bug that caused the operator to delete pods for the wrong cluster.

# v0.23.0

*	Use GetConnectionString method when choosing connection option to always
	get an up-to-date cluster file.
*	Use instance IDs as the identifier when removing pods and PVCs
*	Get the exclusion list directly when we want to determine exclusions.

# v0.22.0

*	Simplify the service reconciler.
*	Add a tool to check for usage of deprecated fields.

# v0.21.2

*	Fix the logic for checking the addresses in the exclusion results.
*	Add more logging to the exclusion and removal process.
*	Refactor the no-wait excludes to use a cleaner retry process.

# v0.21.1

*	Allow promoting the seed connection string to the official connection
	string when the official one is not working.

# v0.21.0

*	Adds initial support for gloangci-lint support.
*	Updates go version 1.15.2.

# v0.20.1

*	Fix the handling of TLS addresses when doing no-wait excludes.

# v0.20.0

*	Update the Go version in our build image.
*	Use a slim base image for the sidecar.
*	Enable non-blocking excludes in newer versions of FDB.
*	Add a mechanism for defining changes to defaults for future versions of the
	operator without enforcing them immediately.
*	Add a future default for the resource requirements for the sidecar
	container.

# v0.19.0

*	Correct helm chart for helm 3.
*	Use slim base image for sample container.
*	Rename the volumeClaim field for greater consistency with StatefulSet.
*	Add watch events for resources created by the operator.
*	Make use of atomic copy for sidecar files.

# v0.18.0

*	Add a check for container readiness before completing reconciliation.
*	Fix configuration and examples in getting started guide.
*	Prevent removing instances that do not have an IP address.
*	Do a clean build when running the PR checks.
*	Use the version from the spec as a fallback when we cannot get status using
	the running version from the status.
*	Add more detail about the replication strategy in our local test examples.

# v0.17.0

*	Adds non-blocking excludes code, but does not enable it.

# v0.16.0

*	Ignore the values for version flags in the live configuration when those
	flags are omitted in the cluster spec.
*	Add support for Fast Restore instances in FDB 6.3.
*	Only include the sidecar conf in the config map when there are pods that
	require it.
*	Add an option to disable the client compatibility check when upgrading.
*	Allow specifying custom labels on backup deployments.
*	Move the backup models into their own file.
*	Use an annotation to track which version of a config map a pod has seen.
*	Prevent completing reconciliation when a pod has a pending config map
	update.
*	Add an option for controlling CLI timeouts.
*	Replace any instances that have an instance ID with the wrong prefix.
*	Prevent bouncing a process when another process has been bounced recently.

# v0.15.2

*	Prevent errors when encountering pods in replace_misconfigured_pods that are
	not owned by the operator.

# v0.15.0

*	Remove step to set default values in the cluster spec.
*	Use the status call when updating the status in replace_misconfigured_pods.

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