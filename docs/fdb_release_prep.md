# Preparing for New Releases of FoundationDB

When a new major or minor release of FoundationDB is coming up, we should take steps to ensure that the operator supports that release. This document captures some common things to look for.

1. Create a label for tracking issues related to the new release.
1. Look for new entries in the `ProcessClass.ClassType` enum in FoundationDB that are not in the `ProcessCounts` struct in the operator. File issues on making those available in the `ProcessCounts`. You can find this in `fdbrpc/Locality.h`.
1. Look for new roles that are not in the `RoleCounts` struct in the operator. File issues on making those available in the `RoleCounts` and incorporating them into the default process counts. Some roles such as the `ratekeeper` are singletons, which means that they do not appear in `RoleCounts`, but are accounted for in the `GetProcessCountsWithDefaults` method. You can look for occurrences of `roles.addRole` in the FoundationDB code to get a quick list of the roles. Note that the role names in this search are going to be singular, but they are always plural in the `RoleCounts`. You can find this in `fdbserver/Status.actor.cpp`.
1. Look for new entries in the database configuration in FoundationDB that are not in the `DatabaseConfiguration` struct in the operator. File issues on making those available in the `DatabaseConfiguration`. If the new entry is coming from a new role, it will be added to the `DatabaseConfiguration` when you add it to `RoleCounts`.
1. Look for any new features in FoundationDB that require new configuration to enable or to use, and file issues on supporting those features.

Once the new version has a full public release, we should update the operator config to support the new release out of the box:

1. Update the manager config to load binaries from the new release. You can do this by adding another init container in our YAML config. You can find existing init containers by looking for container names like `foundationdb-kubernetes-init-X-Y` and use those as a reference.
1. Modify the base test config to use the new version, and create a cluster with that test config.
