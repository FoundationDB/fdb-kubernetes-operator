# Kubectl plugin for FDB on Kubernetes

## Installation

The `kubectl fdb plugin` is released as a binary as part of our release process.
For the latest version take a look at the [release page](https://github.com/FoundationDB/fdb-kubernetes-operator/releases).

Install from release:

```bash
pushd $TMPDIR
OS=$(uname)
ARCH=$(uname -m)
VERSION="$(curl -s "https://api.github.com/repos/FoundationDB/fdb-kubernetes-operator/releases/latest" | jq -r '.tag_name')"
curl -sLO "https://github.com/FoundationDB/fdb-kubernetes-operator/releases/download/${VERSION}/checksums.txt"
curl -sLO "https://github.com/FoundationDB/fdb-kubernetes-operator/releases/download/${VERSION}/kubectl-fdb_${VERSION}_${OS}_${ARCH}"
sha256sum --ignore-missing -c checksums.txt
chmod +x kubectl-fdb_${VERSION}_${OS}_${ARCH}
sudo mv ./kubectl-fdb_${VERSION}_${OS}_${ARCH} /usr/local/bin/kubectl-fdb
popd
```

In order to install the latest version from the source code run:

```bash
make plugin
# move the binary into your path
export PATH="${PATH}:$(pwd)/dist/kubectl-fdb_$(go env GOHOSTOS)_$(go env GOARCH)"
```

You can verify the version of the locally installed `kubectl-fdb` binary by running:

```bash
$ kubectl fdb version --client-only
kubectl-fdb: 1.16.0
```

## Usage

Run `kubectl fdb help` to get the latest help.

### Planned operations

We have a list of [planned operations](https://github.com/FoundationDB/fdb-kubernetes-operator/issues?q=is%3Aissue+is%3Aopen+label%3Aplugin)
that we want to implement.
Raise an issue if you miss a specific command to operate FDB on Kubernetes.
