# e2e tests for FDB Kubernetes operator

Those test must be running on a Kubernetes cluster that has `amd64` Linux nodes as FoundationDB currently has no builds for `arm64`.
Every test suite has a head in the `*_test.go` file that describes the test cases and the targeted scenarios.

## Setup

The e2e tests expect certain customizations in the Kubernetes cluster. To deploy those in the cluster, run:

```bash
make install
```

This is only necessary once.

To cover storage class migrations in tests, annotate your cluster's storage classes as follows. Alternatively, you can skip those tests and provide the default storage class (see "StorageClass selection" below).  

* Annotate the `StorageClass` to use for most tests with the label: `"storageclass.kubernetes.io/is-default-class=true`.
* Annotate at least two `StorageClasses` with the label `foundationdb.org/operator-testing=true`.  If the test suite is not able to get at least 2 different `StorageClasses` the migration test will be skipped.


## Running the e2e tests

The following command will run all the operator related tests with the default values:

```bash
make -kj -C e2e run
```

You can also run a specific test suite by providing the name of the test suite:

```bash
make -C e2e test_operator.run
```

Every test suite will create at least one namespace, HA cluster tests will create all the required namespaces.

### Running against a custom operator version

By default, the tests will test the "latest" official version (`docker.io/fdb-kubernetes-operator:latest`). To test
against a custom-built operator, there are three approaches:

#### Specify the operator container image

You can specify which container image to use via environment variable:

```bash
OPERATOR_IMAGE=fdb-kubernetes-operator:v1.54.0
```

#### Use a private container registry

You can specify a different container registry than `docker.io`:

```bash
REGISTRY=12345.dkr.ecr.us-east-1.amazonaws.com
```

The e2e test will now read all images from there, so make sure to have all the necessary FoundationDB containers
in this registry, too.

#### Specify registry per container

It is possible to use different registries for the operator and system under test by including the registry in the container image specification, e.g.:

```bash
REGISTRY=
OPERATOR_IMAGE=12345.dkr.ecr.us-east-1.amazonaws.com/fdb-kubernetes-operator:latest
UNIFIED_FDB_IMAGE=docker.io/foundationdb/fdb-kubernetes-monitor
```

### Reusing an existing test cluster

A test cluster can be reused if wanted, e.g. for test cases that load a large amount of data into the cluster.
This requires to specify the `CLUSTER_NAME` and the `NAMESPACE`, if those values are not specified the test suite will use randomized values.
_NOTE_ The limitation of this approach is that the test suite will not update the existing `FoundationDBCluster` in the creation step.
So if you're modifying the `FoundationDBCluster` spec, you have to recreate the cluster.

```bash
CLEANUP=false CLUSTER_NAME=dev-cluster-1 NAMESPACE=dev make -kj -C e2e run
```

### StorageClass selection

The e2e tests assume that at least one `StorageClass` is present in the target Kubernetes cluster.
You can provide the targeted `StorageClass` as an environment variable:

```bash
STORAGE_CLASS='my-fancy-storage' make -kj -C e2e test_operator.run
```

If the `STORAGE_CLASS` is not set, the operator will pick storage classes based on labels, see "Setup" above.

### Using a custom nodeSelector

To start the FDB cluster on nodes matching a particular label (e.g. a particular node pool), you can provide a single
key-value pair in an environment variable that is added to the nodeSelector:

```bash
NODE_SELECTOR="my-label=true"
```

### Customize the e2e test runs

The `Makefile` provides different options to customize a test run, e.g. with `FDB_VERSION` a user can specify the used FDB version for a test run:

```bash
FDB_VERSION=7.1.33 make -C e2e -kj test_operator.run
```

If those tests are running on a cluster that has no chaos-mesh installed, you can set `ENABLE_CHAOS_TESTS=false` to disable all test that uses chaos-mesh.

### Running the e2e tests with the unified image

The source can be found in the [FoundationDB repository](https://github.com/apple/foundationdb/tree/main/fdbkubernetesmonitor#foundationdb-kubernetes-monitor).

```bash
FDB_IMAGE=foundationdb/fdb-kubernetes-monitor:7.3.38 \
SIDECAR_IMAGE=foundationdb/fdb-kubernetes-monitor:7.3.38 \
FEATURE_UNIFIED_IMAGE=true \
UPGRADE_VERSIONS="" \
FDB_VERSION="7.3.38" \
make -C e2e test_operator.run
```

### Running the e2e tests with a custom FDB version

The operator e2e tests support to run with custom FDB versions that are not yet released.
The next steps assume that you are able to build a container image for FDB with a custom version.
The `UPGRADE_VERSIONS` defines which versions should be used for the upgrade tests.
The `FDB_VERSION_TAG_MAPPING` provides a way to overwrite the image tag that will be used for the specific tag.
If you use the [split image](../docs/manual/technical_design.md#split-image) you have to build the main `foundationdb` image and the `foundationdb-kubernetes-sidecar` image.
If you use the [unified image](../docs/manual/technical_design.md#unified-image) you only have to build the `fdb-kubernetes-monitor` image.
You have to make sure that the version used in the `FDB_VERSION_TAG_MAPPING` maps to the actual `fdbserver` version in the container image, otherwise some checks will fail because the actual running server is in a different version that the expected version.

```bash
FDB_VERSION="7.3.43" \
UPGRADE_VERSIONS="7.3.43:7.3.52" \
FDB_VERSION_TAG_MAPPING="7.3.52:7.3.52-custom-build" \
make -C e2e test_operator_upgrades.run
```

### Running e2e tests in kind

_NOTE_ This setup is currently not used by anyone and is not being maintained. Some tests currently do not pass in `kind.`

[kind](https://kind.sigs.k8s.io) provides an easy way to run a local Kubernetes cluster.
For running tests on a `kind` cluster you should set the `CLOUD_PROVIDER=kind` environment variable to make sure the test framework is creating clusters with smaller resource requirements.
The following steps assume that `kind` and `helm` are already installed.

```bash
make -C e2e kind-setup
```

This will call the [setup_e2e.sh](./scripts/setup_e2e.sh) script to setup `kind` and install chaos-mesh. Kind clusters do not load images
from a container registry, they need to be explicitly loaded and `kind-setup` doesn't do this correctly right now.  So 
you may need to explicitly load missing images into the kind cluster, like:

```bash
kind load docker-image -n e2e-tests docker.io/foundationdb/fdb-kubernetes-monitor:7.1.57
kind load docker-image -n e2e-tests docker.io/foundationdb/fdb-kubernetes-monitor:7.3.59
```

If you forgot one, the test will not be able to schedule FoundationDB nodes. You can see what is missing by looking
at the events section of the `kubectl describe pod` output.

After testing you can run the following command to remove the kind cluster:

```bash
make -C e2e kind-destroy
```

If you want to iterate over different builds of the operator, you don't have to recreate the kind cluster multiple times.
You just can rebuild the operator image and push the new image inside the kind cluster:

```bash
CLOUD_PROVIDER=kind make -C e2e kind-update-operator
```

_NOTE_: This setup is currently not tested in our CI and requires a Kind cluster that runs with `amd64` nodes.

## What is tested

The following section will cover additional details about the test suites.

### Test running for PRs

The following tests will be running for each PR:

- [test_operator](./test_operator)
- [test_operator_upgrades](./test_operator_upgrades)

Those are all test suites labeled with the `pr` label.
You can run those test suites with: `make -kj -C e2e pr-tests`

### Tests running regularly

The following tests will be running on a nightly base:

- [test_operator](./test_operator)
- [test_operator_upgrades](./test_operator_upgrades)
- [test_operator_ha_upgrades](./test_operator_ha_upgrades)
- [test_operator_velocity](./test_operator_velocity)

Those are all test suites labeled with the `pr` or the `nightly` label.
You can run those test suites with: `make -kj -C e2e nightly-tests`

### Tests that are not run regularly

Those tests will not be running automatically:

- [test_operator_creation_velocity](./test_operator_creation_velocity)
- [test_operator_stress](./test_operator_stress)

e.g. these tests will be used to qualify a new release.
You can run all tests with `make -kj -C e2e run`

## How to debug failed PR tests

All tests will be logging to the `logs` folder in the root of this repository.
If you want to see the current state of a running test you can use `tail`, e.g. `tail -f ./logs/test_operator.log`, to see the progress of the operator tests, the command assumes you are running it from the project directory.
All tests that are started by our CI pipelines will report in the PR with the test status.
