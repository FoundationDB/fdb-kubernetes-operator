# Table of Contents

1. [Introduction](#introduction)
1. [Creating a Cluster](#creating-a-cluster)
1. [Accessing a Cluster](#accessing-a-cluster)
1. [Managing Process Counts](#managing-process-counts)
1. [Growing a Cluster](#growing-a-cluster)
1. [Shrinking a Cluster](#shrinking-a-cluster)
1. [Replacing a Process](#replacing-a-process)
1. [Changing Database Configuration](#changing-database-configuration)
1. [Adding a Knob](#adding-a-knob)
1. [Upgrading a Cluster](#upgrading-a-cluster)
1. [Running multiple Storage Servers per Pod](#running-multiple-storage-servers-per-pod)
1. [Customizing the StorageClass](#customizing-the-storageclass)
1. [Customizing Your Pods](#customizing-your-pods)
1. [Controlling Fault Domains](#controlling-fault-domains)
1. [Using Multiple Namespaces](#using-multiple-namespaces)
1. [Choosing Your Public IP Source](choosing-your-public-ip-source)
1. [Renaming a Cluster](#renaming-a-cluster)
1. [Coordinating Global Operations](#coordinating-global-operations)

# Introduction

This document provides practical examples of how to use the FoundationDB Kubernetes Operator to accomplish common tasks, and additional information and areas to consider when you are managing these tasks in a real environment.

This document assumes that you are generally familiar with Kuberneters. For more information on that area, see the [Kubernetes documentation](https://kubernetes.io/docs/home/).

The core of the operator is a reconciliation loop. In this loop, the operator reads the latest cluster spec, compares it to the running state of the cluster, and carries out whatever tasks need to be done to make the running state of the cluster match the desired state as expressed in the cluster spec. If the operator cannot fully reconcile the cluster in a single pass, it will try the reconciliation again. This can occur for a number of reasons: operations that are disabled, operations that require asynchronous work, error conditions, and so on.

When you make a change to the cluster spec, it will increment the `generation` field in the cluster metadata. Once reconciliation completes, the `generations.reconciled` field in the cluster status will be updated to reflect the last generation that we have reconciled. You can compare these two fields to determine whether your changes have been fully applied. You can also see the current generation and reconciled generation in the output of `kubectl get foundationdbcluster`.

To run the operator in your environment, you need to install the controller and
the CRDs:

    kubectl apply -f https://raw.githubusercontent.com/FoundationDB/fdb-kubernetes-operator/master/config/crd/bases/apps.foundationdb.org_foundationdbclusters.yaml
    kubectl apply -f https://raw.githubusercontent.com/FoundationDB/fdb-kubernetes-operator/master/config/crd/bases/apps.foundationdb.org_foundationdbbackups.yaml
    kubectl apply -f https://raw.githubusercontent.com/FoundationDB/fdb-kubernetes-operator/master/config/crd/bases/apps.foundationdb.org_foundationdbrestores.yaml
    kubectl apply -f https://raw.githubusercontent.com/foundationdb/fdb-kubernetes-operator/master/config/samples/deployment.yaml

You can see logs from the operator by running
`kubectl logs -f -l app=fdb-kubernetes-operator-controller-manager --container=manager`. You will likely want to watch these logs as you make changes to get a better understanding of what the operator is doing.

The example below will cover creating a cluster. All subsequent examples will assume that you have
just created this cluster, and will cover an operation on this cluster.

For more information on the fields you can define on the cluster resource, see
the [go docs](https://godoc.org/github.com/FoundationDB/fdb-kubernetes-operator/pkg/apis/apps/v1beta1#FoundationDBCluster).

For more information on version compatibility, see our [compatibility guide](docs/compatibility.md).

## Before You Create a Cluster

This operator aims to support as many different environments and use cases as we can, by offering a variety of customization options. You may find that the stock configurations don't work as desired in your environment. We recommend reading through this entire manual in order to get a feel for how the operator works. There are a few sections that are most likely to be relevant before you create a cluster: [Customizing Your Pods](#customizing-a-container), [Controlling Fault Domains](#controlling-fault-domains), and [Using Multiple Namespaces](#using-multiple-namespaces).

# Creating a Cluster

To start with, we are going to be creating a cluster with the following configuration:

    apiVersion: apps.foundationdb.org/v1beta1
    kind: FoundationDBCluster
    metadata:
      name: sample-cluster
    spec:
      version: 6.2.20
      databaseConfiguration:
        storage: 5

This will create a cluster with 5 storage processes, 4 log processes, and 7 stateless processes. Each fdbserver process will be in a separate pod, and the pods will have names of the form `sample-cluster-$role-$n`, where `$n` is the instance ID and `$role` is the role for the process.

You can run `kubectl get foundationdbcluster sample-cluster` to check the progress of reconciliation. Once the reconciled generation appears in this output, the cluster should be up and ready. After creating the cluster, you can connect to the cluster by running `kubectl exec -it sample-cluster-log-1 -- fdbcli`.

This example requires non-trivial resources, based on what a process will need in a production environment. This means that is too large to run in a local testing environment. It also requires disk I/O features that are not present in Docker for Mac. If you want to run these tests in that kind of environment, you can try bringing in the resource requirements, knobs, and fault domain information from a [local testing example](../config/samples/cluster_local.yaml).

In addition to the pods, the operator will create a Persistent Volume Claim for any stateful
processes in the cluster. In this example, each volume will be 128 GB.

By default each pod will have two containers and one init container.. The `foundationdb` container will run fdbmonitor and fdbserver, and is the main container for the pod. The `foundationdb-kubernetes-sidecar` container will run a sidecar image designed to help run FDB on Kubernetes. It is responsible for managing the fdbmonitor conf files and providing FDB binaries to the `foundationdb` container. The operator will create a config map that contains a template for the monitor conf file, and the sidecar will interpolate instance-specific fields into the conf and make it available to the fdbmonitor process through a shared volume. The "Upgrading a Cluster" has more detail on we manage binaries. The init container will run the same sidecar image, and will ensure that the initial binaries and dynamic conf are ready before the fdbmonitor process starts.

# Accessing a cluster

Now that your cluster is deployed, you can easily access the cluster. As an example, we are going to deploy a [Kubernetes Job](https://kubernetes.io/docs/tasks/job/) that will check the status of the cluster every minute. The `cluster file` is available through the exposed `config map` that can be mounted as follows:


    apiVersion: batch/v1beta1
    kind: CronJob
    metadata:
      name: fdbcli-status-cronjob
    spec:
      schedule: "*/1 * * * *" # every minute
      jobTemplate:
        spec:
          template:
            spec:
              restartPolicy: OnFailure
              containers:
              - name: fdbcli-status-cronjob
                image: foundationdb/foundationdb:6.2.20
                args:
                - /usr/bin/fdbcli
                - --exec
                - 'status'
                env:
                - name: FDB_CLUSTER_FILE
                  value: /mnt/config-volume/cluster-file
                volumeMounts:
                - name: config-volume
                  mountPath: /mnt/config-volume
              volumes:
              - name: config-volume
                configMap:
                  name: sample-cluster-config

Be careful that:

* the cluster-file is mutable,
* the name of config map depends on the name of your cluster.

# Managing Process Counts

You can manage process counts in either the database configuration or in the process counts in the cluster spec. In most of these examples, we will only manage process counts through the database configuration. This is simpler, and it ensures that the number of processes we launch fits the number of processes that we are telling the database to recruit.

To explicitly set process counts, you could configure the cluster as follows:

    apiVersion: apps.foundationdb.org/v1beta1
    kind: FoundationDBCluster
    metadata:
      name: sample-cluster
    spec:
      version: 6.2.20
      processCounts:
        storage: 6
        log: 5
        stateless: 4


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
      version: 6.2.20
      databaseConfiguration:
        storage: 6
        logs: 4 # default is 3
        proxies: 5 # default is 3
        resolvers: 2 # default is 1

This will provision 1 additional log process and 3 additional stateless processes. After launching those processes, it will change the database configuration to recruit 1 additional log, 2 additional proxies, and 1 additional resolver.

# Shrinking a Cluster

You can shrink a cluster by changing the database configuration or process count, just like when we grew a cluster:

    apiVersion: apps.foundationdb.org/v1beta1
    kind: FoundationDBCluster
    metadata:
      name: sample-cluster
    spec:
      version: 6.2.20
      databaseConfiguration:
        storage: 4

The operator will determine which processes to remove and store them in the `instancesToRemove` field in the cluster spec to make sure the choice of removal stays consistent across repeated runs of the reconciliation loop. Once the processes are in the `instancesToRemove` list, we will exclude them from the database, which moves all of the roles and data off of the process. Once the exclusion is complete, it is safe to remove the processes, and the operator will delete both the pods and the PVCs. Once the processes are shut down, the operator will re-include them to make sure the exclusion state doesn't get cluttered. It will also remove the process from the `instancesToRemove` list.

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
      version: 6.2.20
      databaseConfiguration:
        storage: 5
      instancesToRemove:
        - storage-1

When comparing the desired process count with the current pod count, any pods that are in the pending removal list are not counted. This means that the operator will only consider there to be 4 running storage pods, rather than 5, and will create a new one to fill the gap. Once this is done, it will go through the same removal process described above under "Shrinking a Cluster". The cluster will remain at full fault tolerance throughout the reconciliation. This allows you to replace an arbitrarily large number of processes in a cluster without any risk of availability loss.

# Changing Database Configuration

You can reconfigure the database by changing the fields in the database configuration:

    apiVersion: apps.foundationdb.org/v1beta1
    kind: FoundationDBCluster
    metadata:
      name: sample-cluster
    spec:
      version: 6.2.20
      databaseConfiguration:
        redundancy_mode: triple
        storage: 5

This will run the configuration command on the database, and may also add or remove processes to match the new configuration.

# Adding a Knob

To add a knob, you can change the customParameters in the cluster spec:

    apiVersion: apps.foundationdb.org/v1beta1
    kind: FoundationDBCluster
    metadata:
      name: sample-cluster
    spec:
      version: 6.2.20
      databaseConfiguration:
        storage: 5
      customParameters:
        - "knob_always_causal_read_risky=1"

The operator will update the monitor conf to contain the new knob, and will then bounce all of the fdbserver processes. As soon as fdbmonitor detects that the fdbserver process has died, it will create a new fdbserver process with the latest config. The cluster should be fully available within 10 seconds of executing the bounce, though this can vary based on cluster size.

The process for updating the monitor conf can take several minutes, based on the time it takes Kubernetes to update the config map in the pods.

# Upgrading a Cluster

To upgrade a cluster, you can change the version in the cluster spec:

    apiVersion: apps.foundationdb.org/v1beta1
    kind: FoundationDBCluster
    metadata:
      name: sample-cluster
    spec:
      version: 6.2.20
      databaseConfiguration:
        storage: 5

This will first update the sidecar image in the pod to match the new version, which will restart that container. On restart, it will copy the new FDB binaries into the config volume for the foundationdb container, which will make it available to run. We will then update the fdbmonitor conf to point to the new binaries and bounce all of the fdbserver processes.

Once all of the processes are running at the new version, we will recreate all of the pods so that the `foundationdb` container uses the new version for its own image. This will use the strategies described in [Pod Update Strategy](#pod-update-strategy).

# Running multiple Storage Servers per Pod

Since FoundationDB is limited to a single core it can make sense to run multiple storage server per disk.
You can change the number of storage server per Pod with the `storageServersPerPod`.
This will start multiple FDB processes inside of a single container with `fdbmonitor`.

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.20
  spec:
    storageServersPerPod: 2
```

A change to the `storageServersPerPod` will replace storage Pods.
For more information about this feature read the [multplie storage servers per Pod](./design/multiple_storage_per_disk.md) design doc.

# Customizing the StorageClass

To use a different `StorageClass` than the default you can set your desired `StorageClass` in the [process settings](./cluster_spec.md#processsettings):

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.20
  processes:
    general:
      volumeClaimTemplate:
        spec:
          storageClassName: my-storage-class
```

A change to the `StorageClass` will replace all PVC's and the according Pods. You can also use different `StorageClasses` for different processes.

# Customizing Your Pods

There are many fields in the cluster spec that allow configuring your pods. You can define custom environment variables, add your own containers, add additional volumes, and more. You may want to use these fields to handle things that are specific to your environment, like managing certificates or forwarding logs to a central system.

In this example, we are going to add a volume that mounts certificates from a Kubernetes secret. This is likely not how you would want to manage certificates in a real environment, but it can be helpful as an example.

Before you apply this change, you will need to create a secret. You can apply the following YAML to your environment to set up a secret with the self-signed cert that we use for testing the operator:

    apiVersion: v1
    kind: Secret
    metadata:
      name: fdb-kubernetes-operator-secrets
    data:
      tls.crt: |
        LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUZ4RENDQTZ3Q0NRQ29Bd2FySFhWeWtEQU5CZ2txaGtpRzl3MEJBUXNGQURDQm96RUxNQWtHQTFVRUJoTUMKVlZNeEN6QUpCZ05WQkFnTUFrTkJNUkl3RUFZRFZRUUhEQWxEZFhCbGNuUnBibTh4RlRBVEJnTlZCQW9NREVadgpkVzVrWVhScGIyNUVRakVWTUJNR0ExVUVDd3dNUm05MWJtUmhkR2x2YmtSQ01Sa3dGd1lEVlFRRERCQm1iM1Z1ClpHRjBhVzl1WkdJdWIzSm5NU293S0FZSktvWklodmNOQVFrQkZodGtiMjV2ZEhKbGNHeDVRR1p2ZFc1a1lYUnAKYjI1a1lpNXZjbWN3SGhjTk1Ua3dOak13TWpNME9URTFXaGNOTWpBd05qSTVNak0wT1RFMVdqQ0JvekVMTUFrRwpBMVVFQmhNQ1ZWTXhDekFKQmdOVkJBZ01Ba05CTVJJd0VBWURWUVFIREFsRGRYQmxjblJwYm04eEZUQVRCZ05WCkJBb01ERVp2ZFc1a1lYUnBiMjVFUWpFVk1CTUdBMVVFQ3d3TVJtOTFibVJoZEdsdmJrUkNNUmt3RndZRFZRUUQKREJCbWIzVnVaR0YwYVc5dVpHSXViM0puTVNvd0tBWUpLb1pJaHZjTkFRa0JGaHRrYjI1dmRISmxjR3g1UUdadgpkVzVrWVhScGIyNWtZaTV2Y21jd2dnSWlNQTBHQ1NxR1NJYjNEUUVCQVFVQUE0SUNEd0F3Z2dJS0FvSUNBUUNmCjRrNDRUTEhvQ2hYQmFCVHVhTWs2WURHQTNyN1VJR1BhOVJWeCsxVFFjNUZnL2Z6VFBtOG80dml5eDBwK0diUUcKWC9TM0hqcTJrSGh4NUJLV3A3NEVFWGE1ODJBTTRKU3hRV1kwVzVVVlV3QWR5dkxXZEY1OGRSOTJGMDQwWVJyZwpPOTFGaW5hRFZndUVUN1NVZlN6eGpnUUc4RXIyZmRTVzJLMy9CZDJBT2FwaVFGbzRWYlBPL3ZqMlhxNU5iSk05CmhtZ254aWZhZ2c0UjRPc25SbzN5YUpjNE5DRDRBamFLQ29kVFR1RmV2cGx3RDJ6QnBrdlo4TEpGUkZscDUzVWYKYjZBZGNQK2VibHU0b3ZoWWFpUWVUZ2htRzRvSy9KVFRwYWgwbHlHbUVmcXJrYTVrblBLWlJoQnBSWVZ1VVJUbwpoVEdvMGVGNzBscGExRjVRZWoza1BzWDBta0JpbmNmbkY0di9YdE9pNmNZK3Q3eG5ldzhQMGlNemw5MWNuNXpHCm9CRjNjamx5amVlNnh6WWxxbUhqbGg5OUs2OEZkQzM3TTl2R29LcGJNT3lNZ1AvQTJsdjh2UGw3eG8xQlBQUnIKZlpOWWpta3UwY212UmszbDVWbDFONzh3N3VrMCtqeHF3RXVFeVN3cVJxdHJ6TlRSbWdUMWxReTJlNzVVSlVxUQoxSDlnMk1CajB5c01tUHJaTWZEejRuUlpWa09XdWorMTFUVEUwTHBZb0lxYTdjNm5wUk12TmxyRjhNQTB1VUlBCjdDY01Qc2FUQjNWTmpHck5ueEZrUTh2UFRpQnRWU2NRQVNBc3V5Q3psNHJ5d210eXVmTEh2ZDJxeU8rWGFrL3YKY0hvT2I4bndsWG9VRS9DbmNyYWpqaFdCbWU5SVI4MWxxSVFNKzNBQ0l3SURBUUFCTUEwR0NTcUdTSWIzRFFFQgpDd1VBQTRJQ0FRQm1CUWxDczNRSmtUSEpsT0g4UjU0WTlPWFEzbWxzVmpsYThQemE3dkpIL2l6bTJGdk5EK1lVCjBDT2V6MDE4MlpXbGtqcllOWTdVblk5aVp1ODNWSVpFc1VjVlc3M05NajN0Zi80dG1aZlRPR1JLYmhoZUtNMUcKRDR0T3dwNjVlWExwbFlRazltSEIvenFDWk9vUmwwMHByNlFWYU5oUUlQdzJ1eTkyL2thVlhNOGhqVTBKOWhHNApwdis4MEpYc3A2bk1aNVQxY3FWdWZta3REektyUmxZNmkzUDBTbHdZNE9OTy9LSzcwNTJUS1RKOG5tZDFuYnpwClJXNG5iRTVnbzlYc2xSWDlQTS9jWW1rSTFwdk5vdFN6T2l3OWJ6UWo1ME45cWVVNTBwSU9WL2JZRkQrVU5rNEcKRDJ3Sk55YW9GRGlWS3U2ajRkaDFxQW93NkNsdEV1dWYydi91S0NUd2QyRzF2UlVFZTRVUDlVREoxeDZlVVVJYQoxMm1sbCtXdUt0NXNUWlRCelczYkhTRXRuQnZyMVVxWlFHdmw4UHlhK1FmV2FWcEtWRVZuYlJpSW5MVkhOa2EzCkwrTm5acGE0WmFWUU9KYlpLbFNQUy9ucDNRL3cvZU1weGdEL2hacTVXMTB2dzM5YVBqZEF6NndIZGpuN1RmWG4KbGN1WnBIWjdNWlZBTERNbDFFdE9aQW5LYjhJajVLNWpWbTFGSGZsMXJWY0MzS2tIdU56TzU0OUNxYkVqelNkdgp5ZEVHbUZBZEpTYllwdnRHUWFtVHA0RnVheEdtbWxOME5mMDNIVmFSVnRUVml1SDB2MUViNDc2MVpKWjczbE55CnVmUkFXMnVDZUlRU1ZmdW1pcVJ5WmhFaEh3MmQ2WnlJMzY1TTQyaGtpZXI0M2E2QlJCVzEvdz09Ci0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0K
      tls.key: |
        LS0tLS1CRUdJTiBQUklWQVRFIEtFWS0tLS0tCk1JSUpRZ0lCQURBTkJna3Foa2lHOXcwQkFRRUZBQVNDQ1N3d2dna29BZ0VBQW9JQ0FRQ2Y0azQ0VExIb0NoWEIKYUJUdWFNazZZREdBM3I3VUlHUGE5UlZ4KzFUUWM1RmcvZnpUUG04bzR2aXl4MHArR2JRR1gvUzNIanEya0hoeAo1QktXcDc0RUVYYTU4MkFNNEpTeFFXWTBXNVVWVXdBZHl2TFdkRjU4ZFI5MkYwNDBZUnJnTzkxRmluYURWZ3VFClQ3U1VmU3p4amdRRzhFcjJmZFNXMkszL0JkMkFPYXBpUUZvNFZiUE8vdmoyWHE1TmJKTTlobWdueGlmYWdnNFIKNE9zblJvM3lhSmM0TkNENEFqYUtDb2RUVHVGZXZwbHdEMnpCcGt2WjhMSkZSRmxwNTNVZmI2QWRjUCtlYmx1NApvdmhZYWlRZVRnaG1HNG9LL0pUVHBhaDBseUdtRWZxcmthNWtuUEtaUmhCcFJZVnVVUlRvaFRHbzBlRjcwbHBhCjFGNVFlajNrUHNYMG1rQmluY2ZuRjR2L1h0T2k2Y1krdDd4bmV3OFAwaU16bDkxY241ekdvQkYzY2pseWplZTYKeHpZbHFtSGpsaDk5SzY4RmRDMzdNOXZHb0twYk1PeU1nUC9BMmx2OHZQbDd4bzFCUFBScmZaTllqbWt1MGNtdgpSazNsNVZsMU43OHc3dWswK2p4cXdFdUV5U3dxUnF0cnpOVFJtZ1QxbFF5MmU3NVVKVXFRMUg5ZzJNQmoweXNNCm1QclpNZkR6NG5SWlZrT1d1aisxMVRURTBMcFlvSXFhN2M2bnBSTXZObHJGOE1BMHVVSUE3Q2NNUHNhVEIzVk4KakdyTm54RmtROHZQVGlCdFZTY1FBU0FzdXlDemw0cnl3bXR5dWZMSHZkMnF5TytYYWsvdmNIb09iOG53bFhvVQpFL0NuY3JhampoV0JtZTlJUjgxbHFJUU0rM0FDSXdJREFRQUJBb0lDQUdQRzlDK1lWVkpNc09VQkVrYnlaOW9oClcrTmpuczE4NVRRb3pOaFVFOHIreEZRMlRVaDdaeDJwLzdCNlJKZkxiSmlwMjJ0SDF6WkZsSlRtMDE3bmtlS3kKRDFqZWRDdTFIN1k2N1JCeHN1a2E0akMxamJTZDdMVlkxbWg1Qk5vVlc1TmlhS1ZVVXIrRnZDdzNIYWVwTXBvUQptWnpHNnRGSEY1dUgzNVlPVC93TWdMTk9HNythWkZzaXJiWDZ3bVlaQXc1YlNiYkFwL0JxUjJPSzdOV1c1MURIClNzL05ZR0hGNThsZjVySHJ3U1BDYUxrUk56cm1qK0dUbjMwd3VXZ3BCT080WXNEYzJ2bEJQOFpMRmhiL0xra24KUTRDTllTbVlGVHk3M2hQY21TZ3RnalQ5OWtwZDA5d3BhR1o1OTFvd0NZOU9SLzVsOUlTMGNxVEtjWTFockNzLwpIWlNJNUNMaFpYMUN3a1BCeGlIYXhnVkJlZ2dRQ0hXTmJtM0lHQjZSYjV0U1pKaVd0OElUL1h3TlJJaEpibnQ0Cm1BdUVnN2dUU1UxNG8weUZjNEk3cXhxZnRTUFRIK0ZEUEx3L2xDbzdLVU0wNzNnRjcydkpqR1R1RUNVYTBqSVoKMmNtajE3Q0tFWk5XUktvSGJyM2x5cjVRRmdLNXBFMjlWbDkwV0d4NkUvV3FGYXpORmJydEZBbDZnL1RTQ1VmaApLY0gyMnY5RlJ6eGJISWc4MGEzQ1hmTS9EbklQaUVRYUYvZTRZSCtobXMvcXp6ZWRvNjJldEh3cUM5am1FS1RnClk3Z3RpZUJ6Q2VSQ3psTzEwMDZnYUJacGZLeEtnY3lCSzRxV3BCVVkvNU5HTDNwVWZJdUQ1MitEQXlLNC9uVHEKdTZDanpSM1pMQUU0UEZsYUdUSjVBb0lCQVFET1Q4cVh4RGVPSXNjNVFhM3NSS3hwSC9YSFprT2FjWFlJUk9iNwpYV3BTN21Jd3JCYVlMa1ZDRWJncHQzeGZ3MEd6MXNKS0FXMzA4V3g0MTQvTE1wT2RZMDZuSHNqSk03QVdpczNTCmFNdnRIL0tzUGlmMllwSHh1eUxIY0NuNE1TNzdneExQRjEwTHJvdEdJU3JNckhIdExsZFcxcUxGVHpDVE90QnUKQXhQZlZCM3B1bVU3WnBCTDdDbEE4Sko2dkJOSEZJNG9yY3BKU1BybnBoQUpjSkFaZWVwaENxZTVISlZKUTVzSwpGbk5rbC9xalJlWDdYd0N1dS9aU003ZndPK2hPVkZCelNLL2x6ZUVNRU1kYWlQTC9JVEQ2ZG0zejhaM0gwNERECmROdUtEcDhaN1VPNXNrNWUyaFVwMHNFQ3BQRzBCb2tmN2ZNQjJ0WU5NVHRZa085dkFvSUJBUURHWkFCeDRsMlcKMjBoRHNieloyQjR2N2h4R2cxWFlYTkhBK3ZES1FZeW1vNXZwV00ydjFiclltc2NxYlE1K0QybldaUGlyaWl3Lwo4SjczczdmajN0S1JDdk9pcjRFb015Z3JpVzNHWHlXWFVJa2cxWjVzYzdDQk1IMTRaL09ObnBIY01VZWJiVDNFCktsUVRxMFhjcS9QaWlFMWZLY0x5aVJncUdSTWZYVjVkYnJMR1BvTzdPc1I0TmdQZXdiMjVZL0t2OVFWM29IMGQKZXVSTXhtTU1YZHAwT2Zqb1YyZnUybUZhSlc4SnNGdlZ6Yi9lbzZGVUp4WWxqMFF5Ry9uVjlTRzN6d2pyM2lTeQpEVVM4UHpINWpUMTI0S0Q5dkpZRGZVTGF4N0txNGxJdjFQbk1WdVZXczNjNHQwV0Y2M2JWVUFIQU4zS0RDNUc4CmladkVYL1cwdFA2TkFvSUJBUUNqV1BldDNCU2tmQkxDMmFiTUI3OSthR2lmM084dnJCL3BBaXpqM3AyZFZkTDIKZUhwWE9XTnFvVDd3QUsvLzNrZjZETks5NTQzWXZ3SEVWK0FvNFQyUkFweTJveUFVZGRFNHQrT29jWUxzbHp2NwpkaWNMNUJWcmtHQkVDaUdndWNoYUtQaE9jVkFoUEt4VzlWRyt4ZFphRlRQZnRJY2hzOFpnKzlNbEYxaTNuUkVtCkNvZTJWVWx3WTJaeVhVZU0xN1pucy9XdWJaTlpIT2hUV3Q4ZHFqcmRnUEs2ck1ZSlFZRk5oYktPZFNJZUJsclMKeFRnSEk3d1ZuUXExSU8vRXpKbnMwc0x6MUJ3NDFoNFdBSDdteHNHbWtQQUhqcGNWNnpxaWlXcE0xd3d2cmMzNApxQ3ZVTGtId3hiaTE2WUVhQitDN1NlVnVHMmNwRTh3Z205ZENFMWNQQW9JQkFHUDVqUWZXN1JiU2xrNFd5WFoyCkpIQSs2OXpVM25QVUFwZmZYV3h2TC9QaHl2WUNuRlNadmpqZGRyUjRsSzhPRVdYTEtFMDVxaWJtbVJWMmFacloKZFA5R3A1UTZJVG9pM1lGakZnQzdmZlFNejYzT09MR3FjeTRIUTVOanZ5YUUzRGc4VlR1TUIyNU5ibVVqRUdldAo5NDhXNVBhcDB1WHFGRlZTb1lKU3lQVUlqZXE5SWlFOThqZ3A4RFZYS01hK0NWU0dneVRQcVgwcnF0VE52S2hFCnU0dUtrMVp5aFp1bVRSemlkRnhMbFZ2ZS9XdXl4ZC9rZXBLZTZkemVvRDRqODhQdS95M3Rta3hueDFXZCt3OHAKRCtwU05JN3BkQ2Q1L2pER0pkRmJqOU11M2xzTkJ6Rno2d2FYeE45QjAzYVhoT3BhaHNobkVpQVNzSDU3WlJTVgppUmtDZ2dFQUtPR1NibXkrUEl3dTA5WHI0a2RGWlZqc3F2S0NrZHBFTEVXWmhJT2pQanVVV1BvRXllL0xxR3NCCmFqd3pTdlFLQmU2U2xLeXZ4SVNsdmJmWktseG5JUW1SRHpoZ0x0SGo1dG1ZRDV4dHJmM1BtYUVmUlhMSy93bG8KNk1aRnczK21qaTNORmtPN0F5M3NoVzMwYnlKcDRyeHhrWUgvOHpnMjN3dnZNY2RXOHNBVXJlWW5BWkpSM1lxdQpoeS91aDlJajNDdXBSc1Q3Nyttb3o4YlZReEN0aGxFK2s0NG54ZWNqMUw0SzZuWEpnOHUxV3BPN0FNc1l3aXFKClJxVEJXeXV3UXR3QkxMUWxiQVBTTmlxaXZtb2pyTGdNZmdXam90SWFqYTRFV0RzcmlkaTRkcXVTb2Irc0tXRjIKYU1HYXRWRzQvbjNsdmphTDNNbGRuaENOWXB4TmdBPT0KLS0tLS1FTkQgUFJJVkFURSBLRVktLS0tLQo=

You can make the values from this secret available through a custom volume mount:

    apiVersion: apps.foundationdb.org/v1beta1
    kind: FoundationDBCluster
    metadata:
        name: sample-cluster
    spec:
      version: 6.2.20
      databaseConfiguration:
        storage: 5
      processes:
        general:
          podTemplate:
            spec:
              volumes:
                - name: fdb-certs
                  secret:
                    secretName: fdb-kubernetes-operator-secrets
              containers:
                - name: foundationdb
                  env:
                    - name: FDB_TLS_CERTIFICATE_FILE
                      value: /tmp/fdb-certs/tls.crt
                    - name: FDB_TLS_CA_FILE
                      value: /tmp/fdb-certs/tls.crt
                    - name: FDB_TLS_KEY_FILE
                      value: /tmp/fdb-certs/tls.key
                  volumeMounts:
                    - name: fdb-certs
                      mountPath: /tmp/fdb-certs

This will delete the pods in the cluster and recreate them with the new environment variables and volumes.

You can customize the same kind of fields on the sidecar container by adding them under the `sidecarContainer` section of the spec.

Note: The example above adds certificates to the environment, but it does not enable TLS for the cluster. We do not currently have a way to enable TLS once a cluster is running. If you set the `enableTls` flag on the container when you create the cluster, it will be created with TLS enabled. See the [example TLS cluster](../config/samples/cluster_local_tls.yaml) for more details on this configuration.

The `podTemplate` field allows you to customize nearly every part of the pods this cluster creates. There are some limitations on what you can configure:

* The pod will always have a container called `foundationdb`, a container called `foundationdb-kubernetes-sidecar`, and an init container called `foundationdb-kubernetes-init`. If you do not define containers with these names, the operator will add them. If you define containers with these names, the operator will modify them to add the necessary fields and default values.
* You cannot define a command or arguments for the `foundationdb` container.
* The image version for the built-in containers will be set by the operator. If you define a custom image, the operator will add a tag to the end with the image version the operator needs.
* You cannot directly set the affinity for the pod.
* The pod will always have volumes named `data`, `dynamic-conf`, `config-map`, and `fdb-trace-logs`, which will be defined by the operator. You cannot define custom volumes with these names.
* The `foundationdb` container will always have volume mounts with the names `data`, `dynamic-conf`, and `fdb-trace-logs`, which will be defined by the operator. You cannot define volume mounts with these names.
* The `foundationdb-kubernetes-sidecar` and `foundationdb-kubernetes-init` containers will always have volume mounts with the names `config-map` and `dynamic-conf`, which will be defined by the operator. You cannot define volume mounts with these names.
* The `foundationdb` container will always have environment variables with the names `FDB_CLUSTER_FILE` and `FDB_TLS_CA_FILE`. You can define custom values for these environment variables. If you do not define them, the operator will provide a value.
* The `foundationdb-kubernetes-sidecar` and `foundationdb-kubernetes-init` containers will always have environment variables with the names `SIDECAR_CONF_DIR`, `FDB_PUBLIC_IP`, `FDB_MACHINE_ID`, `FDB_ZONE_ID`, and `FDB_INSTANCE_ID`. You can define custom values for these environment variables. If you do not define them, the operator will provide a value.
* The `foundationdb-kubernetes-init` container will always have an environment variable with the names `COPY_ONCE`. You can define custom values for these environment variables. If you do not define them, the operator will provide a value.
* The `foundationdb-kubernetes-sidecar` container will always have environment variables with the names `FDB_TLS_VERIFY_PEERS` and `FDB_TLS_CA_FILE`. You can define custom values for these environment variables. If you do not define them, the operator will provide a value.
* The `foundationdb-kubernetes-sidecar` container will always have a readiness probe defined. If you do not define one, the operator will provide a default readiness probe.
* If you enable TLS for the Kubernetes sidecar, the operator will add `--tls` to the args for the `foundationdb-kubernetes-sidecar` container.
* The `foundationdb` container will always have resources requests and resource limits defined. If you do not define them yourself, the operator will provide them.

You should be careful when changing images, environment variables, commands, or arguments for the built-in containers. Your custom values may interfere with how the operator is using them. Even if you can make your usage work with the current version of the operator, subsequent releases of the operator may change its behavior in a way that introduces conflicts.

Other than the above, you can make any modifications to the pod definition you need to suit your requirements.

## Pod Update Strategy

When you need to update your pods in a way that requires recreating them, there are two strategies you can use.

The default strategy is to do a rolling bounce, where at most one fault domain is bounced at a time. While a pod is being recreated, it is unavailable, so this will degrade the availability fault tolerance for the cluster. The operator will ensure that pods are not deleted unless the cluster is at full fault tolerance, so if all goes well this will not create an availability loss for clients.

Deleting a pod may cause it to come back with a different IP address. If the process was serving as a coordinator, the coordinator will be considered unavailable when it comes back up. The operator will detect this condition after creating the new pod, and will change the coordinators automatically to ensure that we regain fault tolerance.

The other strategy you can use is to do a migration, where we replace all of the instances in the cluster. If you want to opt in to this strategy, you can set the field `updatePodsByReplacement` in the cluster spec to `true`. This strategy will temporarily use more resources, and requires moving all of the data to a new set of pods, but it will not degrade fault tolerance, and will require fewer recoveries and coordinator changes.

There are some changes that require a migration regardless of the value for the `updatePodsByReplacement` section. For instance, changing the volume size or any other part of the volume spec is always done through a migration.

# Controlling Fault Domains

The operator provides multiple options for defining fault domains for your cluster. The fault domain defines how data is replicated and how processes are distributed across machines. Choosing a fault domain is an important process of managing your deployments.

Fault domains are controlled through the `faultDomain` field in the cluster spec.

## Option 1: Single-Kubernetes Replication

The default fault domain strategy is to replicate across nodes in a single Kubernetes cluster. If you do not specify any fault domain option, we will replicate across nodes.


    apiVersion: apps.foundationdb.org/v1beta1
    kind: FoundationDBCluster
    metadata:
      name: sample-cluster
    spec:
      version: 6.2.20
      databaseConfiguration:
        storage: 5
      faultDomain:
        key: topology.kubernetes.io/zone # default: kubernetes.io/hostname
        valueFrom: spec.zoneName # default: spec.nodeName

The example above divides processes across nodes based on the label `topology.kubernetes.io/zone` on the node, and sets the zone locality information in FDB based on the field `spec.zoneName` on the pod. The latter field does not exist, so this configuration cannot work. There is no clear pattern in Kubernetes for allowing pods to access node information other than the host name, which presents challenges using any other kind of fault domain.

If you have some other mechanism to make this information available in your pod's environment, you can tell the operator to use an environment varilable as the source for the zone locality:


    apiVersion: apps.foundationdb.org/v1beta1
    kind: FoundationDBCluster
    metadata:
      name: sample-cluster
    spec:
      version: 6.2.20
      databaseConfiguration:
        storage: 5
      faultDomain:
        key: topology.kubernetes.io/zone
        valueFrom: $RACK

## Option 2: Multi-Kubernetes Replication

Our second strategy is to run multiple Kubernetes cluster, each as its own fault domain. This strategy adds significant operational complexity, but may allow you to have stronger fault domains and thus more reliable deployments. You can enable this strategy by using a special key in the fault domain:

    apiVersion: apps.foundationdb.org/v1beta1
    kind: FoundationDBCluster
    metadata:
      name: sample-cluster
    spec:
      version: 6.2.20
      instanceIDPrefix: zone2
      databaseConfiguration:
        storage: 5
      volumeSize: "128G"
      faultDomain:
        key: foundationdb.org/kubernetes-cluster
        value: zone2
        zoneIndex: 2
        zoneCount: 5

This tells the operator to use the value "zone2" as the fault domain for every process it creates. The zoneIndex and zoneCount tell the operator where this fault domain is within the list of Kubernetes clusters (KCs) you are using in this DC. This is used to divide processes across fault domains. For instance, this configuration has 7 stateless processes, which need to be divided across 5 fault domains. The zones with zoneIndex 1 and 2 will allocate 2 stateless processes each. The zones with zoneIndex 3, 4, and 5 will allocate 1 stateless process each.

When running across multiple KCs, you will need to apply more care in managing the configurations to make sure all the KCs converge on the same view of the desired configuration. You will likely need some kind of external, global system to store the canonical configuration and push it out to all of your KCs. You will also need to make sure that the different KCs are not fighting each other to control the database configuration. You can set different flags in `automationOptions` to control what the operator can change about the cluster. You can use these fields to designate a master instance of the operator which will handle things like reconfiguring the database.

You must always specify an `instanceIDPrefix` when deploying an FDB cluster to multiple Kubernetes clusters. You must set it to a different value in each Kubernetes cluster. This will prevent instance ID duplicates in the different Kubernetes clusters.

## Option 3: Fake Replication

In local test environments, you may not having any real fault domains to use, and may not care about availability. You can test in this environment while still having replication enabled by using fake fault domains:

    apiVersion: apps.foundationdb.org/v1beta1
    kind: FoundationDBCluster
    metadata:
      name: sample-cluster
    spec:
      version: 6.2.20
      databaseConfiguration:
        storage: 5
      automationOptions:
        configureDatabase: false
        killProcesses: false
      faultDomain:
        key: foundationdb.org/none

This strategy uses the pod name as the fault domain, which allows each process to act as a separate failure domain. Any hardware failure could lead to a complete loss of the cluster. This configuration should not be used in any production environment.

## Multi-Region Replication

The replication strategies above all describe how data is replicated within a data center. They control the `zoneid` field in the cluster's locality. If you want to run a cluster across multiple data centers, you can use FoundationDB's multi-region replication. This can work with any of the replication stragies above. The data center will be a separate fault domain from whatever you provide for the zone.


    apiVersion: apps.foundationdb.org/v1beta1
    kind: FoundationDBCluster
    metadata:
      name: sample-cluster
    spec:
      version: 6.2.20
      dataCenter: dc1
      processCounts:
        stateless: -1
      databaseConfiguration:
        storage: 5
        regions:
          - datacenters:
              - id: dc1
                priority: 1

The `dataCenter` field in the top level of the spec specifies what data center these instances are running in. This will be used to set the `dcid` locality field. The `regions` section of the database describes all of the available regions. See the [FoundationDB documentation](https://apple.github.io/foundationdb/configuration.html#configuring-regions) for more information on how to configure regions.

Replicating across data centers will likely mean running your cluster across multiple Kubernetes clusters, even if you are using a single-Kubernetes replication strategy within each DC. This will mean taking on the operational challenges described in the "Multi-Kubernetes Replication" section above.

# Using Multiple Namespaces

Our [sample deployment](https://raw.githubusercontent.com/foundationdb/fdb-kubernetes-operator/master/config/samples/deployment.yaml) configures the operator to run in single-namespace mode, where it only manages resources in the namespace where the operator itself is running. If you want a single deployment of the operator to manage your FDB clusters across all of your namespaces, you will need to run it in global mode. Which mode is appropriate will depend on the constraints of your environment.

## Single-Namespace Mode

To use single-namespace mode, set the `WATCH_NAMESPACE` environment variable for the controller to be the namespace where your FDB clusters will run. It does not have to be the same namespace where the operator is running, though this is generally the simplest way to configure it. When you are running in single-namespace mode, the controller will ignore any clusters you try to create in namespaces other than the one you give it.

The advantage of single-namespace mode is that it allows owners of different namespaces to run the operator themselves without needing access to other namespaces that may be managed by other tenants. The only cluster-level configuration it requires is the installation of the CRD. The disadvantage of single-namespace mode is that if you are running multiple namespaces for a single team, each namespace will need its own installation of the controller, which can make it more operationally challenging.


To run the controller in single-namespace mode, you will need to configure the following things:

* A service account for the controller
* The serviceAccountName field in the controller's pod spec
* A `WATCH_NAMESPACE` environment variable defined in the controller's pod spec
* A Role that grants access to the necessary permissions to all of the resources that the controller manages. See the [sample role](https://raw.githubusercontent.com/FoundationDB/fdb-kubernetes-operator/master/config/samples/deployment/rbac_role.yaml) for the list of those permissions.
* A RoleBinding that binds that role to the service account for the controller

The sample deployment provides all of this configuration.

## Global Mode

To use global mode, omit the `WATCH_NAMESPACE` environment variable for the controller. When you are running in global mode, the controller will watch for changes to FDB clusters in all namespaces, and will manage them all through a single instance of the controller.

The advantage of global mode is that you can easily add new namespaces without needing to run a new instance of the controller, which limits the per-namespace operational load. The disadvantage of global mode is that it requires the controller to have extensive access to all namespaces in the Kubernetes cluster. In a multi-tenant environment, this means the controller would have to be managed by the team that is adminstering your Kubernetes environment, which may create its own operational concerns.

To run the controller in global mode, you will need to configure the following things:

* A service account for the controller
* The serviceAccountName field in the controller's pod spec
* A ClusterRole that grants access to the necessary permissions to all of the resources that the controller manages. See the [sample role](https://raw.githubusercontent.com/FoundationDB/fdb-kubernetes-operator/master/config/samples/deployment/rbac_role.yaml) for the list of those permissions.
* A ClusterRoleBinding that binds that role to the service account for the controller

You can build this kind of configuration easily from the sample deployment by changing the following things:

* Delete the configuration for the `WATCH_NAMESPACE` variable
* Change the Roles to ClusterRoles
* Change the RoleBindings to ClusterRoleBindings

# Choosing Your Public IP Source

The default behavior of the operator is to use the IP assigned to the pod as the public IP for FoundationDB. This is not the right choice for some environments, so you may need to consider an alternative approach.

## Pod IPs

You can choose this option by setting `spec.services.publicIPSource=pod`. This is currently the default selection.

In this mode, we use the pod's IP as both the listen address and the public address. We will not create any services for the pods.

Using pod IPs can present several challenges:

* Deleting and recreating a pod will lead to the IP changing. If the process is a coordinator, this can only be recovered by changing coordinators, which requires that a majority of the old coordinators still be functioning on their original IP.
* Pod IPs may not be routable from outside the Kubernetes cluster.
* Pods that are failing to schedule will not have IP addresses. This prevents us from excluding them, requiring manual safety checks before removing the pod.

## Service IPs

You can choose this option by setting `spec.services.publicIPSource=service`. This feature is new, and still experimental, but we plan to make it the default in the future.

In this mode, we create one service for each pod, and use that service's IP as the public IP for the pod. The pod IP will still be used as the listen address. This ensures that IPs stay fixed even when pods get rescheduled, which reduces the need for changing coordinators and protects against some unrecoverable failure modes.

Using service IPs presents its own challenges:

* In some networking configurations, pods may not be able to access service IPs that route to the pod. See the section on hairpin mode in the [Kubernetes Docs](https://kubernetes.io/docs/tasks/debug-application-cluster/debug-service/#a-pod-fails-to-reach-itself-via-the-service-ip) for more information.
* Creating one service for each pod may cause performance problems for the Kubernetes cluster
* We currently only support services with the ClusterIP type. These IPs may not be routable from outside the Kubernetes cluster.
* The Service IP space is often more limited than the pod IP space, which could cause you to run out of service IPs.

# Renaming a Cluster

The name of a cluster is immutable, and it is included in the names of all of the dependent resources, as well as in labels on the resources. If you want to change the name later on, you can do so with the following steps. This example assumes you are renaming the cluster `sample-cluster` to `sample-cluster-2`.

1.  Create a new cluster named `sample-cluster-2`, using the current connection string from `sample-cluster` as its seedConnectionString. You will need to set an `instanceIdPrefix` for `sample-cluster-2` to be different from the `instanceIdPrefix` for `sample-cluster`, to make sure that the instance IDs do not collide. The rest of the spec can match `sample-cluster`, other than any fields that you want to customize based on the new name. Wait for reconciliation on `sample-cluster-2` to complete.
2.  Update the spec for `sample-cluster` to set the process counts for `log`, `stateless`, and `storage` to `-1`. You should omit all other process counts. Wait for the reconciliation for `sample-cluster` to complete.
3.  Check that none of the original pods from `sample-cluster` are running.
4.  Delete the `sample-cluster` resource.

At that point, you will be left with just the resources for `sample-cluster-2`. You can continue performing operations on `sample-cluster-2` as normal. You can also change or remove the `instanceIDPrefix` if you had to set it to a different value earlier in the process.

# Coordinating Global Operations

When running a FoundationDB cluster that is deployed across multiple Kubernetes clusters, each Kubernetes cluster will have its own instance of the operator working on the processes in its cluster. There will be some operations that cannot be scoped to a single Kubernetes cluster, such as changing the database configuration. The operator provides a locking system to ensure that only one instance of the operator can perform these operations at a time. You can enable this locking system by setting `lockOptions.disableLocks = false` in the cluster spec. The locking system is automatically enabled by default for any cluster that has multiple regions in its database configuration, or a `zoneCount` greater than 1 in its fault domain configuration.

The locking system uses the `instanceIDPrefix` from the cluster spec to identify an instance of the operator. Make sure to set this to a unique value for each Kubernetes cluster, both to support the locking system and to prevent duplicate instance IDs.

This locking system uses the FoundationDB cluster as its data source. This means that if the cluster is unavailable, no instance of the operator will be able to get a lock. If you hit a case where this becomes an issue, you can disable the locking system by setting `lockOptions.disableLocks = true` in the cluster spec.

In most cases, restarts will be done independently in each Kubernetes cluster, and the locking system will be used to ensure a minimum time between the different restarts and avoid multiple recoveries in a short span of time. During upgrades, however, all instances must be restarted at the same time. The operator will use the locking system to coordinate this. Each instance of the operator will store records indicating what processes it is managing and what version they will be running after the restart. Each instance will then try to acquire a lock and confirm that every process reporting to the cluster is ready for the upgrade. If all processes are prepared, the operator will restart all of them at once. If any instance of the operator is stuck and unable to prepare its processes for the upgrade, the restart will not occur.

## Deny List

There are some situations where an instance of the operator is able to get locks but should not be trusted to perform global actions. For instance, the operator could be partitioned in a way where it cannot access the Kubernetes API but can access the FoundationDB cluster. To block such an instance from taking locks, you can add it to the `denyList` in the lock options. You can set this in the cluster spec on any Kubernetes cluster.

    apiVersion: apps.foundationdb.org/v1beta1
    kind: FoundationDBCluster
    metadata:
      name: sample-cluster
    spec:
      instanceIDPrefix: dc1
      lockOptions:
        denyList:
          - id: dc2

This will clear any locks held by `dc2`, and prevent it from taking further locks. In order to clear this deny list, you must change it to allow that instance again:

    apiVersion: apps.foundationdb.org/v1beta1
    kind: FoundationDBCluster
    metadata:
      name: sample-cluster
    spec:
      instanceIDPrefix: dc1
      lockOptions:
        denyList:
          - id: dc2
            allow: true

Once that change is fully reconciled, you can clear the deny list from the spec.
