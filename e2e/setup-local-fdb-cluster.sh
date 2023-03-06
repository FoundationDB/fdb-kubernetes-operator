#!/bin/bash
# Set up local FDB non-HA cluster on a 4-node kind k8s cluster

SkipCreatingLocalCluster=${1:-0}

cluster="local-cluster"

if ${SkipCreatingLocalCluster} != 0; then
    echo "===Start creating k8s cluster on kind"
    kind create cluster --name ${cluster} --config ./local-cluster-config.yaml
else
    echo "===Skip creating k8s cluster on kind"
    kind get clusters
fi

echo "===Start building operator"
cd ..
echo "---We should be at reop\'s root directory: "
pwd

./config/test-certs/generate_secrets.bash
make rebuild-operator

echo "===Load operator image to kind cluster"
kind load docker-image "fdb-kubernetes-operator:latest" --name ${cluster}

echo "===Creating a FDB cluster with the FDB operator"
kubectl apply -k ./config/tests/base

echo "===Done==="

kubectl get fdb
kubectl get pods

