# Support bin packing of fault domains

## Metadata

* Authors: @johscheuer
* Created: 2021-10-01
* Updated: 2021-10-01

## Background

In the current implementation of the operator we try to distribute our Pod across as many fault domains as possible.
If a cluster has more fault domains than the operator creates Pods for a specific class all Pods will be on a different fault domain.
This has the drawback that the operator can only interact on a single Pod for safe operations e.g. to minimize the risk of data loss.
For clusters with a non-trivial amount of Pods this means that some operational tasks will take very long.
A better way would be to allow the operator to bin pack Pods in the same fault domain.

## General Design Goals

* Provide a way how a user can specify the number of logical fault domains
* The user should be able to decide if the number of logical fault domains is a requirement (default should be optional)

## Current Implementation

The current implementation adds a `preferredDuringSchedulingIgnoredDuringExecution` `PodAntiAffinity` to every Pod [code](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/master/internal/pod_models.go#L308-L334).
The label selector will contain the process class and the cluster name, to ensure processes with the same class are distributed across different fault domains is possible.
A user can define additional `PodAntiAffinity` but can't prevent the operator from createn the default `PodAntiAffinity`.
The only expection is the specival fault domain `foundationdb.org/none` and `foundationdb.org/kubernetes-cluster`.

## Proposed Design

The idea would be to use a custom label like `foundationdb.org/distribution-key`.
As value we would use the process number modulo the `DesiredFaultDomains` and a prefix for the `BinPack` mode.
In addition to that we have to change the `PodAntiAffinity` rule.
The following would be an example of the `affinity` term for a Pod.
The example assumes that process number modulo `DesiredFaultDomains` is `0` and we have set `BinPackAll`:

```yaml
  affinity:
    # Affinity to schedule Pods with the same distribution key together
    podAffinity:
      preferredDuringSchedulingIgnoredDuringExecution:
      - podAffinityTerm:
          labelSelector:
            matchLabels:
              foundationdb.org/distribution-key: "all-0"
          topologyKey: fault_domain
        weight: 1
    # AntiAffinity to not schedule the Pod on the same fault domain where other Pods with a different distribution key are running
    podAntiAffinity:
      preferredDuringSchedulingIgnoredDuringExecution:
      - podAffinityTerm:
          labelSelector:
            matchExpressions:
              key: "foundationdb.org/distribution-key"
              operator: NotIn
              values:
              - "all-0"
          topologyKey: fault_domain
        weight: 1
```

The additional label and the `affinity` term enable the operator to spread Pods across fault domains.
If `Required` is set to false this will be on a best effort base and can be useful if a fault domain is full but the Pod should be scheduled anyway.
The following code snippet shows a possible implementation of the required structs:

```golang
type BinPack string

const (
    // BinPackAll defines that processes independent of their class should be bin packed
    BinPackAll BinPack = "All"
    // BinPackStateful defines that stateful processes should be bin packed and additionally stateless processes
    BinPackStateful BinPack = "Stateful"
    // BinPackClass only processes of the same class will be bin packed
    BinPackClass BinPack = "Class"
)

// DistributionConfig
type DistributionConfig struct {
    // Enabled defines if the binpacking is enabled or not.
    // Default: false
    Enabled *bool
    // DesiredFaultDomains desfines the number of desired fault domain.
    // Should be greater than 0 is fault domain distribution is enabled.
    // Default: 0
    DesiredFaultDomains *int
    // BinPack defines what processes should be bin packed to the fault domains.
    // Default: "Class"
    BinPack *BinPack
    // Required defines if the affinity terms are required or preffered.
    // Default: false
    Required *bool
}
```

The operator won't guarantee that the fault domains are equally used.
Depending on the replacements for unreachable Pods we could use one fault domain more than another (or depending on the capacity),
We also don't guarantee that we use exactly the number of `DesiredFaultDomains` e.g. when we spawn less processes than `DesiredFaultDomains` or when a fault domain is out of capacity and we are not using the `Required` with `true`.
We also don't guarantee that the operator replaces any Pods that violate any constraint and we are not using the distribution with `Required` with `true` (`Required` with `true` would only ensure that we are not hurting the constraint).

A change to `DesiredFaultDomains` will lead to a migration of all existing Pods in order to honor the new affinity term.

### Reducing the number of DesiredFaultDomains

If we currently use `DesiredFaultDomains` and we have `4` fault domains `f0`, `f1`, `f2` and `f3` with a Pod in each fault domain and we reduce `DesiredFaultDomains` to `2` this will result in the following steps:

1. Initial state `P4` in `f0`, `P1` in `f1`, `P2` in `f2` and `P3` in `f3`.
1. We schedule 4 new Pods as migration.
1. Pod `P5` and `P7` will be scheduled in `f1` and `P6` and `P8`will be scheduled in `f0`.
1. Once the old Pods are excluded they will be removed.
1. We only use `f0` and `f1`.

### Increasing the number of DesiredFaultDomains

If we currently use `DesiredFaultDomains` and we have `2` fault domains `f0` and `f1` with two Pods in each fault domain and we increase `DesiredFaultDomains` to `4` this will result in the following steps:

1. Initial state `P4` and `P2` in `f0`, `P1` and `P3` in `f1`
1. We schedule 4 new Pods as migration.
1. `P5` in `f1`, `P6` in (new) `f2`, `P7` in (new) `f3` and `P8` in `f0`.
1. Once the old Pods are excluded they will be removed.
1. We will use `f0`, `f1`, `f2` and `f3` now.

## Related Links

* [Inter-pod affinity and anti-affinity](https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#inter-pod-affinity-and-anti-affinity)
* [Automatic replacements if fault-domain is violated](https://github.com/FoundationDB/fdb-kubernetes-operator/issues/499)
