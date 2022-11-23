#!/usr/bin/env bash
set -o errexit

# We have to install the FDB client libraries
export FDB_VERSION=6.2.29
export FDB_WEBSITE=https://github.com/apple/foundationdb/releases/download
curl --fail -L ${FDB_WEBSITE}/${FDB_VERSION}/foundationdb-clients_${FDB_VERSION}-1_amd64.deb -o /tmp/fdb.deb && dpkg -i /tmp/fdb.deb && rm /tmp/fdb.deb
# Some tests require the presence of kubectl, for more information about the installation see: https://kubernetes.io/docs/tasks/tools/install-kubectl-linux/
curl -L "https://dl.k8s.io/release/v1.24.3/bin/linux/amd64/kubectl" -o /tmp/kubectl
curl -L "https://dl.k8s.io/v1.24.3/bin/linux/amd64/kubectl.sha256" -o /tmp/kubectl.sha256
echo "$(cat /tmp/kubectl.sha256) /tmp/kubectl" | sha256sum --check
install -o root -g root -m 0755 /tmp/kubectl /usr/local/bin/kubectl
# Install Kind to run a Kubernetes cluster inside the container
# TODO (johscheuer): We have to add some additional steps to make that work.
curl -Lo /tmp/kind https://kind.sigs.k8s.io/dl/v0.14.0/kind-linux-amd64
chmod +x /tmp/kind
mv /tmp/kind /usr/local/bin/kind

exec bash
