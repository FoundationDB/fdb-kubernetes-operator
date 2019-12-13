# Overview

This project provides an experimental operator for managing FoundationDB
clusters on Kubernetes.

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
5.	Run `kubectl apply -f config/samples/local_cluster.yaml`
	to create a new FoundationDB cluster with the operator.

You can see logs from the operator by running
`kubectl logs fdb-kubernetes-operator-controller-manager-0 --container=manager -f`.

