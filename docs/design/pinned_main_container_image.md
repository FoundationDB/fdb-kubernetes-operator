# Pinned Main Container Image with Shared-Binary Upgrades

## Metadata

* Authors: @johscheuer
* Created: 2026-10-13
* Updated: 2026-10-13

## Background

With the unified image, every Pod runs two containers from the same image family:

* The main container runs `fdb-kubernetes-monitor` in launcher mode, which starts `fdbserver`.
* The sidecar container runs `fdb-kubernetes-monitor` in sidecar mode. It copies `fdbserver` (and `fdbcli`) from its
  own image into the shared volume at `/var/fdb/shared-binaries/bin/<version>/`.

Today an FDB version upgrade always changes the main container image to the new version's tag, and the monitor decides
which binary to run based on protocol compatibility with the version baked into the main container image:

* Protocol-compatible (patch) upgrade: `updatePods` deletes and recreates every Pod whose spec hash changed (new main
  image tag). The monitor runs the local `fdbserver`.
* Protocol-incompatible upgrade: `updatePods` is paused while the upgrade is running, `updateSidecarVersions` patches
  only the sidecar image in place, the sidecar stages the new binary, and `bounce_processes.go` restarts the processes
  so the monitor launches the shared binary. Once the upgrade completes `updatePods` resumes and recreates all Pods to
  converge the main image tag.

The recreation at the end of every upgrade is expensive and time-consuming, especially for PVC-backed storage and transaction Pods (volume reattachment, an additional wait-for-healthy pass, additional recoveries).

## General Design Goals

* Offer an opt-in upgrade mode in which the main container image does not change when the FDB version changes, and
  `fdbserver` always runs from the binaries staged by the sidecar.
* Keep the current behavior as the default; clusters that do not set the new field are unaffected.
* Reuse the existing sidecar staging, `updateSidecarVersions` and coordinated bounce machinery.
* Make updating the main image (OS/security patches, monitor fixes) an explicit, user-driven rolling update, not a
  side effect of FDB upgrades.
* Avoid hidden drift: the pinned image is declared in the spec and visible, not inferred.

## Non-Goals

* Changing the default upgrade behavior.
* Supporting the split image type (see Validation).
* Defining an automated policy for rolling the pinned image forward. This stays in the user's hands.

## Current Implementation

Relevant code, verified against the current tree:

* `internal/pod_models.go`: `GetImage` resolves the container image from `cluster.Spec.Version` through
  `SelectImageConfig`. The sidecar is configured with `--main-container-version <desiredVersion>`
  (`pod_models.go:299`).
* `fdb-kubernetes-monitor` `main.go:189`: in sidecar mode the copy is **skipped** when
  `--main-container-version == currentContainerVersion` (the sidecar's own `/var/fdb/version`). Output is written to
  `bin/<currentContainerVersion>/`.
* `fdb-kubernetes-monitor` `monitor.go:258` (`readConfiguration`): if
  `currentContainerVersion.IsProtocolCompatible(configuration.Version)` the binary is `--fdbserver-path`; otherwise it
  is `<shared-binary-dir>/<version>/fdbserver`. If the chosen binary fails `checkOwnerExecutable`, the configuration is
  rejected and no process is started.
* The monitor reloads configuration only on a config file change or a newer Pod timestamp from the Pod client feed. It
  does not watch the shared binary directory.
* Nothing in the monitor removes old version directories from the shared volume.
* `kubernetes.go:233` derives `BINARY_DIR` from the active `BinaryPath`, so the environment already follows the
  binary that is actually running.

## Proposed Design

### 1. API

Add an optional field to the main container configuration, for example:

```yaml
spec:
  version: 7.4.6
  imageType: unified
  mainContainer:
    monitorVersion: 7.3.63
```

The field comment in the API types should read along these lines:

```go
// MonitorVersion pins the FDB version of the image used for the main container, which runs fdb-kubernetes-monitor.
// The value must be a valid FDB version and selects the unified image tag for the main container. When set,
// fdbserver is always run from the binaries staged by the sidecar for spec.version, and the main container image
// does not change when spec.version changes. This image also contains fdbcli and other binaries of the pinned
// version, which can differ from the version of the running fdbserver. Only supported with the unified image type.
// +optional
MonitorVersion *string `json:"monitorVersion,omitempty"`
```

Semantics:

* Unset: today's behavior.
* Set: the main container image is resolved from `monitorVersion` instead of `spec.version`. The sidecar image is still
  resolved from `spec.version`, as it is the carrier of the binaries for the desired version.

The sidecar image is still resolved from `spec.version`, so it changes on every FDB upgrade. The Pod spec hash
(`internal/pod_helper.go:GetPodSpecHash`) covers the whole PodSpec and is compared with the Pod's `LastSpecKey`
annotation by `updatePods`. Without further changes, a version bump would therefore still change the hash and recreate
every Pod, and the in-place sidecar patch does not update the annotation. To make the pod spec stable across FDB
upgrades, pinned mode treats the sidecar image as in-place managed state instead of part of the spec hash (see
"Spec hash and sidecar image" in the operator changes):

* The main container image is fixed by `monitorVersion` and is part of the hash.
* The sidecar image is excluded from the hash and is converged in place by `updateSidecarVersions`.

With both in place, the hash is stable across FDB upgrades and `updatePods` does not recreate Pods when only
`spec.version` changes.

Changing `monitorVersion` itself is a normal Pod spec change and goes through the regular `updatePods` rolling update,
which is how users roll in base-image and monitor fixes.

### 2. Monitor changes (`fdb-kubernetes-monitor`)

1. **Always use the shared binary, driven by the monitor configuration.** Add an optional field to
   `ProcessConfiguration` (`api/config.go`), for example `useSharedBinaries *bool` (`json:"useSharedBinaries,omitempty"`).
   When it is true, `readConfiguration` skips the protocol-compatibility branch and always uses
   `<shared-binary-dir>/<version>/fdbserver`. When it is nil or false, behavior is unchanged. Without this, a patch
   upgrade would keep running the baked-in binary.

   Using the configuration instead of a new CLI flag has these advantages:
   * The monitor already reloads the configuration online (file watch and Pod timestamp feed), so the behavior can be
     toggled without restarting the main container. The change takes effect on the next bounce.
   * The binary choice and the version switch change in the same configuration update, so they cannot disagree.
   * The operator already regenerates this configuration on every reconcile (`internal/monitor_conf.go`,
     `internal/configmap_helper.go`), so it stays in sync with the cluster spec.

   The sidecar reads the same configuration (see monitor change 2), so no additional CLI flag or setting is needed to
   switch the copy behavior.
2. **Sidecar reads the process configuration.** In sidecar mode the monitor parses only `version` and
   `useSharedBinaries` from the configuration, using the existing `--input-dir` and `--input-monitor-conf` flags. The
   configuration is read only if `--input-monitor-conf` was set explicitly (`pflag` `Changed`), so the flag defaults
   (`.` and `config.json`) can never make an existing setup read an unrelated file. Behavior:
   * At start, copy the binaries if `useSharedBinaries` is true, or if `--main-container-version` differs from the
     sidecar's own version (today's condition).
   * Keep watching the configuration (reusing the launcher's file watch) and copy once when `useSharedBinaries`
     becomes true. `copyFiles` writes a temp file and renames it, so copying at any time, or repeatedly, is safe.
   * A configuration that is missing or cannot be parsed must never terminate the sidecar, because an exit restarts
     the container. Log the error and wait for the next change.
   * The output directory stays `bin/<sidecar version>/`, so the launcher finds the binary where it expects it.
3. **Handle a late-arriving binary.** Today a missing binary only causes the configuration to be rejected until the
   next trigger. In the new mode this is the normal state at Pod start (the sidecar may still be copying). The monitor
   should retry loading the configuration with a bounded backoff, or watch the version directory, when the failure is
   "binary does not exist yet". Partial reads are not a concern since `copyFile` writes to a temp file and renames.
4. **Prune unused versions.** In the new mode, remove version directories under the shared binary dir that are neither
   the desired version nor the version of a currently running `fdbserver`. This bounds the volume size for long-lived
   Pods. Pruning must never remove a directory a running process was started from, and must not run while a bounce
   to a new version is pending.
5. **Confirm the mode is honored.** `json.Unmarshal` ignores unknown fields, so an older monitor (launcher or
   sidecar) would silently ignore `useSharedBinaries`. Both should report what they actually do: the launcher the
   binary path it launched, the sidecar whether the binaries for its version were staged (for example a
   `uses_shared_binaries` indicator as a Prometheus label/gauge and a Pod annotation), so the operator can verify it
   instead of assuming.
6. The launcher's `currentContainerVersion` remains the baked-in version and is still used for reporting. The existing
   `running_version`/`desired_version` metrics already report the version actually running.

### 3. Operator changes

1. **Image resolution.** In `internal/pod_models.go`, resolve the main container image from `monitorVersion` when set.
2. **Sidecar configuration (gated on `monitorVersion`).** The sidecar currently receives
   `--main-container-version <spec.version>` and therefore skips copying when its own version equals it, which is
   always the case in the steady state. When `monitorVersion` is set, the operator additionally:
   * mounts the `config-map` volume (`/var/dynamic-conf`) into the sidecar container, and
   * passes the existing `--input-dir` and `--input-monitor-conf` flags, using the same values as the main
     container, so the sidecar reads `useSharedBinaries` from the process configuration (monitor change 2).

   No new flag is introduced. The sidecar then copies based on the configuration, so the operator does not have to
   compute `--main-container-version` for this mode.

   **Requirement:** when `monitorVersion` is unset, the sidecar and main container specs must be byte-for-byte
   identical to today's. The Pod spec hash includes container args and volume mounts, so any unconditional change
   would make `updatePods` roll every unified-image cluster on operator upgrade, without the users opting in. A unit
   test in `internal/pod_models_test.go` should assert that the Pod spec (or its hash) for a unified cluster without
   `monitorVersion` is unchanged.
3. **Monitor configuration.** Set `useSharedBinaries: true` in the generated process configuration when
   `monitorVersion` is set. Because the configuration is not part of the Pod spec hash, changing it does not recreate
   Pods. The monitor retries until the binary exists (monitor change 3), so the operator does not need to wait for the
   copy, but it should check the monitors' confirmation (monitor change 5) before relying on the mode.
4. **Spec hash and sidecar image.** When `monitorVersion` is set:
   * `GetPodSpecHash` hashes a copy of the PodSpec in which the sidecar container's image is cleared. The hash used by
     `updatePods` (`update_pods.go:345`) and by the status check (`update_status.go:782`) goes through the same
     function, so both stay consistent. Everything else, including the main container image, remains part of the hash.
   * `updateSidecarVersions` no longer returns early for non-incompatible upgrades. In pinned mode it converges the
     sidecar image of every Pod in place whenever it differs from `GetSidecarImage` (the existing
     `container.Image != image` check in `updateSidecarImage`), so it also covers compatible upgrades and recovers
     after an operator restart. Patching the sidecar image restarts only the sidecar container, which re-stages the
     binary for its version.
   * New Pods are created from the full spec, so they start with the current sidecar image.

   Since the sidecar image is no longer hashed, a change to the sidecar image (for example a new `imageConfigs`
   entry) is rolled out by the in-place patch and never by Pod recreation. This is intended for this mode.

   When `monitorVersion` is unset, the hash input is unchanged (see the requirement in operator change 2).
5. **Upgrade flow.** Reuse the incompatible-upgrade flow for all upgrades in pinned mode: `updateSidecarVersions`
   patches the sidecar image in place, then `bounce_processes.go` performs the coordinated restart. Compatible upgrades
   in pinned mode should also use the in-place sidecar patch and bounce instead of the Pod recreation path, since the
   main image does not change. `IsBeingUpgradedWithVersionIncompatibleVersion` and the places that gate on it need an
   audit for this. How compatible upgrades are restarted (rolling per process group versus the coordinated bounce used
   for incompatible upgrades) is an open question, see below.
6. **Status/observability.** Expose the pinned version in the cluster status (or a condition) so `kubectl fdb` and
   metrics can show how far the main image lags behind `spec.version`. A warning event or condition when the gap
   exceeds a configurable number of versions is optional.

### 4. Validation

* Reject `monitorVersion` when the image type is `split`. In the split image the sidecar is what the old
  `fdbmonitor` setup depends on and the model does not apply.
* Require `monitorVersion` to be a valid FDB version that the operator can resolve to an image.
* Require that `monitorVersion` contains a monitor that supports `useSharedBinaries`. An older monitor ignores the
  unknown JSON field without error; document the minimum version and rely on the monitor's confirmation (monitor
  change 5) to detect this. This applies to both the launcher and the sidecar image.
* Consider requiring `spec.version >= monitorVersion`, to avoid downgrades running with an older monitor than expected.

### 5. Online toggling

Since both the launcher and the sidecar read `useSharedBinaries` from the process configuration, the behavior itself is
toggled without Pod restarts:

* **Online:** the launcher's choice of binary and the sidecar's decision to copy. The sidecar copies when the field
  becomes true, and the launcher picks the shared binary at the next bounce.
* **Not online:** the Pod spec changes that come with the mode. Mounting the config into the sidecar, passing the
  existing input flags, and moving the main image to `monitorVersion` are Pod spec changes and roll Pods once.

After that single rollout, enabling or disabling `useSharedBinaries` is only a configuration change. Only a change of
`monitorVersion` itself rolls Pods again.

### 6. Migration

* Operator upgrade: no effect on existing clusters. Without `monitorVersion` the generated Pod specs are unchanged
  (see operator change 2), so no rollout is triggered.
* Enabling the mode on an existing cluster: set `monitorVersion` to the currently running version. The Pod spec changes
  (sidecar mount and flags, and the image if it differs), so this causes one regular rolling update, triggered by the
  user. This update also switches the hash to the pinned-mode formula (sidecar image excluded), which is part of the same
  rollout. Once Pods have the new spec, the operator sets `useSharedBinaries` in the process configuration, the sidecar
  stages the binary, and the monitors confirm. After that, upgrades no longer recreate Pods.
* Disabling the mode: unset the field. The operator first clears `useSharedBinaries` (online, takes effect at the
  next bounce) and then the Pod spec moves back (image to `spec.version`, sidecar mount and flags removed) via the
  regular rolling update.

## Risks and Open Questions

* **Cold start dependency.** `fdbserver` startup now depends on the sidecar having staged its binary. This adds start
  latency and makes sidecar failures a hard failure for the process, not only for upgrades. Mitigation: the retry in
  the monitor, plus a clear log line and metric for "waiting for binary".
* **Image hygiene.** The main image is no longer refreshed by FDB upgrades. Documentation must say that users are
  responsible for bumping `monitorVersion`, and that FDB upgrades alone will not deliver base-image security fixes.
* **Tooling version skew.** Other binaries in the main image (`fdbcli`, `fdbbackup`, plugin exec paths) are from the
  pinned version, not the running server version. Needs an audit of all consumers; `fdbcli` from the shared directory
  is already copied by the sidecar and may be preferable.
* **Shared volume size.** Old binaries accumulate until pruned (see monitor change 4). The volume size and its
  `sizeLimit` need to account for at least two versions during an upgrade.
* **Multi-operator `SynchronizationMode`.** The field is part of the spec, so it should be consistent across operator
  instances; verify nothing derives the main image locally.
* **`IgnoreLogGroupsForUpgrade`** and other upgrade-related options should be checked for assumptions that the main
  image tracks `spec.version`.
* **Open question: restart of compatible upgrades.** Incompatible upgrades use a coordinated bounce of all processes.
  A compatible (patch) upgrade is a rolling Pod replacement today. Pinned mode must restart processes on the new
  binary without Pod recreation, and should do so in a rolling fashion, not as a simultaneous bounce, to avoid an
  unnecessary recovery. This needs a concrete design (per process group bounce with the existing safety checks) before
  implementation.
* **Relation to the deferral draft.** This design makes the deferral draft unnecessary for users who opt in. The
  deferral draft remains relevant only for users who want to keep the default model. Decision: close the deferral draft
  if this one is accepted.

## Implementation Plan

1. **Monitor:** `useSharedBinaries` configuration field, sidecar reading the configuration via the existing input
   flags, retry on missing binary, pruning, and confirmation of the launched binary path / staged binaries, with unit
   tests in `monitor_test.go`, `copy_test.go` and `api/config_test.go`.
2. **API and validation:** new field, deepcopy/CRD generation, validation tests (unified only).
3. **Operator wiring:** image resolution, spec hash without the sidecar image and in-place sidecar convergence (only
   when `monitorVersion` is set), sidecar config mount and input flags (only when `monitorVersion` is set),
   `useSharedBinaries` in the process configuration (gated on monitor confirmation), upgrade flow audit; unit tests
   in `internal/pod_models_test.go` and `internal/pod_helper_test.go` (including that specs and hashes without
   `monitorVersion` are unchanged, and that with `monitorVersion` the hash is identical for two different
   `spec.version` values) and the controller tests for upgrades (`updatePods` queues no Pod for deletion after a
   version change; `updateSidecarVersions` patches the sidecar for a compatible upgrade).
4. **E2E:** patch upgrade and incompatible upgrade with a pinned main image, verifying no Pod recreation, correct
   running version, and shared volume pruning. Include a test that restarts a Pod mid-upgrade.
5. **Docs:** manual section on the mode, trade-offs and the process for rolling the pinned image forward.

## Related Links

* [Design on Combining the Main Container and the Sidecar Container on Kubernetes](unified_sidecar_and_process_monitor.md)
