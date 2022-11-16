# e2e tests for FDB Kubernetes operator

This folder contains test suites to run e2e tests against a [kind](https://kind.sigs.k8s.io) or a real cluster.

## Running e2e tests

The current setup assumes that it can use an already running Kubernetes cluster.
If you don't have a running test cluster you can create one using [kind](https://kind.sigs.k8s.io)
**Important** if you use an already existing Kubernetes cluster the e2e tests will apply the latest CRDs from the current branch you are running them and afterwards they will be removed.


```bash
$ kind create cluster
$ kind get kubeconfig > ~/.kube/e2e_test
```

Now you can run the e2e tests against the local cluster:

```bash
go test -v ./e2e/... --tags=e2e_test --kubeconfig=${HOME}/.kube/e2e_test
```
