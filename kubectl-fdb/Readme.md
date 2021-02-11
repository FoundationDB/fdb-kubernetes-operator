# Kubectl plugin for FDB on Kubernetes

## Installation

The `kubectl fdb plugin` is released as a binary as part of our release process.
For the latest version take a look at the [release page](https://github.com/FoundationDB/fdb-kubernetes-operator/releases).

Install from release:

```bash
pushd $TMPDIR
OS=macos
VERSION="v0.27.1"
curl -sLo kubectl-fdb.tar.gz "https://github.com/FoundationDB/fdb-kubernetes-operator/releases/download/${VERSION}/kubectl-fdb-${VERSION}-${OS}.tar.gz"
tar xfz kubectl-fdb.tar.gz
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

Run `kubectl fdb help` to get the latest help:

```bash
$ kubectl fdb help
kubectl fdb plugin for the interaction with the FoundationDB operator.

Usage:
  kubectl-fdb [flags]
  kubectl-fdb [command]

Available Commands:
  cordon      Adds all instance (or multiple) that run on a node to the remove list of the given cluster
  exec        Runs a command on a container in an FDB cluster
  help        Help about any command
  remove      Adds an instance (or multiple) to the remove list of the given cluster
  version     version of kubectl-fdb & foundationdb-operator

Flags:
      --as string                      Username to impersonate for the operation
      --as-group stringArray           Group to impersonate for the operation, this flag can be repeated to specify multiple groups.
      --cache-dir string               Default HTTP cache directory (default "/Users/jscheuermann/.kube/http-cache")
      --certificate-authority string   Path to a cert file for the certificate authority
      --client-certificate string      Path to a client certificate file for TLS
      --client-key string              Path to a client key file for TLS
      --cluster string                 The name of the kubeconfig cluster to use
      --context string                 The name of the kubeconfig context to use
  -f, --force                          Suppress the confirmation dialog
  -h, --help                           help for kubectl-fdb
      --insecure-skip-tls-verify       If true, the server's certificate will not be checked for validity. This will make your HTTPS connections insecure
      --kubeconfig string              Path to the kubeconfig file to use for CLI requests.
  -n, --namespace string               If present, the namespace scope for this CLI request
  -o, --operator-name string           Name of the Deployment for the operator. (default "fdb-kubernetes-operator-controller-manager")
      --request-timeout string         The length of time to wait before giving up on a single server request. Non-zero values should contain a corresponding time unit (e.g. 1s, 2m, 3h). A value of zero means don't timeout requests. (default "0")
  -s, --server string                  The address and port of the Kubernetes API server
      --token string                   Bearer token for authentication to the API server
      --user string                    The name of the kubeconfig user to use

Use "kubectl-fdb [command] --help" for more information about a command.
```

### Planned operations

Currently we have a list of [planned operations](https://github.com/FoundationDB/fdb-kubernetes-operator/issues?q=is%3Aissue+is%3Aopen+label%3Aplugin)
that we want to implement.
Raise an issue if you miss a specific command to operate FDB on Kubernetes.
