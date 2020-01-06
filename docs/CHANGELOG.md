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