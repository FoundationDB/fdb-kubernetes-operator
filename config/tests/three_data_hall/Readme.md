# Three-Data-hall example

This example requires that your Kubernetes cluster has nodes which are labeled with `topology.kubernetes.io/zone`.
The example requires at least 3 unique zones, those can be faked for testing, by adding the labels to a node.
If you want to use cloud provider specific zone label values you can set the `AZ1`, `AZ2` and `AZ3` environment variables.

## Create the Three-Data-Hall cluster

This will bring up a FDB cluster using the three-data-hall redundancy mode.

```bash
./create.bash
```

## Delete

This will remove all created resources:

```bash
./delete.bash
```
