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
set -eo errexit

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &> /dev/null && pwd)
cd "${SCRIPT_DIR}"

# Kubernetes version for the Kind clusters
KUBE_VERSION=${KUBE_VERSION:-"v1.24.7"}
# Name for the new Kind cluster
CLUSTER=${CLUSTER:-"ci-test"}

# Create the Kind cluster with the specified Kubernetes version.
# The extra mount makes sure that the Kind cluster is able to pull images from
# a private registry, see: https://kind.sigs.k8s.io/docs/user/private-registries/#mount-a-config-file-to-each-node
cat <<EOF | kind create cluster --name "${CLUSTER}" --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  image: kindest/node:${KUBE_VERSION}
  extraMounts:
  - containerPath: /var/lib/kubelet/config.json
    hostPath: $HOME/.docker/config.json
- role: worker
  image: kindest/node:${KUBE_VERSION}
  extraMounts:
  - containerPath: /var/lib/kubelet/config.json
    hostPath: $HOME/.docker/config.json
- role: worker
  image: kindest/node:${KUBE_VERSION}
  extraMounts:
  - containerPath: /var/lib/kubelet/config.json
    hostPath: $HOME/.docker/config.json
- role: worker
  image: kindest/node:${KUBE_VERSION}
  extraMounts:
  - containerPath: /var/lib/kubelet/config.json
    hostPath: $HOME/.docker/config.json
EOF
