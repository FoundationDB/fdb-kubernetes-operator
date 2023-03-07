#!/usr/bin/env bash
# Set up local FDB non-HA cluster on a 4-node kind k8s cluster
# This only works on x86 machines as FDB doesn't provide arm64 Linux binaries yet
read -p "create kind cluster? (enter yes or no): " createKindCluster

cluster=${cluster:-"local-cluster"}

if [ "${createKindCluster}" = "yes" ]; then
    echo "===Start creating k8s cluster on kind"
    #../scripts/setup_kind_local_registry.sh # the script doesn't work 
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
echo "Waiting for creating fdb pods..."
sleep 2;
kubectl wait --for=condition=ready pod -l foundationdb.org/fdb-cluster-name=test-cluster

