# Three-Data-hall example

This example requires that your Kubernetes cluster has nodes which are labeled with `topology.kubernetes.io/zone`.
The example requires at least 3 unique zones, those can be faked for testing, by adding the labels to a node.

## Create the RBAC settings

This example uses the unified image to read data from the Kubernetes API and therefore we have to
create the according RBAC setup. If you use a different namespace than the default namespace, you have
to adjust the `fdb-kubernetes` `ClusterRoleBinding` to point to the right `ServiceAccount`.

```bash
kubectl apply -f ./config/tests/three_data_hall/unified_image_role.yaml
```


## Create the Three-Data-Hall cluster

This will bring up the three data hall cluster managed by a single `FoundationDBCluster` resource:

```bash
kubectl apply -f ./config/tests/three_data_hall/cluster.yaml
```

## Delete

This will remove all created resources:

```bash
kubectl delete -f ./config/tests/three_data_hall/cluster.yaml
```

## Migration to a Three-Data-Hall cluster

See [Fault domains](../../../docs/manual/fault_domains.md)
