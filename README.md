# Overview

This project provides an experimental operator for managing FoundationDB
clusters on Kubernetes.

# Local Development

To get this controller running in a local Kubernetes cluster:

1.	Run `config/test-certs/generate_secrets.bash` to set up a secret with
	self-signed test certs.
2.	Run `make rebuild-operator` to install the operator.
3.	Run `kubectl apply -f config/samples/apps_v1beta1_foundationdbcluster.yaml`
	to create a new FoundationDB cluster with the operator.

You can see logs from the operator by running
`kubectl logs fdb-kubernetes-operator-controller-manager-0 --container=manager -f`.

