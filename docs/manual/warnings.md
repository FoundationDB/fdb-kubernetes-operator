# Warnings

This operator aims to support as many different environments and use cases as we can, by offering a variety of customization options. You may find that the stock configurations don't work as desired in your environment. We recommend reading through this entire manual in order to get a feel for how the operator works. There are a few sections that are most likely to be relevant before you create a cluster: [Customizing Your Pods](customizing.md#customizing-your-pods), [Controlling Fault Domains](fault_domains.md), and [Using Multiple Namespaces](customizing.md#using-multiple-namespaces).

## Risks when IPs Change

FoundationDB's service discovery works through a cluster file, which contains a list of coordinator IPs. All processes in the cluster, and all client processes, start up by talking to the coordinators. If the pods that are serving as coordinators get deleted, the operator will recreate them, but they will come up with different IP addresses. If this happens to a majority of the coordinators, the database will not be available, and there is no established procedure for recovering from this. 

One way to mitigate this risk is by using service IPs rather than pod IPs as the public IP for your processes. There is more discussion on how to do this in the section on [Choosing your Public IP Source](customization.md#choosing-your-public-ip-source). You may also want to look into using [Pod Disruption Budgets](fault_domains.md#managing-disruption).

## Securing Connections

The operator runs clusters with insecure connections by default, so the clusters are only as secure as your network and your ACLs allow it to be. We also support using [TLS](tls.md) to secure connections, but this will require additional configuration management.

## Resource Requirements

In the interest of giving everyone a good configuration out of the box, the operator applies resource requirements to the built-in containers. The main foundationdb container is configured with 1 CPU and 1 Gi of memory as its requests. It is also configured to use the same values for limits and requests. You can change this behavior by specifying your own resource values in the pod template. If you want your container to have no values set for the CPU or memory, and use whatever values are set by default in your Kubernetes environments, you can accomplish this by specifying a resource request for `org.foundationdb/empty: 0`. This resource constraint will have no direct effect, but it will ensure that the resource object has a non-empty value, and the operator will then pass that on to the container spec.

The operator also provides a default size of 128 Gi for the volumes your cluster will use. You can customize this in the volume claim template.

## Limitations on Pod Customization

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

## Next

You can continue on to the [next section](resources.md) or go back to the [table of contents](index.md).
