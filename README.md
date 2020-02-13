# Overview

This project provides an experimental operator for managing FoundationDB
clusters on Kubernetes.

# Running the Operator

To run the operator in your environment, you need to install the controller and
the CRD:

		kubectl apply -f https://raw.githubusercontent.com/foundationdb/fdb-kubernetes-operator/master/config/samples/deployment.yaml

At that point, you can set up one of the sample clusters:

		kubectl apply -f https://raw.githubusercontent.com/foundationdb/fdb-kubernetes-operator/master/config/samples/cluster_local.yaml

You can see logs from the operator by running
`kubectl logs fdb-kubernetes-operator-controller-manager-0 --container=manager`. To determine whether the reconciliation has completed, you can run `kubectl get foundationdbcluster sample-cluster`. This will show the latest generation of the
spec and the last reconciled generation of the spec. Once reconciliation has completed, these values will be the same.

Once the reconciliation is complete, you can run `kubectl exec -it sample-cluster-1 fdbcli` to open up a CLI on your cluster.

You can also browse the [sample directory](config/samples) for more examples of how to configure a cluster.

Most of these examples are designed for doing local development on the operator, so there may be aspects of them that you need to adapt if you want to run in a more realistic environment. The TLS examples assume you have a certificate and key stored in Kubernetes secrets, which may not be the mechanism you want to use for your certificates. The backup examples assume you have backup credentials stored in Kubernetes secrets, so the same consideration applies.

For more information about using the operator, see the [user manual](docs/user_manual.md).

For more information on version compatibility, see our [compatibility guide](docs/compatibility.md).

For more information on the fields you can define on the cluster resource, see
the [go docs](https://godoc.org/github.com/FoundationDB/fdb-kubernetes-operator/pkg/apis/apps/v1beta1#FoundationDBCluster).

# Local Development

## Environment Set-up

1. Install GO on your machine, see the [Getting Started](https://golang.org/doc/install) guide for more information.
2. Install KubeBuilder and its dependencies on your machine, see [The KubeBuilder Book](https://book.kubebuilder.io/quick-start.html) for more information.
3. Set your $GOPATH, e.x. `/Users/me/Code/go`
4. Install [kustomize](https://github.com/kubernetes-sigs/kustomize).

## Running Locally

To get this controller running in a local Kubernetes cluster:

1.  Change your current directory to $GOPATH/src/github.com using the
	command `cd $GOPATH/src/github.com` and run `mkdir foundationdb`
	to create the directory `foundationdb`.
2.	CD into newly created directory and clone this github repo inside
	the created directory that is `foundationdb`.
3.	Run `config/test-certs/generate_secrets.bash` to set up a secret with
	self-signed test certs.
4.	Run `make rebuild-operator` to install the operator.
5.	Run `kubectl apply -f config/samples/cluster_local_tls.yaml`
	to create a new FoundationDB cluster with the operator.
