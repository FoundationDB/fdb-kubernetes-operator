# Kubernetes reconciliation

In this document we will describe the basics of Kubernetes reconciliation and specifically in the context of the [FoundationDB operator](https://github.com/FoundationDB/fdb-kubernetes-operator).

## Reconciliation

Kubernetes has a declarative way to describe the desired state of resources.
Each resource is watched/controlled by a so called controller or operator.
If a resource gets created or updated the contoller will compare the desired state against the current state (reconcile).
If there is any difference the controller will take action to reach the desired state e.g. by creating/deleting Pods.
Even though the controller normally reacts based on watches (the Kubernetes API will send updates if a resource is changed) the controller will always compare the complete state.
This is also known as [level triggered](#further_reading) and has the benefit that we don't care to much about a missed event (which can easily happen in a distributed system).
Each controller can define for which resources it wants to be notified, normally this includes all resources that the controller "own" e.g. manages.
Besides the active watch a controller also periodically sync it's state (per default the synchronization period is `10h`) that means after the period is over the controller will compare the desired state with the current state for all resources managed by the controller.

## FoundationDB operator

The same pattern applies to the FoundationDB operator with a `FoundationDBCluster` manifest we describe the desired state of a FoundationDB cluster running in Kubernetes.
The FDB operator implements [multiple sub-reconciler](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/v0.33.0/controllers/cluster_controller.go#L123-L149) mostly for a better readability and to separate the individual tasks.
As an example there is a sub-reconciler that only handles the ConfigMap reconcilation and there is another sub-reconciler that deletes Pod.
Why should I care about these sub-reconcilers?
Each sub-reconciler will be called serially in the defined order.
During a reconciliation "step" the sub-reconciler can decide if the reconciliation can continue or not.
That means that a sub-reconciler can block the reconciliation from completion.

### How to debug a stuck reconciliation

A reconciliation can get stuck if the desired state can't be reached e.g. because an upstream resource won't be available (Pod is stuck in `Pending`).
There are two places in the `FoundationDBCluster` status that can provide further guidance on why the reconciliation is stuck:

- The [generations](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/v0.33.0/api/v1beta1/foundationdbcluster_types.go#L670) that describe the latest reconciled generation (if present) and any additional generations (and the reason) that are not fully reconciled. Most of the conditions are fairly well describing.
- The [process group conditions](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/v0.33.0/api/v1beta1/foundationdbcluster_types.go#L622-L642) that describes for each process group the current conditions. If a process group has no conditions that means that the process group is fully reconciled. In most cases a process group reflects a single instance but for the case with multiple Storage Servers per disk we would have multiple instances for a process group. The conditions should give you an idea which process group are blocking the reconciliation and why e.g. `IncorrectCommandLine` would indicate that the process is running with a different commandline than expected by the controller.

The following steps should help to get the reconciliation unstuck:

1. Check the `generations` to get an overview why the reconciliation is stuck.
1. Check the `process group conditions` to see which proces actually block the reconcilation.
1. Check the upstream resouces e.g. Pods (are they running).

### Examples

Assume you have a cluster `sample-cluster` with a stuck reconciliation.
You check the `generations` and find out that the latest generation has `hasUnhealthyProcess`.
In the next step you check the `process group conditions` and find out that the instance `sample-cluster-storage-1` has the condition `MissingProcesses`.
Now you can check the Pod for the instance with `kubectl get po -l fdb-instance-id=sample-cluster-storage-1,fdb-cluster-name=sample-cluster` (or you use `kubectl describe`).
If the Pod is in a `Running` state check the Node of the Pod for potential network issues and replace the instance.
If the Pod is in a `Terminating`, `Failed` or `Failed` state check the status of the Pod e.g. with `kubectl descibe` or `kubectl get po ... -oyaml` for the reason, depending on the reason contact the Kube team or replace the instance.

### Automatic replacements

The FDB operator supports [automatic replacements](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/v0.33.0/docs/cluster_spec.md#automaticreplacementoptions) which allows the operator to replace failed Pods automatically.
Currently the operator will only replace 1 Pod at a time and only if there are no other ongoing exclusions.
If the operator detects a process group that has the `MissingProcesses` for longer than 30 minutes (this is the default value) the operator will replace the process group by adding the ID to the `instancesToRemove` list.

#### How does it work

Once a Pod will fail or an instance is missing in the FDB status json the `process group` will be marked with the `MissingProcesses`.
During the next reconcilation (assuming that that at least 30 minutes have passed) the failed instance will be replaced.
There are a few things to note:

1. Only when a direct instance failes e.g. the Pod changes the state to `Failed` or something else a reconciliation will be triggered and the `MissingProcesses` will be added (otherwise we have to wait for the sync period).
1. The timestamp that is added to the conditions don't reflect the time these conditions happened rather when the controller discovered these conditions. That said the timestamp and the actual issue could be separated by ~10h (sync period).
1. The instance will only be removed when the reconciliation will be triggered again and the timestamp is older than 30 minutes (or any other defined valued).

This implies that in a worst case/bad scenario it can take `~20h` to actually replace a bad instance. Thanks to the data distribution of FDB that is normally not an issue but could still trigger alerts if e.g. a coordinator has failed in a way that doesn't trigger a reconciliation (e.g. network issue on the host).

## Further reading

- [Kubernetes: What is "reconciliation"](https://speakerdeck.com/thockin/kubernetes-what-is-reconciliation)
- [Edge vs. Level triggered](https://speakerdeck.com/thockin/edge-vs-level-triggered-logic)
- [Video: The Magic of Kubernetes Self-Healing Capabilities](https://www.youtube.com/watch?v=91dgNqma7-Q)
- [Writing a custom controller: Extending the functionality of your cluster](https://www.youtube.com/watch?v=U2Bm-Fs3b7Q)
