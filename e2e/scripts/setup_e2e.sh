
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
set -eo errexit

function get_image_name() {
  if [[ -z ${REGISTRY} ]]
  then
    echo "${1}"
  else
    echo "${REGISTRY}/${1}"
  fi
}

function preload_foundationdb_images() {
  kind load docker-image "${2}" --name "${1}"
  kind load docker-image "${3}" --name "${1}"
}

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)
cd "${SCRIPT_DIR}"

# Kubernetes version for the Kind clusters
KUBE_VERSION=${KUBE_VERSION:-"v1.24.7"}
# Defines the FDB version that should be preloaded into the Kind cluster
FDB_VERSION=${FDB_VERSION:-"7.1.25"}
# Defines the registry to pull the images from.
REGISTRY=${REGISTRY:-""}
# Name for the new Kind cluster
CLUSTER=${CLUSTER:-"e2e-tests"}
# Chaos Mesh version that should be used to install chaos-mesh
CHAOS_MESH_VERSION=${CHAOS_MESH_VERSION:-"2.5.0"}
# Defines the namespace to install Chaos Mesh to.
CHAOS_NAMESPACE=${CHAOS_NAMESPACE:-"chaos-testing"}

# Create the Kind cluster with the specified Kubernetes version.
CLUSTER=${CLUSTER} KUBE_VERSION=${KUBE_VERSION} ${SCRIPT_DIR}/start_kind_cluster.sh

echo "Preload FoundationDB images for version ${FDB_VERSION}"
fdb_image=$(get_image_name "foundationdb/foundationdb:${FDB_VERSION}")
fdb_sidecar_image=$(get_image_name "foundationdb/foundationdb-kubernetes-sidecar:${FDB_VERSION}-1")
docker pull "${fdb_image}"
docker pull "${fdb_sidecar_image}"

preload_foundationdb_images "${CLUSTER}" "${fdb_image}" "${fdb_sidecar_image}"

echo "Build the operator image from the current revision"
BUILD_PLATFORM="linux/amd64" make -C "${SCRIPT_DIR}/../.." container-build
kind load docker-image "fdb-kubernetes-operator:latest" --name "${CLUSTER}"

echo "Install the CRDs in the Kind cluster"
kubectl apply -f "${SCRIPT_DIR}/../../config/crd/bases/"

echo "Install the chaos-mesh in the Kind cluster"
kubectl create ns "${CHAOS_NAMESPACE}" || true
helm repo add chaos-mesh https://charts.chaos-mesh.org
helm repo update

# The configuration below is tested on a local Kind installation and might be different for the target Kubernetes cluster.
helm upgrade -i chaos-mesh chaos-mesh/chaos-mesh \
    --namespace "${CHAOS_NAMESPACE}" \
    --set dashboard.securityMode=false \
    --set chaosDaemon.socketPath=/run/containerd/containerd.sock \
    --set chaosDaemon.runtime=containerd \
    --version "${CHAOS_MESH_VERSION}"

# Check if the Pods are running
kubectl wait --for=condition=ready pods --namespace chaos-testing -l app.kubernetes.io/instance=chaos-mesh
