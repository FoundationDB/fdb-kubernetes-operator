# v0.5.0

*	Allow replacicng pods that are failing to launch.
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