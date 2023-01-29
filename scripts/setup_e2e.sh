#!/usr/bin/env bash

# This source file is part of the FoundationDB open source project
#
# Copyright 2022 Apple Inc. and the FoundationDB project authors
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

# wait_for_node_setup is used to wait until the kubelet has started and the Pod CIDR is available.
function wait_for_node_setup() {
  for _ in {1..60}; do
    if [ -n "$(kubectl --kubeconfig "${1}" get node  -o jsonpath='{range .items[*]}{.spec.podCIDR}{"\n"}')" ]; then
      return
    fi
    sleep 1
  done

  exit 1
}

function preload_foundationdb_images() {
  kind load docker-image "foundationdb/foundationdb:${2}" --name "${1}"
  kind load docker-image "foundationdb/foundationdb-kubernetes-sidecar:${2}-1" --name "${1}"
}

# add_routes is used to allow Pods in the different clusters to communicate
function add_routes() {
  unset IFS
  routes=$(kubectl --kubeconfig "${3}" get node "${2}" -o jsonpath='ip route add {.spec.podCIDR} via {.status.addresses[?(.type=="InternalIP")].address}')
  echo "Connecting cluster ${1} to ${2}"

  IFS=$'\n'
  for n in $(kind get nodes --name "${1}"); do
    for r in $routes; do
      docker exec "${n}" bash -c "${r}"
    done
   done
   unset IFS
}

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)
cd "${SCRIPT_DIR}"

# Kubernetes version for the Kind clusters
KUBE_VERSION=${KUBE_VERSION:-"v1.24.7"}
FDB_VERSION=${FDB_VERSION:-"7.1.25"}

# To test a multi-region FDB cluster setup we need to have 4 Kubernetes clusters
cluster1=${CLUSTER1:-cluster1}
cluster2=${CLUSTER2:-cluster2}
cluster3=${CLUSTER3:-cluster3}
cluster4=${CLUSTER4:-cluster4}

# For all the clusters we have to create tha according kubeconfig
kubeconfig1=${KUBECONFIG1:-"${cluster1}.kubeconfig"}
kubeconfig2=${KUBECONFIG2:-"${cluster2}.kubeconfig"}
kubeconfig3=${KUBECONFIG3:-"${cluster3}.kubeconfig"}
kubeconfig4=${KUBECONFIG4:-"${cluster4}.kubeconfig"}

# We have to export them for envsubst
export KUBE_VERSION
export reg_name
export reg_port

echo "Creating Kind clusters"
envsubst < "${cluster1}.yml" | kind create cluster --name "${cluster1}" --config=-
envsubst < "${cluster2}.yml" | kind create cluster --name "${cluster2}" --config=-
envsubst < "${cluster3}.yml" | kind create cluster --name "${cluster3}" --config=-
envsubst < "${cluster4}.yml" | kind create cluster --name "${cluster4}" --config=-

unset KUBE_VERSION reg_name reg_port

kind get kubeconfig --name "${cluster1}" > "${kubeconfig1}"
kind get kubeconfig --name "${cluster2}" > "${kubeconfig2}"
kind get kubeconfig --name "${cluster3}" > "${kubeconfig3}"
kind get kubeconfig --name "${cluster4}" > "${kubeconfig4}"

echo "Preload FoundationDB images"
docker pull "foundationdb/foundationdb:${FDB_VERSION}"
docker pull "foundationdb/foundationdb-kubernetes-sidecar:${FDB_VERSION}-1"

preload_foundationdb_images "${cluster1}" "${FDB_VERSION}"
preload_foundationdb_images "${cluster2}" "${FDB_VERSION}"
preload_foundationdb_images "${cluster3}" "${FDB_VERSION}"
preload_foundationdb_images "${cluster4}" "${FDB_VERSION}"

echo "Waiting for Kind nodes to be initialized"
wait_for_node_setup "${kubeconfig1}"
wait_for_node_setup "${kubeconfig2}"
wait_for_node_setup "${kubeconfig3}"
wait_for_node_setup "${kubeconfig4}"

echo "Connect Kind clusters"
add_routes "${cluster1}" "${cluster2}-control-plane" "${kubeconfig2}"
add_routes "${cluster1}" "${cluster3}-control-plane" "${kubeconfig3}"
add_routes "${cluster1}" "${cluster4}-control-plane" "${kubeconfig4}"

add_routes "${cluster2}" "${cluster1}-control-plane" "${kubeconfig1}"
add_routes "${cluster2}" "${cluster3}-control-plane" "${kubeconfig3}"
add_routes "${cluster2}" "${cluster4}-control-plane" "${kubeconfig4}"

add_routes "${cluster3}" "${cluster1}-control-plane" "${kubeconfig1}"
add_routes "${cluster3}" "${cluster2}-control-plane" "${kubeconfig2}"
add_routes "${cluster3}" "${cluster4}-control-plane" "${kubeconfig4}"

add_routes "${cluster4}" "${cluster1}-control-plane" "${kubeconfig1}"
add_routes "${cluster4}" "${cluster2}-control-plane" "${kubeconfig2}"
add_routes "${cluster4}" "${cluster3}-control-plane" "${kubeconfig3}"

make -C "${SCRIPT_DIR}/.." container-build
kind load docker-image "fdb-kubernetes-operator:latest" --name "${cluster1}"
kind load docker-image "fdb-kubernetes-operator:latest" --name "${cluster2}"
kind load docker-image "fdb-kubernetes-operator:latest" --name "${cluster3}"
kind load docker-image "fdb-kubernetes-operator:latest" --name "${cluster4}"

echo "Installing the test certificates"
KUBECONFIG=${SCRIPT_DIR}/${kubeconfig1} ${SCRIPT_DIR}/../config/test-certs/generate_secrets.bash
KUBECONFIG=${SCRIPT_DIR}/${kubeconfig2} ${SCRIPT_DIR}/../config/test-certs/generate_secrets.bash
KUBECONFIG=${SCRIPT_DIR}/${kubeconfig3} ${SCRIPT_DIR}/../config/test-certs/generate_secrets.bash
KUBECONFIG=${SCRIPT_DIR}/${kubeconfig4} ${SCRIPT_DIR}/../config/test-certs/generate_secrets.bash

echo "Install the CRDs and the operator in every Kind cluster"
KUBECTL_ARGS="--kubeconfig ${SCRIPT_DIR}/${kubeconfig1}" make -C "${SCRIPT_DIR}/.." deploy
KUBECTL_ARGS="--kubeconfig ${SCRIPT_DIR}/${kubeconfig2}" make -C "${SCRIPT_DIR}/.." deploy
KUBECTL_ARGS="--kubeconfig ${SCRIPT_DIR}/${kubeconfig3}" make -C "${SCRIPT_DIR}/.." deploy
KUBECTL_ARGS="--kubeconfig ${SCRIPT_DIR}/${kubeconfig4}" make -C "${SCRIPT_DIR}/.." deploy

# TODO(johscheuer): Next step is to add e2e tests for this setup.

echo "For clean up run kind delete clusters ${cluster1} ${cluster2} ${cluster3} ${cluster4}"

echo "For usage run the following export commands:
export KUBECONFIG1=${SCRIPT_DIR}/${kubeconfig1}
export KUBECONFIG2=${SCRIPT_DIR}/${kubeconfig2}
export KUBECONFIG3=${SCRIPT_DIR}/${kubeconfig3}
export KUBECONFIG4=${SCRIPT_DIR}/${kubeconfig4}"
