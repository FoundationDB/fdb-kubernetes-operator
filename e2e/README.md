# e2e tests for FDB Kubernetes operator

Those test must be running on a Kubernetes cluster that has `amd64` Linux nodes as FoundationDB currently has no builds for `arm64`.
Every test suite has a head in the `*_test.go` file that describes the test cases and the targeted scenarios.

## Running the e2e tests

The following command will run all the operator related tests with the default values:

```bash
make -C e2e -kj run
```

You can also run a specific test suite by providing the name of the test suite:

```bash
make -C e2e -kj test_operator.run
```

Every test suite will create at least one namespace, HA cluster tests will create all the required namespaces.

### StorageClass selection

The e2e tests assume that at least one `StorageClass` is present in the target Kubernetes cluster.
You can provide the targeted `StorageClass` as an environment variable:

```bash
STORAGE_CLASS='my-fancy-storage' make -C e2e -kj test_operator.run
```

If the `STORAGE_CLASS` is not set, the operator will take the default `StorageClass` in this cluster.
The default `StorageClass` will be identified based on the annotation: `"storageclass.kubernetes.io/is-default-class=true`.

The e2e test suite has some tests, that will test a migration from one `StorageClass` to another.
To prevent potential issues, the e2e test suite will only select `StorageClasses` that have the label `foundationdb.org/operator-testing=true`.
If the test suite is not able to get at least 2 different `StorageClasses` the migration test will be skipped.

### Customize the e2e test runs

The `Makefile` provides different options to customize a test run, e.g. with `FDB_VERSION` a user can specify the used FDB version for a test run:

```bash
FDB_VERSION-7.1.29 make -C e2e -kj test_operator.run
```

If those tests are running on a cluster that has no chaos-mesh installed, you can set `ENABLE_CHAOS_TESTS=false` to disable all test that uses chaos-mesh.

### Running e2e tests in kind

[kind](https://kind.sigs.k8s.io) provides an easy way to run a local Kubernetes cluster.
The following steps assume that `kind` is already installed.

```bash
make -C e2e kind-setup
```

This will call the [setup_e2e.sh](./scripts/setup_e2e.sh) script to setup `kind` and install chaos-mesh.
After testing you can run the following command to remove the kind cluster:

```bash
make -C e2e kind-destroy
```

If you want to iterate over different builds of the operator, you don't have to recreate the kind cluster multiple times.
You just can rebuild the operator image and push the new image inside the kind cluster:

```bash
make -C e2e kind-update-operator
```
