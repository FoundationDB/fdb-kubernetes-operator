# e2e tests for FDB Kubernetes operator

## Running the e2e tests

The following command will run all the operator related tests with the default values:

```bash
make -C e2e -kj run
```

You can also run a specific test suite by providing the name of the test suite:

```bash
make -C e2e -kj test_operator.run
```

Every test suite will create at least one namespace, HA cluster tests will create all the required namespaces.

### Running e2e tests in kind

[kind](https://kind.sigs.k8s.io) provides an easy way to run a local Kubernetes cluster.

```bash
kind create cluster
# This command assumes to be executed from the project root.
make container-build
# Push the image into the kind cluster.
kind load docker-image "fdb-kubernetes-operator:latest"
```

Before you run the e2e tests you have to ensure that the latest CRDs for the operator are installed:

```bash
# This command should be executed from the project root
kubectl apply -f ./config/crd/bases/
```

If you want to run all tests, including tests that inject chaos you have to install [chaos mesh](https://chaos-mesh.org):

```bash
chaos_mesh_version="2.5.0"

kubectl create ns chaos-testing || true
helm repo add chaos-mesh https://charts.chaos-mesh.org
helm repo update

# The configuration below is tested on a local Kind installation and might be different for the target Kubernetes cluster.
helm upgrade -i chaos-mesh chaos-mesh/chaos-mesh \
    --namespace chaos-testing \
    --set dashboard.securityMode=false \
    --set chaosDaemon.socketPath=/run/containerd/containerd.sock \
    --set chaosDaemon.runtime=containerd \
    --version "${chaos_mesh_version}"

# Check if the Pods are running
kubectl wait --for=condition=ready pods --namespace chaos-testing -l app.kubernetes.io/instance=chaos-mesh
```

The actual installation steps might be different based on your Kubernetes cluster.
