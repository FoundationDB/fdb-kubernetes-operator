# Warnings

This operator aims to support many different environments and use cases, by offering a variety of customization options.
You may find that the stock configurations don't work as desired in your environment.
We recommend reading through this entire manual in order to get a feel for how the operator works.
There are a few sections that are most likely to be relevant before you create a cluster:

- [Customizing Your Pods](customizing.md#customizing-your-pods)
- [Controlling Fault Domains](fault_domains.md)
- [Using Multiple Namespaces](customizing.md#using-multiple-namespaces).

## Securing Connections

The operator runs clusters with insecure connections by default, so the clusters are only as secure as your network and your ACLs allow it to be.
We also support using [TLS](tls.md) to secure connections, but this requires additional configuration management.

## Resource Requirements

In the interest of giving everyone a good configuration out of the box, the operator applies resource requirements to the built-in containers.
The main `foundationdb` container is configured with 1 CPU and 8 GiB of memory as its requests, based on the [official docs](https://apple.github.io/foundationdb/configuration.html#system-requirements).
It is also configured to use the same values for limits and requests.
You can change this behavior by specifying your own resource values in the pod template.
If you want your container to have no values set for the CPU or memory, and use whatever values are set by default in your Kubernetes environments, you can accomplish this by specifying a resource request for `org.foundationdb/empty: 0`.
This resource constraint will have no direct effect, but it will ensure that the resource object has a non-empty value, and the operator will then pass that on to the container spec.

The operator also provides a default size of 128 GiB for the volumes your cluster will use. You can customize this in the volume claim template.

## Limitations on Pod Customization

The `podTemplate` field allows you to customize nearly every part of the pods this cluster creates. There are some limitations on what you can configure:

* The pod will always have a container called `foundationdb`, a container called `foundationdb-kubernetes-sidecar`, and for the [split image](./technical_design.md#split-image) an init container called `foundationdb-kubernetes-init`. If you do not define containers with these names, the operator will add them. If you define containers with these names, the operator will modify them to add the necessary fields and default values.
* You cannot define a command or arguments for the `foundationdb` container.
* The image version for the built-in containers will be set by the operator. If you define a custom image, the operator will add a tag to the end with the image version the operator needs.
* You can set affinities on the Pod level, but depending on the `fault domain key` the operator will add at least one `PodAntiAffinity` to try to spread the Pods across multiple failure domains.
* The pod will always have volumes named `data`, `dynamic-conf`, `config-map`, and `fdb-trace-logs`, which will be defined by the operator. You cannot define custom volumes with these names.
* The `foundationdb` container will always have volume mounts with the names `data`, `dynamic-conf`, and `fdb-trace-logs`, which will be defined by the operator. You cannot define volume mounts with these names.
* The `foundationdb-kubernetes-sidecar` and `foundationdb-kubernetes-init` containers will always have volume mounts with the names `config-map` and `dynamic-conf`, which will be defined by the operator. You cannot define volume mounts with these names.
* The `foundationdb` container will always have environment variables with the names `FDB_CLUSTER_FILE` and `FDB_TLS_CA_FILE`. You can define custom values for these environment variables. If you do not define them, the operator will provide a value.
* The `foundationdb-kubernetes-sidecar` and `foundationdb-kubernetes-init` containers will always have environment variables with the names `FDB_PUBLIC_IP`, `FDB_MACHINE_ID`, `FDB_ZONE_ID`, and `FDB_INSTANCE_ID`. You can define custom values for these environment variables. If you do not define them, the operator will provide a value.
* The `foundationdb-kubernetes-sidecar` container will always have environment variables with the names `FDB_TLS_VERIFY_PEERS` and `FDB_TLS_CA_FILE`. You can define custom values for these environment variables. If you do not define them, the operator will provide a value.
* If you specify the environment variable `ADDITIONAL_ENV_FILE` for the `foundationdb-kubernetes-sidecar` and `foundationdb-kubernetes-init` containers, its content will be sourced before any container command runs, and you can override or define there any other environment variable; this can be used for example to inject environment variables using a shared volume.
* The `foundationdb-kubernetes-sidecar` container will always have a readiness probe defined. If you do not define one, the operator will provide a default readiness probe.
* If you enable TLS for the Kubernetes sidecar, the operator will add `--tls` to the args for the `foundationdb-kubernetes-sidecar` container.
* The `foundationdb` container will always have resources requests and resource limits defined. If you do not define them yourself, the operator will provide them.

You should be careful when changing images, environment variables, commands, or arguments for the built-in containers. Your custom values may interfere with how the operator is using them. Even if you can make your usage work with the current version of the operator, subsequent releases of the operator may change its behavior in a way that introduces conflicts.

Other than the above, you can make any modifications to the pod definition you need to suit your requirements.

## Limitations on FDB Port Customization

The operator doesn't support to use custom ports for FDB.
Per default a FDB cluster without TLS will use the ports `4501` and a FDB cluster using TLS will be using `4500`.
If more than one process should be running per Pod, e.g. when using the `storageServersPerPod` setting, the additional processes will get the `standard port + 2*processNumber`, e.g. for the second process in a TLS cluster that would be `4502`.

## FDB version upgrades

It's not recommended to perform additional changes during an FDB version upgrade.
This can cause side effects that might cause some downtime or will interfere with the upgrade process.
You should ensure that the cluster is fully reconciled before starting the upgrade, and you should make sure that the Pod spec is not changed during an upgrade.
Once the cluster is fully reconciled after the upgrade you can change the pod configuration again.

## Next

You can continue on to the [next section](resources.md) or go back to the [table of contents](index.md).
