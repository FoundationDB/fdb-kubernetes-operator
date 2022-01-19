# Customization

This document covers some of the options the operator provides for customizing your FoundationDB deployment.

Many of these customizations involve the `processes` field in the cluster spec, which we will refer to as the "process settings". This field is a dictionary, mapping a process class to a process settings object. This also supports a special key called `general` which is applied to all process classes. If a value is specified for a specific process class, the `general` value will be ignored. These values are merged at the top level of the process settings object. If you specify a `volumeClaimTemplate` object in the `storage` settings and a `podTemplate` object in the `general` settings, the storage processes will use both the custom `volumeClaimTemplate` and the general `podTemplate`. If you specify a `podTemplate` object in the `storage` settings and `podTemplate` object in the `general` settings, the storage processes will only use the values given in the `storage` settings, and will the pod template from the `general` settings completely.

## Running Multiple Storage Servers per Pod

Since FoundationDB is limited to a single core it can make sense to run multiple storage server per disk. You can change the number of storage server per Pod with the `storageServersPerPod` setting. This will start multiple FDB processes inside of a single container, under a single `fdbmonitor` process.

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.30
  spec:
    storageServersPerPod: 2
```

A change to the `storageServersPerPod` will replace all of the storage pods. For more information about this feature read the [multiple storage servers per pod](/docs/design/multiple_storage_per_disk.md) design doc.

## Customizing the Volumes

To use a different `StorageClass` than the default you can set your desired `StorageClass` in the [process settings](/docs/cluster_spec.md#processsettings):

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.30
  processes:
    general:
      volumeClaimTemplate:
        spec:
          storageClassName: my-storage-class
```

You can use the same field to customize other parts of the volume claim. For instance, this is how you would define the storage size for your volumes:

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.30
  processes:
    general:
      volumeClaimTemplate:
        spec:
          resources:
            requests:
              storage: "256G"
```

A change to the volume claim template will replace all PVC' and the according Pods. You can also use different volume settings for different processes. For instance, you could use a slower but higher-capacity storage class for your storage processes:

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.30
  processes:
    log:
      volumeClaimTemplate:
        spec:
          storageClassName: fast-storage
    storage:
      volumeClaimTemplate:
        spec:
          storageClassName: slow-storage
```

## Customizing Your Pods

The process settings in the cluster spec also allow specifying a pod template, which allows customizing almost everything about your pods. You can define custom environment variables, add your own containers, add additional volumes, and more. You may want to use these fields to handle things that are specific to your environment, like forwarding logs to a central system. In the example below, we add custom resource requirements and a custom container for logging. This new container is making use of the `fdb-trace-logs` volume, which is defined by the operator automatically.

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
    name: sample-cluster
spec:
  version: 6.2.30
  processes:
    general:
      podTemplate:
        spec:
          containers:
            - name: foundationdb
              resources:
                requests:
                  cpu: 1
                  memory: 8Gi
                limits:
                  cpu: 2
                  memory: 8Gi
            - name: log-forwarder
              image: example/log-forwarder
              args:
                - "--log-dir"
                - "/var/log/fdb-trace-logs"
              volumeMounts:
                - name: fdb-trace-logs
                  mountPath: /var/log/fdb-trace-logs
```

## Customizing the FoundationDB Image

If you want to use custom builds of the FoundationDB images, you can specify
that in the `imageConfigs` in the container settings. The example below shows
how to specify a custom base image for both the main container and the sidecar
container.

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
    name: sample-cluster
spec:
  version: 6.2.30
  mainContainer:
    imageConfigs:
      - baseImage: docker.example/foundationdb
  sidecarContainer:
    imageConfigs:
      - baseImage: docker.example/foundationdb-kubernetes-sidecar
```

This will produce pods where the `foundationdb` container runs the image `docker.example/foundationdb:6.2.30`, and the `foundationdb-kubernetes-sidecar` container runs the image `docker.example/foundationdb-kubernetes-sidecar:6.2.30-1`.

The image configs also allow specifying a specific tag for the images.

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
    name: sample-cluster
spec:
  version: 6.2.30
  mainContainer:
    imageConfigs:
      - baseImage: docker.example/foundationdb
        tag: "build-20210711161700"
```

This will produce pods where the `foundationdb` container runs the image `docker.example/foundationdb:build-20210711161700`.

You can also specify multiple image configs, and customize image configs separately for different FoundationDB versions. The operator will determine each field in the image config by looking for the first entry that specifies that field and is applicable for the current version.


```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
    name: sample-cluster
spec:
  version: 6.2.30
  mainContainer:
    imageConfigs:
      - baseImage: docker.example/foundationdb
      - version: 6.2.30
        tag: "build-20210711161700"
      - version: 6.3.0
        tag: "build-20210712161700"
```

This tells the operator to use the base image `docker.example/foundationdb` for all versions, and to use different tags for versions 6.2.30 and versions 6.3.0. Applying this config will produce pods where the `foundationdb` container runs the image `docker.example/foundationdb:build-20210711161700`. If you upgrade the cluster to version 6.3.0, it will run the image `docker.example.com/build-20210712161700`.

You can also specify a suffix for the tag, which tells the operator to append that suffix to the version the cluster is running.

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
    name: sample-cluster
spec:
  version: 6.2.30
  mainContainer:
    imageConfigs:
      - baseImage: docker.example/foundationdb
        tagSuffix: slim
```

This will produce pods where the `foundationdb` container runs the image `docker.example/foundationdb:6.2.30-slim`. If you upgrade the cluster to version 6.3.0, it will run the image `docker.example/foundationdb:6.3.0-slim`.

The operator uses a default tag suffix of `-1` for the sidecar container. If you provide a custom tag suffix for the sidecar container, your custom suffix will take precedence.

## Pod Update Strategy

When you need to update your pods in a way that requires recreating them, there are two strategies you can use.

The default strategy is to do a rolling bounce, where at most one fault domain is bounced at a time.
While a pod is being recreated, it is unavailable, so this will degrade the fault tolerance for the cluster.
The operator will ensure that pods are not deleted unless the cluster is at full fault tolerance, so if all goes well this will not create an availability loss for clients.

Deleting a pod may cause it to come back with a different IP address.
If the process was serving as a coordinator, the coordinator will still be considered unavailable after the replaced pod starts.
The operator will detect this condition, and will change the coordinators automatically to ensure that we regain fault tolerance.

The other strategy you can use is to do a migration, where we replace all process groups in the cluster.
If you want to opt in to this strategy, you can set the field `updatePodsByReplacement` in the cluster spec to `true`.
This strategy will temporarily use more resources, and requires moving the data to a new set of pods, but it will not degrade fault tolerance, and will require fewer recoveries and coordinator changes.

There are some changes that require a migration regardless of the value for the `updatePodsByReplacement` section.
For instance, changing the volume size or any other part of the volume spec is always done through a migration.

## Choosing Your Public IP Source

The default behavior of the operator is to use the IP assigned to the pod as the public IP for FoundationDB.
This is not the right choice for some environments, so you may need to consider an alternative approach.

### Pod IPs

You can choose this option by setting `spec.routing.publicIPSource=pod`. This is currently the default selection.

In this mode, we use the pod's IP as both the listen address and the public address. We will not create any services for the pods.

Using pod IPs can present several challenges:

* Deleting and recreating a pod will lead to the IP changing. If the process is a coordinator, this can only be recovered by changing coordinators, which requires that a majority of the old coordinators still be functioning on their original IP.
* Pod IPs may not be routable from outside the Kubernetes cluster.
* Pods that are failing to schedule will not have IP addresses. This prevents us from excluding them, requiring manual safety checks before removing the pod.

### Service IPs

You can choose this option by setting `spec.routing.publicIPSource=service`. This feature is new, and still experimental, but we plan to make it the default in the future.

In this mode, we create one service for each pod, and use that service's IP as the public IP for the pod. The pod IP will still be used as the listen address. This ensures that IPs stay fixed even when pods get rescheduled, which reduces the need for changing coordinators and protects against some unrecoverable failure modes.

Using service IPs presents its own challenges:

* In some networking configurations, pods may not be able to access service IPs that route to the pod. See the section on hairpin mode in the [Kubernetes Docs](https://kubernetes.io/docs/tasks/debug-application-cluster/debug-service/#a-pod-fails-to-reach-itself-via-the-service-ip) for more information.
* Creating one service for each pod may cause performance problems for the Kubernetes cluster
* We currently only support services with the ClusterIP type. These IPs may not be routable from outside the Kubernetes cluster.
* The Service IP space is often more limited than the pod IP space, which could cause you to run out of service IPs.

## Using Multiple Namespaces

Our [sample deployment](https://raw.githubusercontent.com/foundationdb/fdb-kubernetes-operator/master/config/samples/deployment.yaml) configures the operator to run in single-namespace mode, where it only manages resources in the namespace where the operator itself is running. If you want a single deployment of the operator to manage your FDB clusters across all of your namespaces, you will need to run it in global mode. Which mode is appropriate will depend on the constraints of your environment.

### Single-Namespace Mode

To use single-namespace mode, set the `WATCH_NAMESPACE` environment variable or the command-line option `-watch-namespace` for the controller to be the namespace where your FDB clusters will run. It does not have to be the same namespace where the operator is running, though this is generally the simplest way to configure it. When you are running in single-namespace mode, the controller will ignore any clusters you try to create in namespaces other than the one you give it. If both options are defined, the environment variable will take precedence.

The advantage of single-namespace mode is that it allows owners of different namespaces to run the operator themselves without needing access to other namespaces that may be managed by other tenants. The only cluster-level configuration it requires is the installation of the CRD. The disadvantage of single-namespace mode is that if you are running multiple namespaces for a single team, each namespace will need its own installation of the controller, which can make it more operationally challenging.

To run the controller in single-namespace mode, you will need to configure the following things:

* A service account for the controller
* The serviceAccountName field in the controller's pod spec
* A `WATCH_NAMESPACE` environment variable defined in the controller's pod spec or in the arguments of the container command
* A Role that grants access to the necessary permissions to all of the resources that the controller manages. See the [sample role](https://raw.githubusercontent.com/FoundationDB/fdb-kubernetes-operator/master/config/samples/deployment/rbac_role.yaml) for the list of those permissions.
* A RoleBinding that binds that role to the service account for the controller

The sample deployment provides all of this configuration.

### Global Mode

To use global mode, omit the `WATCH_NAMESPACE` environment variable or the `-watch-namespace` command line flag for the controller. When you are running in global mode, the controller will watch for changes to FDB clusters in all namespaces, and will manage them all through a single instance of the controller.

The advantage of global mode is that you can easily add new namespaces without needing to run a new instance of the controller, which limits the per-namespace operational load. The disadvantage of global mode is that it requires the controller to have extensive access to all namespaces in the Kubernetes cluster. In a multi-tenant environment, this means the controller would have to be managed by the team that is adminstering your Kubernetes environment, which may create its own operational concerns.

To run the controller in global mode, you will need to configure the following things:

* A service account for the controller
* The serviceAccountName field in the controller's pod spec
* A ClusterRole that grants access to the necessary permissions to all of the resources that the controller manages. See the [sample role](https://raw.githubusercontent.com/FoundationDB/fdb-kubernetes-operator/master/config/samples/deployment/rbac_role.yaml) for the list of those permissions.
* A ClusterRoleBinding that binds that role to the service account for the controller

You can build this kind of configuration easily from the sample deployment by changing the following things:

* Delete the configuration for the `WATCH_NAMESPACE` variable or remove the command line option `-watch-namespace`.
* Change the Roles to ClusterRoles
* Change the RoleBindings to ClusterRoleBindings

## Resource Labeling

The operator has default labels that it applies to all resources it manages in order to track those resources. You can customize this labeling through the label config in the cluster spec.

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
    name: sample-cluster
spec:
  version: 6.2.30
  labels:
    # The default match labels are {"fdb-cluster-name": "sample-cluster"}
    matchLabels:
      my-cluster: sample-cluster
```

These match labels will be automatically applied to new resources, and will be used to fetch those resources when the operator runs list commands. The match labels are also used as part of the selector in services that the operator creates and in the default affinity rules.

You must be careful when changing match labels. Most resources in Kubernetes cannot support two different kinds of match labels at once. If you want to change match labels you will need to go through a four step process: first add both sets of labels to all resources, then update the match labels to exclusively use the new labels, then remove the old labels from the spec, then remove the labels from the resources. Between each of these steps you must wait for reconciliation to complete.

Let's say you want the new match labels to be `{"this-cluster": "sample-cluster"}

Add both sets of labels to all resources:

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.30
  labels:
    matchLabels:
      my-cluster: sample-cluster
    # resourceLabels gives additional labels that we put on resources. Putting
    # the same label in both matchLabels and resourceLabels is redundant, but we
    # are putting both here to make the behavior more clear, and so that we only
    # change one field in each step.
    resourceLabels:
      my-cluster: sample-cluster
      this-cluster: sample-cluster
```

Update the match labels:

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.30
  labels:
    matchLabels:
      this-cluster: sample-cluster
    resourceLabels:
      my-cluster: sample-cluster
      this-cluster: sample-cluster
```

Remove the old labels from the spec:

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.30
  labels:
    matchLabels:
      this-cluster: sample-cluster
```

Remove the old labels from existing resources:

```bash
kubectl label pod,pvc,configmap,service -l this-cluster=sample-cluster my-cluster-
```

### Per-Resource Labels

The operator also sets labels on pods and PVCs indicating the process class and process group ID associated with the pod. You can customize the labels used for this through the label config in the cluster spec:

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.30
  labels:
    processClassLabels:
      # Default: ["fdb-process-class", "foundationdb.org/fdb-process-class"]
      - my-class
    processGroupIDLabels:
      # Default: ["fdb-instance-id", "foundationdb.org/fdb-process-group-id"]
      - my-instance
```

You can provide multiple label names in these fields. The first entry in the list will be the label that is used when listing resources in the operator. In order to change the labels used, you will have to follow a multi-step process. The examples below will focus on changing the process class label, but the same series of steps will work for changing the process group ID labels.

First, you will need to add the new label to all of the resources:

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
    name: sample-cluster
spec:
  version: 6.2.30
  labels:
    processClassLabels:
      - my-class
      - this-class
```

Once this change is reconciled and the new label is applied, you can remove the old label from the spec:

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.30
  labels:
    processClassLabels:
      - this-class
```

This will tell the operator to stop applying or using the labels, but existing resources will still have the old labels. Then you can remove the old labels from existing resources:

```bash
kubectl label pod,pvc,configmap,service -l foundationdb.org/fdb-cluster-name=sample-cluster my-class-
```

## Unified vs Split Images

The operator currently supports two different image types: a split image and a unified image. The split image provides two different images for the `foundationdb` container and the `foundationdb-kubernetes-sidecar` container. The unified image provides a single image which handles launching `fdbserver` processes as well as providing feedback to the operator on locality information and updates to dynamic conf.

**NOTE**: The unified image is still experimental, and is not recommended outside of development environments.

The default behavior in the operator is to use the split image. To switch to the unified image, set the flag in the cluster spec as follows:

```yaml
apiVersion: apps.foundationdb.org/v1beta1
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 6.2.30
  useUnifiedImage: true
```

For more information on how the interaction between the operator and these images works, see the [technical design](technical_design.md#interaction-between-the-operator-and-the-pods).

## Next

You can continue on to the [next section](replacements_and_deletions.md) or go back to the [table of contents](index.md).
