# Restricting Pod Template Modifications

This document describes how operator administrators can limit which pod template fields CR authors are permitted to set.

## Background

The `podTemplate` field in `FoundationDBCluster` and `FoundationDBBackup` resources lets users customize the pods the operator creates.
Without restrictions, any user who can write these resources can instruct the operator to:

* Mount arbitrary Kubernetes Secrets as volumes, potentially exfiltrating credentials the operator has access to.
* Set `spec.serviceAccountName` to the operator's own service account, effectively granting that user the operator's RBAC permissions.
* Enable `hostNetwork`, `hostPID`, or `hostIPC` for privilege escalation.

These risks are most relevant in multi-tenant clusters where the operator manages resources across namespaces, but they apply whenever CR-write access is granted to less-trusted users.

The `AllowedPodModifications` feature lets the operator admin define an allowlist of what is permitted.
All restrictions are **opt-in**: the defaults are fully permissive so that upgrading the operator does not break existing configurations.

## Configuration

Restrictions are configured through operator startup flags.
Set these flags in the operator's `Deployment` manifest under `spec.template.spec.containers[].args`.

### Boolean flags

These flags each default to `true` (permitted). Set to `false` to forbid the corresponding field.

| Flag | Restricts | Default |
|------|-----------|---------|
| `--allow-automount-service-account-token` | `spec.AutomountServiceAccountToken: true` | `true` |
| `--allow-host-network` | `spec.HostNetwork: true` | `true` |
| `--allow-host-pid` | `spec.HostPID: true` | `true` |
| `--allow-host-ipc` | `spec.HostIPC: true` | `true` |

Setting a flag to `false` blocks the pod template from setting that field to `true`.
A field set to `false` (or not set at all) is always allowed, so disabling `--allow-host-network` still permits pods that do not use host networking.

### Allowlist flags

These flags accept zero or more values. When a flag is not set (empty list), no restriction is applied.
Once at least one value is specified, only the listed values are permitted.

| Flag | Restricts |
|------|-----------|
| `--allowed-service-account-names` | `spec.serviceAccountName` and `spec.deprecatedServiceAccount` |
| `--allowed-additional-volume-sources` | Volume source types in `spec.volumes` |
| `--allowed-additional-volume-mounts` | Volume mount names in `spec.containers[*].volumeMounts` and `spec.initContainers[*].volumeMounts` |

These flags may be repeated to build a list:

```
--allowed-additional-volume-sources=configMap
--allowed-additional-volume-sources=emptyDir
```

#### Volume source type names

Volume source type names use the Kubernetes JSON field name (camelCase), which is the field name as it appears in the `corev1.VolumeSource` struct tags. Common examples:

| Volume type | Key to use |
|-------------|-----------|
| ConfigMap | `configMap` |
| Secret | `secret` |
| EmptyDir | `emptyDir` |
| HostPath | `hostPath` |
| PersistentVolumeClaim | `persistentVolumeClaim` |
| Projected | `projected` |
| CSI | `csi` |

## Example

The following operator deployment restricts CR authors to `ConfigMap` and `EmptyDir` volumes, forbids host-level privileges, and limits service account names to a known set:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: fdb-kubernetes-operator-controller-manager
spec:
  template:
    spec:
      containers:
        - name: manager
          args:
            - --allow-host-network=false
            - --allow-host-pid=false
            - --allow-host-ipc=false
            - --allow-automount-service-account-token=false
            - --allowed-additional-volume-sources=configMap
            - --allowed-additional-volume-sources=emptyDir
            - --allowed-service-account-names=fdb-kubernetes
```

With this configuration, a `FoundationDBCluster` or `FoundationDBBackup` whose `podTemplate` specifies a Secret volume, `hostNetwork: true`, or an unlisted service account name will be rejected at reconciliation time, and a Kubernetes event will be recorded on the resource.

## Scope

The restriction is enforced in both reconcilers:

* `FoundationDBClusterReconciler` — validates every process class's pod template in `spec.processes`.
* `FoundationDBBackupReconciler` — validates `spec.podTemplateSpec` if present.

Validation runs at the start of each reconciliation loop, before any pod is created or updated.
A violation does not stop the operator from running; it produces a warning event and halts reconciliation for that resource until the pod template is corrected or the operator flags are updated.

## Next

You can continue on to the [next section](tls.md) or go back to the [table of contents](index.md).
