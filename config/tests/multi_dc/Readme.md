# Multi DC example

Ensure that you ran the `setup_e2e.sh` script and setup the environment variables for the clusters.
This setup is currently meant to run manually. In the future this will be rewritten to be a real e2e test.

## Create the multi-dc cluster

This will bringup a FDB cluster running across 3 different Kubernetes clusters in a multi-region configuration.

```bash
./create.bash
```

## Delete

This will remove all created resources:

```bash
./bash.bash
```
