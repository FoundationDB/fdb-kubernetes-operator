#!/usr/bin/env bash
# Set up local FDB non-HA cluster on a 4-node kind k8s cluster
# This only works on x86 machines as FDB doesn't provide arm64 Linux binaries yet
# Assumptions: (1) There is at most one kind cluster in the env; (2) kubectl is pointing to the kind cluster

if [ "$#" -le 1 ]; then
read -p "create kind cluster? (enter yes or no): " createKindCluster
read -p "enter k8s node version? (e.g., v1.23.0): " version
else
    createKindCluster=$1
    version=${2}
fi

cluster=${cluster:-"local-cluster"}

if [ "${createKindCluster}" = "yes" ]; then
    echo "===Start creating k8s cluster on kind"
    cat <<EOF | kind create cluster --name ${cluster} --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  image: kindest/node:${version}
- role: worker
- role: worker
- role: worker
EOF

else
    echo "===Skip creating k8s cluster on kind"
    # TODO: make sure kind is using the local-cluster context
    kind get clusters
fi

echo "===Start building operator"
echo "---We should be at the repo's root directory: "
pwd

./config/test-certs/generate_secrets.bash
make rebuild-operator

echo "===Load operator image to kind cluster"
kind load docker-image "fdb-kubernetes-operator:latest" --name ${cluster}

echo "===Creating a FDB cluster with the FDB operator"
kubectl apply -k ./config/tests/base

echo "===Done==="

# TODO: Make sure kubectl context is pointing to the kind cluster
kubectl get fdb
echo "Waiting for creating FDB Pods..."
sleep 10;
kubectl wait --for=condition=ready pod -l foundationdb.org/fdb-cluster-name=test-cluster
kubectl get fdb
