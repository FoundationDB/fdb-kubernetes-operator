#!/usr/bin/env bash
# This source file is part of the FoundationDB open source project
#
# Copyright 2023 Apple Inc. and the FoundationDB project authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This script works out of the box for Linux environments (tested on Ubuntu 16.04.5 LTS), after installing dependent software (kind, Docker, and kustomize);
# Running in other environments may involve non-trivial debugging effort.

# Set up local FDB non-HA cluster on a 4-node kind k8s cluster
# This only works on x86 machines as FDB doesn't provide arm64 Linux binaries yet.
set -eo errexit

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)
cd "${SCRIPT_DIR}"

# Kubernetes version for the Kind clusters
KUBE_VERSION=${KUBE_VERSION:-"v1.24.7"}
# Kind cluster name
CLUSTER=${CLUSTER:-"local-cluster"}

if ! kind get clusters | grep "${CLUSTER}" &> /dev/null; then
    echo "===Start creating k8s cluster on kind"
    cat <<EOF | kind create cluster --name "${CLUSTER}" --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  image: kindest/node:${KUBE_VERSION}
- role: worker
  image: kindest/node:${KUBE_VERSION}
- role: worker
  image: kindest/node:${KUBE_VERSION}
- role: worker
  image: kindest/node:${KUBE_VERSION}
EOF
else
    echo "===Kind cluster already exists"
fi

kubectl config use-context "kind-${CLUSTER}"

echo "===Start building operator"
${SCRIPT_DIR}/../../config/test-certs/generate_secrets.bash
BUILD_PLATFORM="linux/amd64" make -C "${SCRIPT_DIR}/../.." rebuild-operator

echo "===Load operator image to kind cluster"
kind load docker-image "fdb-kubernetes-operator:latest" --name "${CLUSTER}"

echo "===Creating a FDB cluster with the FDB operator"
kubectl apply -k "${SCRIPT_DIR}/../../config/tests/base"

echo "===Done==="
kubectl get fdb
echo "Waiting for creating FDB Pods..."
sleep 10;
kubectl wait --for=condition=ready pod -l foundationdb.org/fdb-cluster-name=test-cluster
kubectl get fdb
