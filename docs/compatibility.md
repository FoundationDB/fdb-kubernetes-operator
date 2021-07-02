# Version Compatibility

A new major release of the Kubernetes operator may break compatibility with the
last major version in order to address technical debt and keep the code base
manageable. The breaks may include:

* Dropping support for old versions of FDB
* Dropping support for old versions of Kubernetes
* Removing deprecated fields from the cluster resource
* Changing the default values when fields are omitted in the spec
* Removing flags and environment variables from the operator
* Removing behavior from the operator
* Removing deprecated metrics

This document will contain general information about how to manage major version
upgrades, as well as version-specific notes on things you may need to do during
the upgrade process.

## Supported Versions

This table details the major versions of the Kubernetes operator and which
versions of related services that each operator version is compatible with. The
"Most Recent Version" column shows the last version of the operator that was
published for each major version.

| Operator Version  | Most Recent Version | Supported Cluster Models  | Supported FDB Versions  | Supported Kubernetes Versions |
| ----------------- | ------------------- | ------------------------- | ----------------------- | ----------------------------- |
| 0.x               | 0.37.0              | v1beta1                   | 6.1.12+                 | 1.15.0+                       |

## Preparing for a Major Release

Before you upgrade to a new major version, you should first update the operator
and the CRD to the most recent release for the major version you are currently
running. You can find that release in the table above. This will ensure that you
can opt in to new behavior and move to the latest supported fields in the spec
in advance of the upgrade, through whatever process you need to update your
clusters safely.

At this point, you can use the `kubectl-fdb` plugin to check your cluster specs for deprecated
fields or defaults.
For more information see the [kubectl-fdb plugin Readme](../kubectl-fdb/Readme.md) and
the `deprecation` subcommand.
The plugin will report any deprecations and can also printout the new cluster spec:

```bash
$ kubectl fdb deprecation
Cluster sample-cluster has no deprecation
1 cluster(s) without deprecations
```
