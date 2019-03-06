# Overview

This project provides an experimental operator for managing FoundationDB
clusters on Kubernetes.

# Local Development

To get this controller running in a local Kubernetes cluster, run
`make docker-build deploy bounce-operator` to install the operator. Then run
`kubectl apply -f config/samples/apps_v1beta1_foundationdbcluster.yaml` to
create a new FoundationDB cluster with the operator.

You can see logs from the operator by running
`kubectl logs fdb-kubernetes-operator-controller-manager-0 --container=manager -f`.
