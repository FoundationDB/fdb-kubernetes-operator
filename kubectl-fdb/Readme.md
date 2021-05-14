# Kubectl plugin for FDB on Kubernetes

## Installation

The `kubectl fdb plugin` is released as a binary as part of our release process.
For the latest version take a look at the [release page](https://github.com/FoundationDB/fdb-kubernetes-operator/releases).

Install from release:

```bash
pushd $TMPDIR
OS=macos
VERSION="$(curl -s "https://api.github.com/repos/FoundationDB/fdb-kubernetes-operator/releases/latest" | jq -r '.tag_name')"
curl -sLo kubectl-fdb "https://github.com/FoundationDB/fdb-kubernetes-operator/releases/download/${VERSION}/kubectl-fdb-${VERSION}-${OS}"
chmod +x kubectl-fdb
sudo mv ./kubectl-fdb /usr/local/bin
popd
```

In order to install the latest version from the source code run:

```bash
make plugin
# move the binary into your path
export PATH="$PATH:$(pwd)/bin" 
```

## Usage

Run `kubectl fdb help` to get the latest help.

### Planned operations

We have a list of [planned operations](https://github.com/FoundationDB/fdb-kubernetes-operator/issues?q=is%3Aissue+is%3Aopen+label%3Aplugin)
that we want to implement.
Raise an issue if you miss a specific command to operate FDB on Kubernetes.
