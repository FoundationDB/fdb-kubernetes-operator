# Table of Contents

1.	[Introduction](#introduction)
2.	[Creating a Cluster](#creating-a-cluster)
3.	[Managing Process Counts](#managing-process-counts)
4.	[Growing a Cluster](#growing-a-cluster)
5.	[Shrinking a Cluster](#shrinking-a-cluster)
6.	[Replacing a Process](#replacing-a-process)
7.	[Changing Database Configuration](#changing-database-configuration)
8.	[Adding a Knob](#adding-a-knob)
9.	[Upgrading a Cluster](#upgrading-a-cluster)
10.	[Customizing Your Pods](#customizing-a-container)
11. [Controlling Fault Domains](#controlling-fault-domains)

# Introduction

This document provides practical examples of how to use the FoundationDB Kubernetes Operator to accomplish common tasks, and additional information and areas to consider when you are managing these tasks in a real environment.

This document assumes that you are generally familiar with Kuberneters. For more information on that area, see the [Kubernetes documentation](https://kubernetes.io/docs/home/).

The core of the operator is a reconciliation loop. In this loop, the operator reads the latest cluster spec, compares it to the running state of the cluster, and carries out whatever tasks need to be done to make the running state of the cluster match the desired state as expressed in the cluster spec. If the operator cannot fully reconcile the cluster in a single pass, it will try the reconciliation again. This can occur for a number of reasons: operations that are disabled, operations that require asynchronous work, error conditions, and so on. 

When you make a change to the cluster spec, it will increment the `generation` field in the cluster metadata. Once reconciliation completes, the `generations.reconciled` field in the cluster status will be updated to reflect the last generation that we have reconciled. You can compare these two fields to determine whether your changes have been fully applied. You can also see the current generation and reconciled generation in the output of `kubectl get foundationdbcluster`.

To run the operator in your environment, you need to install the controller and
the CRD:

	kubectl apply -f https://raw.githubusercontent.com/foundationdb/fdb-kubernetes-operator/master/config/samples/deployment.yaml

You can see logs from the operator by running
`kubectl logs fdb-kubernetes-operator-controller-manager-0 --container=manager`. You will likely want to watch these logs as you make changes to get a better understanding of what the operator is doing.

The example below will cover creating a cluster. All subsequent examples will assume that you have
just created this cluster, and will cover an operation on this cluster.

For more information on the fields you can define on the cluster resource, see
the [go docs](https://godoc.org/github.com/FoundationDB/fdb-kubernetes-operator/pkg/apis/apps/v1beta1#FoundationDBCluster).

# Creating a Cluster

To start with, we are going to be creating a cluster with the following configuration:

	apiVersion: apps.foundationdb.org/v1beta1
	kind: FoundationDBCluster
	metadata:
		name: sample-cluster
	spec:
		version: 6.2.10
		databaseConfiguration:
			redundancy_mode: double
			storage: 5
		volumeSize: "128G"
		resources:
			requests:
				cpu: 2
				memory: 8Gi

This will create a cluster with 5 storage processes, 4 log processes, and 7 stateless processes. Each fdbserver process will be in a separate pod, and the pods will have names of the form `sample-cluster-$n`, where `$n` is the instance ID for the process.

You can run `kubectl get foundationdbcluster sample-cluster` to check the progress of reconciliation. Once the reconciled generation appears in this output, the cluster should be up and ready. After creating the cluster, you can 
You can connect to the cluster by running `kubectl exec -it sample-cluster-1 fdbcli`.

This example requires non-trivial resources, based on what a process will need in a production environment. This means that is too large to run in a local testing environment. It also requires disk I/O features that are not present in Docker for Mac. If you want to run these tests in that kind of environment, you can try bringing in the resource requirements, knobs, and fault domain information from a [local testing example](../config/samples/cluster_local.yaml).

In addition to the pods, the operator will create a Persistent Volume Claim for any stateful
processes in the cluster. In this example, each volume will be 128 GB. 

By default each pod will have two containers and one init container.. The `foundationdb` container will run fdbmonitor and fdbserver, and is the main container for the pod. The `foundationdb-kubernetes-sidecar` container will run a sidecar image designed to help run FDB on Kubernetes. It is responsible for managing the fdbmonitor conf files and providing FDB binaries to the `foundationdb` container. The operator will create a config map that contains a template for the monitor conf file, and the sidecar will interpolate instance-specific fields into the conf and make it available to the fdbmonitor process through a shared volume. The "Upgrading a Cluster" has more detail on we manage binaries.

# Managing Process Counts

You can manage process counts in either the database configuration or in the process counts in the cluster spec. In most of these examples, we will only manage process counts through the database configuration. This is simpler, and it ensures that the number of processes we launch fits the number of processes that we are telling the database to recruit.

To explicitly set process counts, you could configure the cluster as follows:

	apiVersion: apps.foundationdb.org/v1beta1
	kind: FoundationDBCluster
	metadata:
		name: sample-cluster
	spec:
		version: 6.2.10
		databaseConfiguration:
			redundancy_mode: double
		processCounts:
			storage: 6
			log: 5
			stateless: 4
		volumeSize: "128G"
		resources:
			requests:
				cpu: 2
				memory: 8Gi


This will configure 6 storage processes, 5 log processes, and 4 stateless processes. This is fewer stateless processes that we had by default, which means that some processes will be running multiple roles. This is generally something you want to avoid in a production configuration, as it can lead to high activity on one role starving another role of resources.

By default, the operator will provision processes with the following process types and counts:

1. `storage`. Equal to the storage count in the database configuration. If no storage count is provided, this will be `2*F+1`, where `F` is the desired fault tolerance. For a double replicated cluster, the desired fault tolerance is 1.
2. `log`. Equal to the `F+max(logs, remote_logs)`. The `logs` and `remote_logs` here are the counts specified in the database configuration. By default, `logs` is set to 3 and `remote_logs` is set to either `-1` or `logs`.
3. `stateless`. Equal to the sum of all other roles in the database configuration + `F`. Currently, this is `max(proxies+resolvers+2, log_routers)`. The `2` is for the master and cluster controller, and may change in the future as we add more roles to the database. By default, `proxies` is set to 3, `resolvers` is set to 1, and `log_routers` is set to -1.

You can also set a process count to -1 to tell the operator not to provision any processes of that type.

# Growing a Cluster

Instead of setting the counts directly, let's update the counts of recruited roles in the database configuration:

	apiVersion: apps.foundationdb.org/v1beta1
	kind: FoundationDBCluster
	metadata:
		name: sample-cluster
	spec:
		version: 6.2.10
		databaseConfiguration:
			redundancy_mode: double
			storage: 6
			logs: 4 # default is 3
			proxies: 5 # default is 3
			resolvers: 2 # default is 1
		volumeSize: "128G"
		resources:
			requests:
				cpu: 2
				memory: 8Gi

This will provision 1 additional log process and 3 additional stateless processes. After launching those processes, it will change the database configuration to recruit 1 additional log, 2 additional proxies, and 1 additional resolver.

# Shrinking a Cluster

You can shrink a cluster by changing the database configuration or process count, just like when we grew a cluster:

	apiVersion: apps.foundationdb.org/v1beta1
	kind: FoundationDBCluster
	metadata:
		name: sample-cluster
	spec:
		version: 6.2.10
		databaseConfiguration:
			redundancy_mode: double
			storage: 4
		volumeSize: "128G"
		resources:
			requests:
				cpu: 2
				memory: 8Gi

The operator will determine which processes to remove and store them in the `pendingRemovals` field in the cluster spec to make sure the choice of removal stays consistent across repeated runs of the reconciliation loop. Once the processes are in the `pendingRemovals` list, we will exclude them from the database, which moves all of the roles and data off of the process. Once the exclusion is complete, it is safe to remove the processes, and the operator will delete both the pods and the PVCs. Once the processes are shut down, the operator will re-include them to make sure the exclusion state doesn't get cluttered. It will also remove the process from the `pendingRemovals` list.

The exclusion can take a long time, and any changes that happen later in the reconciliation process will be blocked until the exclusion completes.

If one of the removed processes is a coordinator, the operator will recruit a new set of coordinators before shutting down the process.

Any changes to the database configuration will happen before we exclude any processes. 

# Replacing a Process

If you delete a pod, the operator will automatically create a new pod to replace it. If there is a volume available for re-use, we will create a new pod to match that volume. This means that in general you can replace a bad process just by deleting the pod. This may not be desirable in all situations, as it creates a loss of fault tolerance until the replacement pod is created.

As an alternative, you can replace a pod by explicitly placing it in the pending removals list:

	apiVersion: apps.foundationdb.org/v1beta1
	kind: FoundationDBCluster
	metadata:
		name: sample-cluster
	spec:
		version: 6.2.10
		databaseConfiguration:
			redundancy_mode: double
			storage: 5
		volumeSize: "128G"
		pendingRemovals:
			sample-cluster-1: ""
		resources:
			requests:
				cpu: 2
				memory: 8Gi

When comparing the desired process count with the current pod count, any pods that are in the pending removal list are not counted. This means that the operator will only consider there to be 4 running storage pods, rather than 5, and will create a new one to fill the gap. Once this is done, it will go through the same removal process described above under "Shrinking a Cluster". The cluster will remain at full fault tolerance throughout the reconciliation. This allows you to replace an arbitrarily large number of processes in a cluster without any risk of availability loss.

# Changing Database Configuration

You can reconfigure the database by changing the fields in the database configuration:

	apiVersion: apps.foundationdb.org/v1beta1
	kind: FoundationDBCluster
	metadata:
		name: sample-cluster
	spec:
		version: 6.2.10
		databaseConfiguration:
			redundancy_mode: triple
			storage: 5
		volumeSize: "128G"
		resources:
			requests:
				cpu: 2
				memory: 8Gi

This will run the configuration command on the database, and may also add or remove processes to match the new configuration.

# Adding a Knob

To add a knob, you can change the customParameters in the cluster spec:

	apiVersion: apps.foundationdb.org/v1beta1
	kind: FoundationDBCluster
	metadata:
		name: sample-cluster
	spec:
		version: 6.2.10
		databaseConfiguration:
			redundancy_mode: double
			storage: 5
		customParameters:
			- "knob_always_causal_read_risky=1"
		volumeSize: "128G"
		resources:
			requests:
				cpu: 2
				memory: 8Gi

The operator will update the monitor conf to contain the new knob, and will then bounce all of the fdbserver processes. As soon as fdbmonitor detects that the fdbserver process has died, it will create a new fdbserver process with the latest config. The cluster should be fully available within 10 seconds of executing the bounce, though this can vary based on cluster size.

The process for updating the monitor conf can take several minutes, based on the time it takes Kubernetes to update the config map in the pods.

# Upgrading a Cluster

To upgrade a cluster, you can change the version in the cluster spec:

	apiVersion: apps.foundationdb.org/v1beta1
	kind: FoundationDBCluster
	metadata:
		name: sample-cluster
	spec:
		version: 6.2.11
		databaseConfiguration:
			redundancy_mode: double
			storage: 5
		volumeSize: "128G"
		resources:
			requests:
				cpu: 2
				memory: 8Gi

This will first update the sidecar image in the pod to match the new version, which will restart that container. On restart, it will copy the new FDB binaries into the config volume for the foundationdb container, which will make it available to run. We will then update the fdbmonitor conf to point to the new binaries and bounce all of the fdbserver processes.

Once all of the processes are running at the new version, we will recreate all of the pods so that the `foundationdb` container uses the new version for its own image. This will be done in a rolling bounce, where at most one fault domain is bounced at a time. While a pod is being recreated, it is unavailable, so this will degrade the availability fault tolerance for the cluster. The operator will ensure that pods are not deleted unless the cluster is at full fault tolerance, so if all goes well this will not create an availability loss for clients.

Deleting a pod may cause it to come back with a different IP address. If the process was serving as a coordinator, the coordinator will not be considered unavailable when it comes back up. The operator will detect this condition after creating the new pod, and will change the coordinators automatically to ensure that we regain fault tolerance.

# Customizing Your Pods

There are many fields in the cluster spec that allow configuring your pods. You can define custom environment variables, add your own containers, add additional volumes, and more. You may want to use these fields to handle things that are specific to your environment, like managing certificates or forwarding logs to a central system.

In this example, we are going to add a volume that mounts certificates from a Kubernetes secret. This is likely not how you would want to manage certificates in a real environment, but it can be helpful as an example.

Before you apply this change, you will need to create a secret. You can apply the following YAML to your environment to set up a secret with the self-signed cert that we use for testing the operator:

	apiVersion: v1
	kind: Secret
	metadata:
		name: fdb-kubernetes-operator-secrets
	data:
		cert.pem: |
			LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUZ4RENDQTZ3Q0NRQ29Bd2FySFhWeWtEQU5CZ2txaGtpRzl3MEJBUXNGQURDQm96RUxNQWtHQTFVRUJoTUMKVlZNeEN6QUpCZ05WQkFnTUFrTkJNUkl3RUFZRFZRUUhEQWxEZFhCbGNuUnBibTh4RlRBVEJnTlZCQW9NREVadgpkVzVrWVhScGIyNUVRakVWTUJNR0ExVUVDd3dNUm05MWJtUmhkR2x2YmtSQ01Sa3dGd1lEVlFRRERCQm1iM1Z1ClpHRjBhVzl1WkdJdWIzSm5NU293S0FZSktvWklodmNOQVFrQkZodGtiMjV2ZEhKbGNHeDVRR1p2ZFc1a1lYUnAKYjI1a1lpNXZjbWN3SGhjTk1Ua3dOak13TWpNME9URTFXaGNOTWpBd05qSTVNak0wT1RFMVdqQ0JvekVMTUFrRwpBMVVFQmhNQ1ZWTXhDekFKQmdOVkJBZ01Ba05CTVJJd0VBWURWUVFIREFsRGRYQmxjblJwYm04eEZUQVRCZ05WCkJBb01ERVp2ZFc1a1lYUnBiMjVFUWpFVk1CTUdBMVVFQ3d3TVJtOTFibVJoZEdsdmJrUkNNUmt3RndZRFZRUUQKREJCbWIzVnVaR0YwYVc5dVpHSXViM0puTVNvd0tBWUpLb1pJaHZjTkFRa0JGaHRrYjI1dmRISmxjR3g1UUdadgpkVzVrWVhScGIyNWtZaTV2Y21jd2dnSWlNQTBHQ1NxR1NJYjNEUUVCQVFVQUE0SUNEd0F3Z2dJS0FvSUNBUUNmCjRrNDRUTEhvQ2hYQmFCVHVhTWs2WURHQTNyN1VJR1BhOVJWeCsxVFFjNUZnL2Z6VFBtOG80dml5eDBwK0diUUcKWC9TM0hqcTJrSGh4NUJLV3A3NEVFWGE1ODJBTTRKU3hRV1kwVzVVVlV3QWR5dkxXZEY1OGRSOTJGMDQwWVJyZwpPOTFGaW5hRFZndUVUN1NVZlN6eGpnUUc4RXIyZmRTVzJLMy9CZDJBT2FwaVFGbzRWYlBPL3ZqMlhxNU5iSk05CmhtZ254aWZhZ2c0UjRPc25SbzN5YUpjNE5DRDRBamFLQ29kVFR1RmV2cGx3RDJ6QnBrdlo4TEpGUkZscDUzVWYKYjZBZGNQK2VibHU0b3ZoWWFpUWVUZ2htRzRvSy9KVFRwYWgwbHlHbUVmcXJrYTVrblBLWlJoQnBSWVZ1VVJUbwpoVEdvMGVGNzBscGExRjVRZWoza1BzWDBta0JpbmNmbkY0di9YdE9pNmNZK3Q3eG5ldzhQMGlNemw5MWNuNXpHCm9CRjNjamx5amVlNnh6WWxxbUhqbGg5OUs2OEZkQzM3TTl2R29LcGJNT3lNZ1AvQTJsdjh2UGw3eG8xQlBQUnIKZlpOWWpta3UwY212UmszbDVWbDFONzh3N3VrMCtqeHF3RXVFeVN3cVJxdHJ6TlRSbWdUMWxReTJlNzVVSlVxUQoxSDlnMk1CajB5c01tUHJaTWZEejRuUlpWa09XdWorMTFUVEUwTHBZb0lxYTdjNm5wUk12TmxyRjhNQTB1VUlBCjdDY01Qc2FUQjNWTmpHck5ueEZrUTh2UFRpQnRWU2NRQVNBc3V5Q3psNHJ5d210eXVmTEh2ZDJxeU8rWGFrL3YKY0hvT2I4bndsWG9VRS9DbmNyYWpqaFdCbWU5SVI4MWxxSVFNKzNBQ0l3SURBUUFCTUEwR0NTcUdTSWIzRFFFQgpDd1VBQTRJQ0FRQm1CUWxDczNRSmtUSEpsT0g4UjU0WTlPWFEzbWxzVmpsYThQemE3dkpIL2l6bTJGdk5EK1lVCjBDT2V6MDE4MlpXbGtqcllOWTdVblk5aVp1ODNWSVpFc1VjVlc3M05NajN0Zi80dG1aZlRPR1JLYmhoZUtNMUcKRDR0T3dwNjVlWExwbFlRazltSEIvenFDWk9vUmwwMHByNlFWYU5oUUlQdzJ1eTkyL2thVlhNOGhqVTBKOWhHNApwdis4MEpYc3A2bk1aNVQxY3FWdWZta3REektyUmxZNmkzUDBTbHdZNE9OTy9LSzcwNTJUS1RKOG5tZDFuYnpwClJXNG5iRTVnbzlYc2xSWDlQTS9jWW1rSTFwdk5vdFN6T2l3OWJ6UWo1ME45cWVVNTBwSU9WL2JZRkQrVU5rNEcKRDJ3Sk55YW9GRGlWS3U2ajRkaDFxQW93NkNsdEV1dWYydi91S0NUd2QyRzF2UlVFZTRVUDlVREoxeDZlVVVJYQoxMm1sbCtXdUt0NXNUWlRCelczYkhTRXRuQnZyMVVxWlFHdmw4UHlhK1FmV2FWcEtWRVZuYlJpSW5MVkhOa2EzCkwrTm5acGE0WmFWUU9KYlpLbFNQUy9ucDNRL3cvZU1weGdEL2hacTVXMTB2dzM5YVBqZEF6NndIZGpuN1RmWG4KbGN1WnBIWjdNWlZBTERNbDFFdE9aQW5LYjhJajVLNWpWbTFGSGZsMXJWY0MzS2tIdU56TzU0OUNxYkVqelNkdgp5ZEVHbUZBZEpTYllwdnRHUWFtVHA0RnVheEdtbWxOME5mMDNIVmFSVnRUVml1SDB2MUViNDc2MVpKWjczbE55CnVmUkFXMnVDZUlRU1ZmdW1pcVJ5WmhFaEh3MmQ2WnlJMzY1TTQyaGtpZXI0M2E2QlJCVzEvdz09Ci0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0K
		key.pem: |
			LS0tLS1CRUdJTiBQUklWQVRFIEtFWS0tLS0tCk1JSUpRZ0lCQURBTkJna3Foa2lHOXcwQkFRRUZBQVNDQ1N3d2dna29BZ0VBQW9JQ0FRQ2Y0azQ0VExIb0NoWEIKYUJUdWFNazZZREdBM3I3VUlHUGE5UlZ4KzFUUWM1RmcvZnpUUG04bzR2aXl4MHArR2JRR1gvUzNIanEya0hoeAo1QktXcDc0RUVYYTU4MkFNNEpTeFFXWTBXNVVWVXdBZHl2TFdkRjU4ZFI5MkYwNDBZUnJnTzkxRmluYURWZ3VFClQ3U1VmU3p4amdRRzhFcjJmZFNXMkszL0JkMkFPYXBpUUZvNFZiUE8vdmoyWHE1TmJKTTlobWdueGlmYWdnNFIKNE9zblJvM3lhSmM0TkNENEFqYUtDb2RUVHVGZXZwbHdEMnpCcGt2WjhMSkZSRmxwNTNVZmI2QWRjUCtlYmx1NApvdmhZYWlRZVRnaG1HNG9LL0pUVHBhaDBseUdtRWZxcmthNWtuUEtaUmhCcFJZVnVVUlRvaFRHbzBlRjcwbHBhCjFGNVFlajNrUHNYMG1rQmluY2ZuRjR2L1h0T2k2Y1krdDd4bmV3OFAwaU16bDkxY241ekdvQkYzY2pseWplZTYKeHpZbHFtSGpsaDk5SzY4RmRDMzdNOXZHb0twYk1PeU1nUC9BMmx2OHZQbDd4bzFCUFBScmZaTllqbWt1MGNtdgpSazNsNVZsMU43OHc3dWswK2p4cXdFdUV5U3dxUnF0cnpOVFJtZ1QxbFF5MmU3NVVKVXFRMUg5ZzJNQmoweXNNCm1QclpNZkR6NG5SWlZrT1d1aisxMVRURTBMcFlvSXFhN2M2bnBSTXZObHJGOE1BMHVVSUE3Q2NNUHNhVEIzVk4KakdyTm54RmtROHZQVGlCdFZTY1FBU0FzdXlDemw0cnl3bXR5dWZMSHZkMnF5TytYYWsvdmNIb09iOG53bFhvVQpFL0NuY3JhampoV0JtZTlJUjgxbHFJUU0rM0FDSXdJREFRQUJBb0lDQUdQRzlDK1lWVkpNc09VQkVrYnlaOW9oClcrTmpuczE4NVRRb3pOaFVFOHIreEZRMlRVaDdaeDJwLzdCNlJKZkxiSmlwMjJ0SDF6WkZsSlRtMDE3bmtlS3kKRDFqZWRDdTFIN1k2N1JCeHN1a2E0akMxamJTZDdMVlkxbWg1Qk5vVlc1TmlhS1ZVVXIrRnZDdzNIYWVwTXBvUQptWnpHNnRGSEY1dUgzNVlPVC93TWdMTk9HNythWkZzaXJiWDZ3bVlaQXc1YlNiYkFwL0JxUjJPSzdOV1c1MURIClNzL05ZR0hGNThsZjVySHJ3U1BDYUxrUk56cm1qK0dUbjMwd3VXZ3BCT080WXNEYzJ2bEJQOFpMRmhiL0xra24KUTRDTllTbVlGVHk3M2hQY21TZ3RnalQ5OWtwZDA5d3BhR1o1OTFvd0NZOU9SLzVsOUlTMGNxVEtjWTFockNzLwpIWlNJNUNMaFpYMUN3a1BCeGlIYXhnVkJlZ2dRQ0hXTmJtM0lHQjZSYjV0U1pKaVd0OElUL1h3TlJJaEpibnQ0Cm1BdUVnN2dUU1UxNG8weUZjNEk3cXhxZnRTUFRIK0ZEUEx3L2xDbzdLVU0wNzNnRjcydkpqR1R1RUNVYTBqSVoKMmNtajE3Q0tFWk5XUktvSGJyM2x5cjVRRmdLNXBFMjlWbDkwV0d4NkUvV3FGYXpORmJydEZBbDZnL1RTQ1VmaApLY0gyMnY5RlJ6eGJISWc4MGEzQ1hmTS9EbklQaUVRYUYvZTRZSCtobXMvcXp6ZWRvNjJldEh3cUM5am1FS1RnClk3Z3RpZUJ6Q2VSQ3psTzEwMDZnYUJacGZLeEtnY3lCSzRxV3BCVVkvNU5HTDNwVWZJdUQ1MitEQXlLNC9uVHEKdTZDanpSM1pMQUU0UEZsYUdUSjVBb0lCQVFET1Q4cVh4RGVPSXNjNVFhM3NSS3hwSC9YSFprT2FjWFlJUk9iNwpYV3BTN21Jd3JCYVlMa1ZDRWJncHQzeGZ3MEd6MXNKS0FXMzA4V3g0MTQvTE1wT2RZMDZuSHNqSk03QVdpczNTCmFNdnRIL0tzUGlmMllwSHh1eUxIY0NuNE1TNzdneExQRjEwTHJvdEdJU3JNckhIdExsZFcxcUxGVHpDVE90QnUKQXhQZlZCM3B1bVU3WnBCTDdDbEE4Sko2dkJOSEZJNG9yY3BKU1BybnBoQUpjSkFaZWVwaENxZTVISlZKUTVzSwpGbk5rbC9xalJlWDdYd0N1dS9aU003ZndPK2hPVkZCelNLL2x6ZUVNRU1kYWlQTC9JVEQ2ZG0zejhaM0gwNERECmROdUtEcDhaN1VPNXNrNWUyaFVwMHNFQ3BQRzBCb2tmN2ZNQjJ0WU5NVHRZa085dkFvSUJBUURHWkFCeDRsMlcKMjBoRHNieloyQjR2N2h4R2cxWFlYTkhBK3ZES1FZeW1vNXZwV00ydjFiclltc2NxYlE1K0QybldaUGlyaWl3Lwo4SjczczdmajN0S1JDdk9pcjRFb015Z3JpVzNHWHlXWFVJa2cxWjVzYzdDQk1IMTRaL09ObnBIY01VZWJiVDNFCktsUVRxMFhjcS9QaWlFMWZLY0x5aVJncUdSTWZYVjVkYnJMR1BvTzdPc1I0TmdQZXdiMjVZL0t2OVFWM29IMGQKZXVSTXhtTU1YZHAwT2Zqb1YyZnUybUZhSlc4SnNGdlZ6Yi9lbzZGVUp4WWxqMFF5Ry9uVjlTRzN6d2pyM2lTeQpEVVM4UHpINWpUMTI0S0Q5dkpZRGZVTGF4N0txNGxJdjFQbk1WdVZXczNjNHQwV0Y2M2JWVUFIQU4zS0RDNUc4CmladkVYL1cwdFA2TkFvSUJBUUNqV1BldDNCU2tmQkxDMmFiTUI3OSthR2lmM084dnJCL3BBaXpqM3AyZFZkTDIKZUhwWE9XTnFvVDd3QUsvLzNrZjZETks5NTQzWXZ3SEVWK0FvNFQyUkFweTJveUFVZGRFNHQrT29jWUxzbHp2NwpkaWNMNUJWcmtHQkVDaUdndWNoYUtQaE9jVkFoUEt4VzlWRyt4ZFphRlRQZnRJY2hzOFpnKzlNbEYxaTNuUkVtCkNvZTJWVWx3WTJaeVhVZU0xN1pucy9XdWJaTlpIT2hUV3Q4ZHFqcmRnUEs2ck1ZSlFZRk5oYktPZFNJZUJsclMKeFRnSEk3d1ZuUXExSU8vRXpKbnMwc0x6MUJ3NDFoNFdBSDdteHNHbWtQQUhqcGNWNnpxaWlXcE0xd3d2cmMzNApxQ3ZVTGtId3hiaTE2WUVhQitDN1NlVnVHMmNwRTh3Z205ZENFMWNQQW9JQkFHUDVqUWZXN1JiU2xrNFd5WFoyCkpIQSs2OXpVM25QVUFwZmZYV3h2TC9QaHl2WUNuRlNadmpqZGRyUjRsSzhPRVdYTEtFMDVxaWJtbVJWMmFacloKZFA5R3A1UTZJVG9pM1lGakZnQzdmZlFNejYzT09MR3FjeTRIUTVOanZ5YUUzRGc4VlR1TUIyNU5ibVVqRUdldAo5NDhXNVBhcDB1WHFGRlZTb1lKU3lQVUlqZXE5SWlFOThqZ3A4RFZYS01hK0NWU0dneVRQcVgwcnF0VE52S2hFCnU0dUtrMVp5aFp1bVRSemlkRnhMbFZ2ZS9XdXl4ZC9rZXBLZTZkemVvRDRqODhQdS95M3Rta3hueDFXZCt3OHAKRCtwU05JN3BkQ2Q1L2pER0pkRmJqOU11M2xzTkJ6Rno2d2FYeE45QjAzYVhoT3BhaHNobkVpQVNzSDU3WlJTVgppUmtDZ2dFQUtPR1NibXkrUEl3dTA5WHI0a2RGWlZqc3F2S0NrZHBFTEVXWmhJT2pQanVVV1BvRXllL0xxR3NCCmFqd3pTdlFLQmU2U2xLeXZ4SVNsdmJmWktseG5JUW1SRHpoZ0x0SGo1dG1ZRDV4dHJmM1BtYUVmUlhMSy93bG8KNk1aRnczK21qaTNORmtPN0F5M3NoVzMwYnlKcDRyeHhrWUgvOHpnMjN3dnZNY2RXOHNBVXJlWW5BWkpSM1lxdQpoeS91aDlJajNDdXBSc1Q3Nyttb3o4YlZReEN0aGxFK2s0NG54ZWNqMUw0SzZuWEpnOHUxV3BPN0FNc1l3aXFKClJxVEJXeXV3UXR3QkxMUWxiQVBTTmlxaXZtb2pyTGdNZmdXam90SWFqYTRFV0RzcmlkaTRkcXVTb2Irc0tXRjIKYU1HYXRWRzQvbjNsdmphTDNNbGRuaENOWXB4TmdBPT0KLS0tLS1FTkQgUFJJVkFURSBLRVktLS0tLQo=

You can make the values from this secret available through a custom volume mount:

	apiVersion: apps.foundationdb.org/v1beta1
	kind: FoundationDBCluster
	metadata:
		name: sample-cluster
	spec:
		version: 6.2.10
		databaseConfiguration:
			redundancy_mode: double
			storage: 5
		volumeSize: "128G"
		resources:
			requests:
				cpu: 2
				memory: 8Gi
		volumes:
			-	name: fdb-certs
				secret:
					secretName: fdb-kubernetes-operator-secrets
		mainContainer:
			env:
				-	name: FDB_TLS_CERTIFICATE_FILE
					value: /tmp/fdb-certs/cert.pem
				-	name: FDB_TLS_CA_FILE
					value: /tmp/fdb-certs/cert.pem
				-	name: FDB_TLS_KEY_FILE
					value: /tmp/fdb-certs/key.pem
			volumeMounts:
				-	name: fdb-certs
					mountPath: /tmp/fdb-certs

This will delete the pods in the cluster and recreate them with the new environment variables and volumes. The operator will follow the rolling bounce process described in "Upgrading a Cluster".

You can customize the same kind of fields on the sidecar container by adding them under the `sidecarContainer` section of the spec.

Note: The example above adds certificates to the environment, but it does not enable TLS for the cluster. We do not currently have a way to enable TLS once a cluster is running. If you set the `enableTLS` flag on the container when you create the cluster, it will be created with TLS enabled. See the [example TLS cluster](../config/samples/cluster_local_tls.yaml) for more details on this configuration.

# Controlling Fault Domains

The operator provides multiple options for defining fault domains for your cluster. The fault domain defines how data is replicated and how processes are replicated across machines. Choosing a fault domain is an important process of managing your deployments.

Fault domains are controlled through the `faultDomain` field in the cluster spec.

## Option 1: Single-Kubernetes Replication

The default fault domain strategy is to replicate across nodes in a single Kubernetes cluster. If you do not specify any fault domain option, we will replicate across nodes.


	apiVersion: apps.foundationdb.org/v1beta1
	kind: FoundationDBCluster
	metadata:
		name: sample-cluster
	spec:
		version: 6.2.10
		databaseConfiguration:
			redundancy_mode: double
			storage: 5
		volumeSize: "128G"
		faultDomain:
			key: topology.kubernetes.io/zone # default: kubernetes.io/hostname
			valueFrom: spec.zoneName # default: spec.nodeName
		resources:
			requests:
				cpu: 2
				memory: 8Gi

The example above divides processes across nodes based on the label `topology.kubernetes.io/zone` on the node, and sets the zone locality information in FDB based on the field `spec.zoneName` on the pod. The latter field does not exist, so this configuration cannot work. There is no clear pattern in Kubernetes for allowing pods to access node information other than the host name, which presents challenges using any other kind of fault domain.

If you have some other mechanism to make this information available in your pod's environment, you can tell the operator to use an environment varilable as the source for the zone locality:


	apiVersion: apps.foundationdb.org/v1beta1
	kind: FoundationDBCluster
	metadata:
		name: sample-cluster
	spec:
		version: 6.2.10
		databaseConfiguration:
			redundancy_mode: double
			storage: 5
		volumeSize: "128G"
		faultDomain:
			key: topology.kubernetes.io/zone
			valueFrom: $RACK
		resources:
			requests:
				cpu: 2
				memory: 8Gi

## Option 2: Multi-Kubernetes Replication

Our second strategy is to run multiple Kubernetes cluster, each as its own fault domain. This strategy adds significant operational complexity, but may allow you to have stronger fault domains and thus more reliable deployments. You can enable this strategy by using a special key in the fault domain:

	apiVersion: apps.foundationdb.org/v1beta1
	kind: FoundationDBCluster
	metadata:
		name: sample-cluster
	spec:
		version: 6.2.10
		databaseConfiguration:
			redundancy_mode: double
			storage: 5
		volumeSize: "128G"
		faultDomain:
			key: foundationdb.org/kubernetes-cluster
			value: zone2
			zoneIndex: 2
			zoneCount: 5
		resources:
			requests:
				cpu: 2
				memory: 8Gi

This tells the operator to use the value "zone2" as the fault domain for every process it creates. The zoneIndex and zoneCount tell the operator where this fault domain is within the list of Kubernetes clusters (KCs) you are using in this DC. This is used to divide processes across fault domains. For instance, this configuration has 7 stateless processes, which needs to be divided across 5 fault domains. The instances with zoneIndex 1 and 2 will allocate 2 stateless processes each. The instances with zoneIndex 3, 4, and 5 will allocate 1 stateless process each.

When running across multiple KCs, you will need to apply more care in managing the configurations to make sure all the KCs converge on the same view of the desired configuration. You will likely need some kind of external, global system to store the canonical configuration and push it out to all of your KCs. You will also need to make sure that the different KCs are not fighting each other to control the database configuration. You can set different flags in `automationOptions` to control what the operator can change about the cluster. You can use these fields to designate a master instance of the operator which will handle things like reconfiguring the database.

When upgrading a cluster, you will need to bounce instances simultaneously across all of your KCs. The best way to do this is to disable killing processes through `automationOptions`, and then kill them outside of the reconciliation loop. You can determine if each instance of the operator is ready for the kill by checking the `generations` field in the cluster status. The new generation will appear there under the field `needsBounce` when the operator is ready to do a bounce, but is forbidden from doing so by the automation options.

Example with automation options disabled:

	apiVersion: apps.foundationdb.org/v1beta1
	kind: FoundationDBCluster
	metadata:
		name: sample-cluster
	spec:
		version: 6.2.10
		databaseConfiguration:
			redundancy_mode: double
			storage: 5
		automationOptions:
			configureDatabase: false
			killProcesses: false
		volumeSize: "128G"
		faultDomain:
			key: foundationdb.org/kubernetes-cluster
			value: zone2
			zoneIndex: 2
			zoneCount: 5
		resources:
			requests:
				cpu: 2
				memory: 8Gi

## Option 3: Fake Replication

In local test environments, you may not having any real fault domains to use, and may not care about availability. You can test in this environment while still having replication enabled by using fake fault domains:

	apiVersion: apps.foundationdb.org/v1beta1
	kind: FoundationDBCluster
	metadata:
		name: sample-cluster
	spec:
		version: 6.2.10
		databaseConfiguration:
			redundancy_mode: double
			storage: 5
		automationOptions:
			configureDatabase: false
			killProcesses: false
		volumeSize: "128G"
		faultDomain:
			key: foundationdb.org/none
		resources:
			requests:
				cpu: 2
				memory: 8Gi

This strategy uses the pod name as the fault domain, which allows each process to act as a separate failure domain. Any hardware failure could lead to a complete loss of the cluster. This configuration should not be used in any production environment.

## Multi-Region Replication

The replication strategies above all describe how data is replicated within a data center. They control the `zoneid` field in the cluster's locality. If you want to run a cluster across multiple data centers, you can use FoundationDB's multi-region replication. This can work with any of the replication stragies above. The data center will be a separate fault domain from whatever you provide for the zone. 


	apiVersion: apps.foundationdb.org/v1beta1
	kind: FoundationDBCluster
	metadata:
		name: sample-cluster
	spec:
		version: 6.2.10
		dataCenter: dc1
		databaseConfiguration:
			redundancy_mode: double
			storage: 5
			regions:
				-	datacenters:
						-	id: dc1
							priority: 1
		volumeSize: "128G"
		resources:
			requests:
				cpu: 2
				memory: 8Gi

The `dataCenter` field in the top level of the spec specifies what data center these instances are running in. This will be used to set the `dcid` locality field. The `regions` section of the database describes all of the available regions. See the [FoundationDB documentation](https://apple.github.io/foundationdb/configuration.html#configuring-regions) for more information on how to configure regions. 

Replicating across data centers will likely mean running your cluster across multiple Kubernetes clusters, even if you are using a single-Kubernetes replication strategy within each DC. This will mean taking on the operational challenges described in the "Multi-Kubernetes Replication" section above.