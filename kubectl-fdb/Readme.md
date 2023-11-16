# Kubectl plugin for FDB on Kubernetes

## Installation

Make sure you have `sha256sum` and `jq` installed otherwise run the following `brew` commands:

```bash
brew install coreutils # required for sha256sum
brew install jq # required for jq
```

The `kubectl fdb plugin` is released as a binary as part of our release process.
For the latest version take a look at the [release page](https://github.com/FoundationDB/fdb-kubernetes-operator/releases).

Install from release:

```bash
pushd $TMPDIR
if [ -f "$TMPDIR/latest.plugin" ]; then
   rm "$TMPDIR/latest.plugin"
fi
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

## Plugin Expiration
We have added a commandline flag to the plugin that by default makes plugin to check its version against latest release(using GitHub API). If plugin is not using latest release version, it will print a warning message and skip the command. You may skip this version check by setting default value of the flag when issuing a command: `--version-check=false`. The plugin will read the version info from GitHub API and store in a file in temp directory(`$TMPDIR/latest.plugin`), it'll use this file to check the version and will refresh it daily. If you still get the warning message right after upgrading, please remove `$TMPDIR/latest.plugin` file and retry.    
