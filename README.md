<img alt="FoundationDB logo" src="docs/FDB_logo.png?raw=true" width="400">

FoundationDB is a distributed database designed to handle large volumes of structured data across clusters of commodity servers. It organizes data as an ordered key-value store and employs ACID transactions for all operations. It is especially well-suited for read/write workloads but also has excellent performance for write-intensive workloads. Users interact with the database using API language binding.

To learn more about FoundationDB, visit [foundationdb.org](https://www.foundationdb.org/)

# Overview

[![Go Report Card](https://goreportcard.com/badge/github.com/FoundationDB/fdb-kubernetes-operator)](https://goreportcard.com/report/github.com/FoundationDB/fdb-kubernetes-operator)
![GitHub](https://img.shields.io/github/license/FoundationDB/fdb-kubernetes-operator)
[![CI for main branch](https://github.com/FoundationDB/fdb-kubernetes-operator/actions/workflows/pull_request.yml/badge.svg)](https://github.com/FoundationDB/fdb-kubernetes-operator/actions/workflows/pull_request.yml)

This project provides an operator for managing FoundationDB clusters on Kubernetes.

## Running the Operator

To run the operator in your environment, you need to install the controller and the CRDs:
*Note* this will install the latest version from main. For a production setup you should refer to a specific tag.

```bash
kubectl apply -f https://raw.githubusercontent.com/FoundationDB/fdb-kubernetes-operator/main/config/crd/bases/apps.foundationdb.org_foundationdbclusters.yaml
kubectl apply -f https://raw.githubusercontent.com/FoundationDB/fdb-kubernetes-operator/main/config/crd/bases/apps.foundationdb.org_foundationdbbackups.yaml
kubectl apply -f https://raw.githubusercontent.com/FoundationDB/fdb-kubernetes-operator/main/config/crd/bases/apps.foundationdb.org_foundationdbrestores.yaml
kubectl apply -f https://raw.githubusercontent.com/foundationdb/fdb-kubernetes-operator/main/config/samples/deployment.yaml
```

At that point, you can set up a sample cluster:

```bash
kubectl apply -f https://raw.githubusercontent.com/foundationdb/fdb-kubernetes-operator/main/config/samples/cluster.yaml
```

You can see logs from the operator by running
`kubectl logs -f -l app=fdb-kubernetes-operator-controller-manager --container=manager`. To determine whether the reconciliation has completed, you can run `kubectl get foundationdbcluster test-cluster`. This will show the latest generation of the
spec and the last reconciled generation of the spec. Once reconciliation has completed, these values will be the same.

Once the reconciliation is complete, you can run `kubectl exec -it test-cluster-log-1 -- fdbcli` to open up a CLI on your cluster.

You can also browse the [sample directory](config/samples) for more examples of different resource configurations.

For more information about using the operator, including detailed discussion of how to customize your deployments, see the [user manual](docs/manual/index.md).

For more information on version compatibility, see our [compatibility guide](docs/compatibility.md).

For more information on the fields you can define on the cluster resource, see the [API documentation](docs/cluster_spec.md).

### Using helm

You can also use helm to install and manage the operator:

```bash
helm repo add fdb-kubernetes-operator https://foundationdb.github.io/fdb-kubernetes-operator/
helm repo update
helm install fdb-kubernetes-operator fdb-kubernetes-operator/fdb-kubernetes-operator
 ```

## Local Development

### Environment Set-up

1. Install Go on your machine, see the [Getting Started](https://golang.org/doc/install) guide for more information.
1. Install the required dependencies with `make deps`.
1. Install the [foundationDB client package](https://github.com/apple/foundationdb/releases).

### Running Locally

To get this controller running in a local Kubernetes cluster:

1. The assumption is that you have local Kubernetes cluster running.
   Depending on what solution you use some of the following steps might differ.
1. Clone this repository onto your local machine.
1. Run `config/test-certs/generate_secrets.bash` to set up a secret with
   self-signed test certs.
1. Run `make rebuild-operator` to install the operator. By default, the
   container image is built for the platform where this command is executed.
   To override the platform, for example, to build an amd64 image on Apple M1,
   you can set the `BUILD_PLATFORM` env variable
   `BUILD_PLATFORM="linux/amd64" make rebuild-operator`.
1. Run `kubectl apply -k ./config/tests/base`
   to create a new FoundationDB cluster with the operator.

### Running Locally with nerdctl

Instead of Docker you can also use [nerdctl](https://github.com/containerd/nerdctl) to build and push your images.
In order to use a different image builder than docker you can use the env variable `BUILDER`:

```bash
# This will use nerdctl for building the image in the k8s.io namespace
export BUILDER='nerdctl -n k8s.io'
```

You can test your setup with `SKIP_TEST=1 make container-build` which will build the image locally.
After the command successfully finished you can verify with `nerdctl -n k8s.io images fdb-kubernetes-operator:latest` that the image is available.

### Customizing Your Build

The makefile supports environment variables that allow you to customize your build. You can use these to push to custom docker repos and deployment platforms.

* `IMG`: This specifies the image that gets built for the operator.
* `SIDECAR_IMG`: This specifies the image for the foundationdb-kubernetes-sidecar process used in init containers for the operator. This does not change the images used for the FoundationDB clusters, which are specified in the cluster spec.
* `REMOTE_BUILD`: This can be set to 1 to indicate that you are running the operator in a remote environment, rather than on your local development machine. This will activate a [remote build](config/development/remote_build.yaml) patch, which changes the image pull policy in the operator's pod spec. Setting this also tells the Makefile to push images as part of the `rebuild-operator` command.
* `FDB_WEBSITE`: This specifies the base path for the website used to download FDB client packages in the docker builds. You can use this to download custom binaries from your own host, provided that your path structure matches the paths expected in the [Dockerfile](Dockerfile).

### Local Development with arm64

Currently FDB doesn't provide any client libraries for arm64, this means we have to run everything inside a VM or with Rosetta.

#### Docker based approach

If you already have Docker installed you can use the following steps to build and test the operator locally inside a Docker container:

```bash
# Create a container with amd64 which contains go
docker run --rm --entrypoint=/bin/bash -ti --platform="linux/amd64" -v $(pwd):/work -w /work docker.io/library/golang:1.18 ./scripts/setup_container.sh
# Now we can run the lint and test steps
make fmt lint test
```

## Known Limitations

1. Support for backups in the operator is still in development, and there are significant missing features.
2. The unified image is still experimental, and is not recommended outside of development environments.
3. Additional limitations can be found under [Warnings](docs/manual/warnings.md).
