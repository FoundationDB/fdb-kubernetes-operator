#!/usr/bin/env bash

set -eu

# To test a multi-region FDB cluster setup we need to have 4 Kubernetes clusters
cluster1=${CLUSTER1:-cluster1}
cluster2=${CLUSTER2:-cluster2}
cluster3=${CLUSTER3:-cluster3}

# For all the clusters we have to create tha according kubeconfig
kubeconfig1=${KUBECONFIG1:-"${cluster1}.kubeconfig"}
kubeconfig2=${KUBECONFIG2:-"${cluster2}.kubeconfig"}
kubeconfig3=${KUBECONFIG3:-"${cluster3}.kubeconfig"}

kubectl --kubeconfig "${kubeconfig1}" delete fdb -l cluster-group=test-cluster
kubectl --kubeconfig "${kubeconfig2}" delete fdb -l cluster-group=test-cluster
kubectl --kubeconfig "${kubeconfig3}" delete fdb -l cluster-group=test-cluster
