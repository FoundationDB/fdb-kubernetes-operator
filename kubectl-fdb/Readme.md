# Kubectl plugin for FDB on Kubernetes

Current status: *alpha*

## Installation

In order to install the latest version from the source code run:

```bash
make plugin
# move the binary into your path
export PATH="$PATH:$(pwd)/bin" 
```

## Usage

Run `kubectl fdb help` to get the latest help.

### Supported operations

- `exlcude` instances from a cluster.
- Print the `version` of the plugin and the operator.

### Planned operations

- Evacuate node: removes all instances on a node (e.g. maintenance)
- Status: show the status of the fdb cluster (e.g. `fdbcli status`)
- top: show the current resource utilization
- df: shows the current free/used disk capacity either for each instance or aggregated per class

Raise an issue if you miss a specific command to operate FDB on Kubernetes.