# Overview

[![Go Report Card](https://goreportcard.com/badge/github.com/FoundationDB/fdb-kubernetes-operator)](https://goreportcard.com/report/github.com/FoundationDB/fdb-kubernetes-operator)
![GitHub](https://img.shields.io/github/license/FoundationDB/fdb-kubernetes-operator)
[![CI for master branch](https://github.com/FoundationDB/fdb-kubernetes-operator/actions/workflows/pull_request.yml/badge.svg)](https://github.com/FoundationDB/fdb-kubernetes-operator/actions/workflows/pull_request.yml)

This project provides an experimental operator for managing FoundationDB
clusters on Kubernetes.

## Running the Operator

To run the operator in your environment, you need to install the controller and
the CRDs:

```bash
kubectl apply -f https://raw.githubusercontent.com/FoundationDB/fdb-kubernetes-operator/master/config/crd/bases/apps.foundationdb.org_foundationdbclusters.yaml
kubectl apply -f https://raw.githubusercontent.com/FoundationDB/fdb-kubernetes-operator/master/config/crd/bases/apps.foundationdb.org_foundationdbbackups.yaml
kubectl apply -f https://raw.githubusercontent.com/FoundationDB/fdb-kubernetes-operator/master/config/crd/bases/apps.foundationdb.org_foundationdbrestores.yaml
kubectl apply -f https://raw.githubusercontent.com/foundationdb/fdb-kubernetes-operator/master/config/samples/deployment.yaml
```

At that point, you can set up a sample cluster:

```bash
kubectl apply -f https://raw.githubusercontent.com/foundationdb/fdb-kubernetes-operator/master/config/samples/cluster.yaml
```

You can see logs from the operator by running
`kubectl logs -f -l app=fdb-kubernetes-operator-controller-manager --container=manager`. To determine whether the reconciliation has completed, you can run `kubectl get foundationdbcluster my-cluster`. This will show the latest generation of the
spec and the last reconciled generation of the spec. Once reconciliation has completed, these values will be the same.

Once the reconciliation is complete, you can run `kubectl exec -it my-cluster-log-1 -- fdbcli` to open up a CLI on your cluster.

You can also browse the [sample directory](config/samples) for more examples of different resource configurations.

For more information about using the operator, including detailed discussion of how to customize your deployments, see the [user manual](docs/manual/index.md).

For more information on version compatibility, see our [compatibility guide](docs/compatibility.md).

For more information on the fields you can define on the cluster resource, see
the [API documentation](docs/cluster_spec.md).

## Local Development

### Environment Set-up

1. Install GO on your machine, see the [Getting Started](https://golang.org/doc/install) guide for more information.
2. Install KubeBuilder and its dependencies on your machine, see [The KubeBuilder Book](https://book.kubebuilder.io/quick-start.html) for more information (currently version `2.3.2` is used).
3. Set your $GOPATH, e.x. `/Users/me/Code/go`.
4. Install [kustomize](https://github.com/kubernetes-sigs/kustomize).
5. Install the [foundationDB client package](https://www.foundationdb.org/download).
6. If you want to modify the manifests you currently need [yq](https://github.com/mikefarah/yq), we use the `v4.6.1` version.

### Running Locally

To get this controller running in a local Kubernetes cluster:

1. Change your current directory to $GOPATH/src/github.com using the
   command `cd $GOPATH/src/github.com` and run `mkdir foundationdb`
   to create the directory `foundationdb`.
2. CD into the newly created directory and clone this github repo inside
   `$GOPATH/src/github.com/foundationdb`.
3. Run `config/test-certs/generate_secrets.bash` to set up a secret with
   self-signed test certs.
4. Run `make rebuild-operator` to install the operator.
5. Run `kubectl apply -f config/samples/cluster_local_tls.yaml`
   to create a new FoundationDB cluster with the operator.

### Testing

The test suite runs a live copy of etcd which is heavily dependent on disk I/O
performance; if you encounter unexpected test failures, slow test warnings, or
overall slow test runs (more than 2 minutes in duration), this may be the cause.

One workaround is to run the test suite with etcd writing to an in-memory
filesystem. To configure this on Linux:

```shell
mkdir ramdisk
sudo mount -t tmpfs tempfs $(pwd)/ramdisk
export TMPDIR=$(pwd)/ramdisk
```

Then run the test suite under that environment.

For `MacOS` you can use the script located under `./scripts/setup_ramdisk_macos.sh`.
This script will setup a ramdisk and mount it (the default location will be `${HOME}/volatile`).
