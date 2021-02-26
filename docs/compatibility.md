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
| 0.x               | 0.29.0              | v1beta1                   | 6.1.12+                 | 1.15.0+                       |

## Preparing for a Major Release

Before you upgrade to a new major version, you should first update the operator
and the CRD to the most recent release for the major version you are currently
running. You can find that release in the table above. This will ensure that you
can opt in to new behavior and move to the latest supported fields in the spec
in advance of the upgrade, through whatever process you need to update your
clusters safely.

At this point, you can run a job to check your cluster specs for deprecated
fields or defaults. 

	--- 
	apiVersion: batch/v1
	kind: Job
	metadata:
	  name: fdb-deprecation-check
	spec:
	  template:
	    spec:
	      containers:
	        - image: foundationdb/fdb-kubernetes-operator:$version
	          name: fdb-deprecation-check
	          args:
	            - --check-deprecations
	      restartPolicy: Never

Make sure to fill in the `$version` placeholder with the version of the operator
that you are running in your cluster.

This will print out new cluster specs that do not use deprecated fields, and
explicitly specify values for fields that have defaults that will change in the
next release. You can use this to update the YAML files in your source of truth,
or modify your tooling to use the supported fields. This job will not make any
modifications to the stored specs, or to anything else. You can access the
output from the job by running `kubectl logs job/fdb-deprecation-check`. You
may also need to check the job list to make sure that the job ran successfully.
Once you've captured this output, you can delete the job.

If there are no clusters that require changes for the next release, this job
will print out "No deprecated specs found.".

Note that the specs that this job outputs may include some empty fields that
are omitted from your spec, such as `automationOptions: {}`. This is merely an
artifact of the serialization; you can leave them out of your specs. As long as
there are no meaningful changes to a cluster spec, the cluster will not be
included in the job's output.

Before running this job, you will need to fill in any secrets, volumes, or other
configuration that you apply to your normal controller, to make sure that it
can access clusters in the same way that the controller itself does.

By default, the deprecation check will fill in defaults to match the current
behavior, so you can update the specs without having any impact on your
clusters. If you want to opt in to the new defaults, you can pass the argument
`--use-future-defaults`, and it will print out specs that use the new defaults
instead.