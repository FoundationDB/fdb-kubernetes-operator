# Pinned Main Container Image with Shared-Binary Upgrades

## Metadata

* Authors: @johscheuer
* Created: 2026-10-13
* Updated: 2026-11-05

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

The recreation at the end of every upgrade is expensive and time-consuming, especially for PVC-backed storage and
transaction Pods (volume reattachment, an additional wait-for-healthy pass, additional recoveries).

## General Design Goals

* Offer an opt-in upgrade mode in which the main container image does not change when the FDB version changes, and
  `fdbserver` runs from the binaries staged by the sidecar.
* Keep the current behavior as the default; clusters that do not set the new field are unaffected.
* **Require no change in `fdb-kubernetes-monitor`.** The mode must work with the monitor versions that are already
  released, so that it is available for older FDB versions without releasing new images. Future monitor versions will
  simplify the mode (see "Future monitor versions").
* **Enabling the mode must not roll any Pod.** A rollout is only acceptable when it is needed by an upgrade that is
  being started, and it must never be more expensive than the rollout the upgrade would cause today.
* Reuse the existing sidecar staging, `updateSidecarVersions` and coordinated bounce machinery.
* Make updating the main image (OS/security patches, monitor fixes) an explicit, user-driven rolling update, not a
  side effect of FDB upgrades.
* Avoid hidden drift: the pinned image is declared in the spec and visible, not inferred.

## Non-Goals

* Changing the default upgrade behavior.
* Supporting the split image type (see Validation).
* Defining an automated policy for rolling the pinned image forward. This stays in the user's hands.
* Online toggling of the mode without any Pod spec change (this needs a newer monitor, see "Future monitor versions").

## Current Implementation

Relevant code, verified against the current trees (operator and `fdbkubernetesmonitor`):

* `internal/pod_models.go`: `GetImage` resolves the container image from `cluster.Spec.Version` through
  `SelectImageConfig`. The sidecar is configured with `--main-container-version <desiredVersion>`
  (`configureContainersForUnifiedImages`).
* `main.go` (monitor): in sidecar mode the copy is **skipped** only if the string `--main-container-version` equals
  `currentContainerVersion` (`mainContainerVersion != currentContainerVersion`). `currentContainerVersion` is read from
  the file given by `--version-file` (default `/var/fdb/version`) once at process start. Output is written to
  `bin/<currentContainerVersion>/` (`copy.go`).
* `monitor.go` (`readConfiguration`): if `currentContainerVersion.IsProtocolCompatible(configuration.Version)` the binary
  is `--fdbserver-path`; otherwise it is `<shared-binary-dir>/<version>/fdbserver`. Protocol compatibility is equality
  of major, minor and release candidate. `currentContainerVersion` is used for nothing else in the launcher. If the
  chosen binary fails `checkOwnerExecutable`, the configuration is rejected (`nil`) and no process is (re)started.
* The launcher reloads the configuration only on a file event for the config file or a newer Pod timestamp from the Pod
  client feed. It does not retry on a timer and does not watch the shared binary directory.
* Nothing in the monitor removes old version directories from the shared volume.
* Container args are immutable on an existing Pod; only the container image (and metadata such as annotations) can be
  patched in place.
* All flags used by this design (`--version-file`, `--main-container-version`, `--fdbserver-path`) were added together
  with the sidecar mode (monitor commit `c7858d2441`), so every image that supports the unified sidecar has them.

## Key Observations

Two existing behaviors give us what we need without a monitor change:

1. **The sidecar copy decision is a plain string comparison against a flag the operator controls.** A pre-existing Pod
   keeps the `--main-container-version` it was created with. If its sidecar image is patched in place to a different
   version, the sidecar copies. This is exactly how the incompatible-upgrade flow works today.
2. **The launcher's binary choice depends only on `--version-file`.** Pointing `--version-file` at a file with a version
   that is never protocol compatible (for example `0.0.0`) makes the launcher always use the shared binary. The file
   can live in the ConfigMap that is already mounted at `/var/dynamic-conf`.

Observation 1 is enough for upgrades that are **not protocol compatible**, and it works on Pods that were created before
the mode was enabled. Observation 2 is needed for **protocol compatible (patch)** upgrades and requires an additional
container arg, which an existing Pod cannot get without being recreated.

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
// the main container image does not change when spec.version changes, and fdbserver is run from the binaries staged
// by the sidecar whenever spec.version is not the version of the main container image. This image also contains
// fdbcli and other binaries of the pinned version, which can differ from the version of the running fdbserver.
// Only supported with the unified image type.
// +optional
MonitorVersion *string `json:"monitorVersion,omitempty"`
```

Semantics:

* Unset: today's behavior.
* Set: the main container image is resolved from `monitorVersion` instead of `spec.version`. The sidecar image is still
  resolved from `spec.version`, as it is the carrier of the binaries for the desired version.
* If `spec.version == monitorVersion`, the cluster may run the binary baked into the main image. This is fine and
  needs no staging.

Changing `monitorVersion` itself is a normal Pod spec change and goes through the regular `updatePods` rolling update,
which is how users roll in base-image and monitor fixes.

### Rolling the pinned image forward together with an upgrade

If the user sets `monitorVersion` to the same value as `spec.version`, e.g. in the same change that upgrades the
cluster, the image is not pinned to a different version anymore and the cluster is upgraded like a cluster without
`monitorVersion`:

* The images of both containers follow the Pod version: the running version during a version incompatible upgrade, the
  new version during a protocol compatible upgrade. Pods that are created during the upgrade get the same images as
  without the feature.
* The sidecar image is not updated in-place for compatible upgrades and the process version check keeps the behavior
  for compatible upgrades, because the Pods are replaced.
* The main container image changes, so the hash changes and `updatePods` (or the replacement strategy) recreates the
  Pods: right away for a compatible upgrade, after the upgrade for an incompatible one.
* Pods that are capable of starting the sidecar binary are replaced as well, because the main container image differs.

Afterwards `monitorVersion` equals the version of the cluster and the next upgrade is in-place again.

### 2. Pod specification in pinned mode

Everything here is gated on `monitorVersion` being set. When it is unset the generated Pod spec must be byte-for-byte
identical to today's (see the requirement under "Migration").

* **Main container image:** resolved from `monitorVersion`.
* **Sidecar image:** resolved from `spec.version`, converged in place (see 4).
* **Sidecar `--main-container-version`:** set to `monitorVersion` instead of `spec.version`. This keeps the arg stable
  across FDB upgrades, which keeps the Pod spec stable. The sidecar copies whenever its image version differs from
  `monitorVersion` and skips the copy when they are equal (then the baked binary is the right one).

This is the full Pod spec change that is needed for incompatible upgrades. For a cluster where `monitorVersion` is set
to the currently running version, it is identical to today's spec, so enabling the mode does not change any Pod.

Patch upgrades additionally need a "pinned-capable" Pod, which has these extra pieces in its spec:

* A key in the cluster ConfigMap containing a constant version that is never protocol compatible, for example `0.0.0`.
  The main container gets `--version-file /var/dynamic-conf/<key>`. The file is constant, so the launcher always uses
  `<shared-binary-dir>/<version>/fdbserver`.
* The sidecar gets no `--main-container-version` flag at all. The flag defaults to the empty string, which never equals
  the sidecar's version, so the sidecar always copies. This needs no placeholder value in the Pod spec. Otherwise a
  version equal to `monitorVersion` would skip the copy and a launcher with the fake version could not find its binary.

The only placeholder is the content of the version file, and it lives in the ConfigMap, not in the Pod spec. Its content
is read only when the launcher starts. The operator can therefore change it without rolling Pods, but it must stay
incompatible with every `spec.version`, because a container restart would otherwise pick it up.

**`--version-file` must only be set on the main container, never on the sidecar.** The monitor also uses the version
file in sidecar mode to build the output directory (`bin/<version>`). A fake value there would stage the binaries into
`bin/0.0.0/` and the launcher would not find them. The sidecar must keep reading its own image's `/var/fdb/version`. A
unit test in `internal/pod_models_test.go` must assert that the sidecar args never contain `--version-file`.

Pods are pinned-capable only if they were created with these args. The operator identifies capable Pods by inspecting
the main container args of the Pod (not by the hash). When are Pods created capable?

* Pods created while `spec.version == monitorVersion` use the non-capable spec (identical to today's). This is what
  keeps the enable step free of rollouts.
* Pods created while `spec.version != monitorVersion` use the capable spec.

Since a Pod is only created (or recreated) for a spec change, the capable spec is only introduced by a rollout that
an upgrade needs anyway (see 5).

**Capability is one-way.** The fake version file works for any `spec.version`, so a capable Pod is a valid Pod for both
the capable and the non-capable desired spec. The desired spec therefore must not make a capable Pod roll when
`spec.version` changes back to `monitorVersion` (downgrade) or when an incompatible upgrade follows. `updatePods`
accepts a capable Pod when its hash matches the desired spec of either type (modulo the sidecar image, see 3). Only a
non-capable Pod that needs the capable spec (a patch upgrade with `spec.version != monitorVersion`) is rolled. The
fake version file is needed only for patch upgrades; incompatible upgrades work on both Pod types.

### 3. Spec hash and sidecar image

The Pod spec hash (`internal/pod_helper.go:GetPodSpecHash`) covers the whole PodSpec and is compared with the Pod's
`LastSpecKey` annotation by `updatePods`. The sidecar image changes on every upgrade and the in-place patch does not
update the annotation, so pinned mode must treat the sidecar image as in-place managed state:

* `GetPodSpecHash` hashes a copy of the PodSpec in which the sidecar container's image is cleared. The hash used by
  `updatePods` (`update_pods.go`) and by the status check (`update_status.go`) goes through the same function.
* The sidecar `--main-container-version` and the `--version-file` args must be part of the hash only through the Pod
  type (non-capable / capable), not through `spec.version`. Because the value is `monitorVersion`, absent, or a
  constant path, this is the case.

Since the hash formula changes for pinned mode, existing Pods have a `LastSpecKey` that does not match the new formula
even though their spec is unchanged. The operator must not interpret that as a spec change:

* **Re-stamp on enable.** In pinned mode, for a Pod whose `LastSpecKey` equals the legacy (full) hash of the desired
  spec, the operator patches the annotation to the pinned-mode hash. Annotations are mutable, so this is a metadata
  patch and no Pod is recreated. The "legacy hash matches" condition ensures that a real spec change is not hidden.
* Enabling the mode should happen before an upgrade is started. If it is enabled together with a version change the
  legacy hash does not match and the regular update path applies (a rollout, as without the feature).

When `monitorVersion` is unset, the hash input is unchanged.

### 4. Operator changes

1. **Image resolution.** In `internal/pod_models.go`, resolve the main container image from `monitorVersion` when set.
2. **Sidecar args** as described in 2.
3. **ConfigMap.** Add the constant version file key when `monitorVersion` is set. Adding a key to the shared ConfigMap
   does not change any Pod spec.
4. **Sidecar convergence.** `updateSidecarVersions` no longer returns early for non-incompatible upgrades. In pinned
   mode it converges the sidecar image of every Pod in place whenever it differs from `GetSidecarImage` (the existing
   `container.Image != image` check in `updateSidecarImage`), so it also recovers after an operator restart. Patching
   the sidecar image restarts only the sidecar container, which stages the binary for its version.
5. **Upgrade flow** (see next section).
6. **Status/observability.** Expose the pinned version in the cluster status (or a condition) so `kubectl fdb` and
   metrics can show how far the main image lags behind `spec.version`. A warning event or condition when the gap
   exceeds a configurable number of versions is optional.
7. **Verification instead of monitor confirmation.** The operator verifies the mode with information it already has:
   the version the processes report in `fdbcli status` / `running_version` after the bounce, which must equal
   `spec.version`. A mismatch after the bounce blocks the upgrade and surfaces as a condition.

### 5. Upgrade flow

The flow depends on whether the upgrade is protocol compatible and on whether the Pods are pinned-capable.

| Upgrade | Pods | Behavior |
|---|---|---|
| Incompatible | any (capable or not) | Existing incompatible flow: patch the sidecar image in place, the sidecar stages the binary, coordinated bounce. At the end of the upgrade `updatePods` finds no hash change (main image pinned, sidecar image not hashed) and recreates nothing. **No rollout.** |
| Compatible | pinned-capable | Same in-place flow: sidecar patch, then bounce. **No rollout.** |
| Compatible | not capable | The launcher would keep running the baked binary after the bounce, so the in-place flow is **not allowed**. The regular rolling update is used: new Pods are created with the capable spec and the new sidecar image. This is the same cost as a patch upgrade without the feature. All later patch upgrades are in place. |

Notes:

* In a mixed cluster (some Pods capable, some not), the operator decides per Pod. A Pod that has to be replaced to
  become capable has the `IncorrectPodSpec` condition and is excluded from the bounce. It is recreated by `updatePods`
  (or replaced, depending on the `PodUpdateStrategy`) and starts with the new version.
* The "compatible and not capable" case must be detected explicitly. Otherwise the sidecar would copy the new binary
  while the launcher silently restarts the old one, and the cluster would never converge.
* **Implementation.** In-place compatible upgrades are restarted through the regular bounce path, like any other
  configuration change that requires a restart. `updateSidecarVersions` runs for every upgrade in pinned mode
  (`UpdatesSidecarInPlace`). The process version check is not skipped for compatible upgrades in pinned mode, so every
  process with the old version has the `IncorrectCommandLine` condition. The bounce waits for the conditions
  `IncorrectSidecarImage`, `IncorrectConfigMap`, `IncorrectPodSpec` and `SidecarUnreachable` to be absent. The
  `IncorrectConfigMap` condition is only cleared once the monitor accepted the configuration of the new version, which
  the monitor only does if the staged binary exists. All processes that are ready are killed in a single call. The
  operator does not use the upgrade lock for compatible upgrades.
* The Pod spec of a Pod that requires the shared binary launcher (`UseSharedBinaryLauncher`) differs in the
  `--version-file` argument of the main container, the `shared-binary-version` item of the `config-map` volume and the
  missing `--main-container-version` argument of the sidecar. These settings, together with the sidecar image, are
  removed before the hash is calculated. `PodHasDesiredSpec` compares the hash and additionally requires a capable
  Pod if the desired spec requires it. It is used by `updatePods`, the status check and the replacements.

### 6. Validation

* Reject `monitorVersion` when the image type is `split`. In the split image the sidecar is what the old
  `fdbmonitor` setup depends on and the model does not apply.
* Require `monitorVersion` to be a valid FDB version that the operator can resolve to an image.
* Require that the images of `monitorVersion` and of `spec.version` are unified images that contain the flags listed in
  "Current Implementation" (all unified images do).
* Consider requiring `spec.version >= monitorVersion`, to avoid downgrades running with an older monitor than expected.
* There is no restriction on the relation between `spec.version` and `monitorVersion`. Versions that are protocol
  compatible with `monitorVersion` use the shared binary launcher, see 2.

### 7. Migration

* **Operator upgrade:** no effect on existing clusters. Without `monitorVersion` the generated Pod specs and hashes are
  unchanged. A unit test in `internal/pod_models_test.go` must assert this for a unified cluster without
  `monitorVersion`, since any unconditional change to args or mounts would roll every unified-image cluster on
  operator upgrade.
* **Enabling the mode:** set `monitorVersion` to the currently running `spec.version`. The desired Pod spec equals the
  existing one, the operator re-stamps the hash annotation (see 3), and **no Pod is rolled**. If `monitorVersion`
  differs from the running version, the main image changes and this is a regular user-triggered rolling update.
* **First upgrade after enabling:**
  * incompatible: in place, no rollout.
  * compatible: one regular rolling update (the Pods become pinned-capable), same cost as today.
* **Later upgrades:** in place, no rollout.
* **Disabling the mode:** unset the field. The desired spec moves back (main image to `spec.version`, sidecar arg to
  `spec.version`, version file removed), which is a regular rolling update. Because of the sidecar image handling in
  3, the operator must treat the hash formula change as it does when enabling (re-stamp when only the formula
  changed, otherwise roll).

## Future monitor versions

None of the following is required by this design. Future `fdb-kubernetes-monitor` releases will add features that
remove limitations of the operator-only design. The minimum monitor version that contains them is not yet defined and
must be filled in here once released.

1. **`useSharedBinary` in `ProcessConfiguration`.** The launcher always starts `<shared-binary-dir>/<version>/fdbserver`
   when the generated process configuration sets `useSharedBinary: true`, even if the version is protocol compatible
   with the main container. With it:
   * the fake `--version-file`, the capable/non-capable Pod types and the one-time rollout of the first patch
     upgrade are not needed (see 2),
   * the behavior can be toggled online, because the monitor reloads its configuration.
2. **Retry on a missing binary.** A missing, empty or non-executable binary no longer leaves the launcher idle until
   the next trigger. The launcher retries with a backoff (1s up to 60s), re-reading the configuration on each attempt.
   This closes the cold start gap described in the Risks.
3. **Sidecar always copies.** The sidecar ignores `--main-container-version` and always stages its binaries, and
   re-copies files that were removed from its output directory.
4. **Pruning of unused version directories by the launcher.** The launcher removes version directories that are neither
   the desired version nor used by a running process, and skips pruning while a bounce is pending. Versions newer than
   the desired one are kept.

Not covered by the monitor yet: reporting of the launched binary path / staged versions (Prometheus label and Pod
annotation). The operator keeps verifying the mode through the running version (see 4.7).

### How the operator uses them

`monitorVersion` is part of the spec, so the operator knows which monitor runs in the main container and does not have
to detect support at runtime (an older monitor silently ignores the unknown `useSharedBinary` field, so the operator
must never set it for a monitor that does not support it). The versions that contain the features are
`Version.SupportsSharedBinaryConfig`: **7.4.10 and newer 7.4 patches, 8.0.2 and newer 8.0 patches, and 8.1 and newer**.
The features will not be backported to 7.3 and older releases.

| `monitorVersion` | Mechanism |
|---|---|
| Does not support the features (7.3 and older, 7.4.x < 7.4.10, 8.0.0, 8.0.1) | Operator-only design as described above: fake `--version-file` on capable Pods, no `--main-container-version` flag, rolling update at the first patch upgrade, nudge of Pods whose monitor has not reported a configuration. |
| Supports the features | `useSharedBinary: true` in the generated process configuration whenever the Pod version is a different patch version of `monitorVersion`. The Pod spec equals the non-capable spec (no `--version-file`), so there are no capable Pods and the first patch upgrade is in place too. The monitor retries to load the configuration if the binary is not staged yet. |

**Escape hatch.** If the version table is wrong for a monitor (an old monitor ignores the option silently), the
annotation `foundationdb.org/disable-shared-binary-config: "true"` on the `FoundationDBCluster` makes the operator treat
every monitor of this cluster as not supporting the option. The operator then uses the version file and does not write
the option into the process configuration. An annotation was chosen over a spec field, because a spec field would be new
API surface that is hard to remove again. Setting the annotation does not change the hash, but Pods that are
supposed to use the shared binary and have no version file get the `IncorrectPodSpec` condition and are replaced once, like
for the first patch upgrade with an old monitor. Clusters where `spec.version` is equal to or incompatible with
`monitorVersion` are not affected. Only the value `true` (any form accepted by `strconv.ParseBool`) disables the option,
other values are ignored.

The option is only set for unified images and only if it is needed (the Pod version is protocol compatible with
`monitorVersion` but not equal). In all other cases the monitor already starts the right binary. The monitor does not
restart processes when only the option changes. The `shared-binary-version` ConfigMap key is still written for all pinned
clusters. Removing it while Pods that mount it are still running would break the ConfigMap updates of these Pods.

The sidecar image follows `spec.version`, which can be older than the minimum version. Leaving out
`--main-container-version` works with both: old sidecars always copy, new ones ignore the flag. Pods that mount the
file keep working with both mechanisms.

Switching between the two mechanisms by changing `monitorVersion` is a normal Pod spec change and goes through the
regular rolling update. While Pods with an older monitor are still replaced, they ignore the option and rely on the
version file, or are replaced before they are restarted, because their main container image differs.

Open questions for the monitor side:

* Pruning currently runs for every launcher, not only when `useSharedBinary` is set. It should be gated, or this
  documented as a behavior change for default clusters.
* The sidecar always copying means default clusters stage an additional copy of the binaries in the shared volume.
* The sidecar restores a removed directory of its own version. If the sidecar is older than the desired version, the
  launcher prunes that directory and the sidecar copies it again at every process start. This should only happen
  while the sidecar lags behind and is harmless.
* If the gate table is wrong and a monitor ignores the option, the baked-in binary keeps running and the bounce repeats.
  The operator does not detect this yet, see "mode honored" above.

**Development note:** until the monitor change is released, the operator builds against the monitor API with a local
`replace` directive in `go.mod`. It must be replaced by a version bump of
`github.com/apple/foundationdb/fdbkubernetesmonitor` before merging.

## Risks and Open Questions

* **Cold start dependency.** For capable Pods `fdbserver` startup depends on the sidecar having staged its binary.
  If the launcher loads the configuration before the copy has finished it rejects it and starts nothing until the
  next trigger (config file event or Pod timestamp). In the upgrade flow the operator avoids this by patching the
  sidecar and waiting before the config version is bumped. For a Pod restart or a freshly created Pod it remains a
  gap. Observed in an e2e run: all new Pods stayed without any monitor annotation, because the operator skipped the
  Pods in `updatePodConfig` (their config hash already matched), so the monitor was never notified to reload the
  configuration. The operator now runs the config sync for every Pod of a pinned cluster whose monitor has not reported a
  configuration yet (`shouldSyncPodConfig`), which sets the outdated-config-map annotation until the monitor accepted the
  configuration. The permanent fix is the retry in future monitor versions.
* **Reliance on monitor internals.** The mode depends on `currentContainerVersion` only being used for the
  compatibility check, and on the string comparison for the sidecar copy. A future monitor change could break this.
  Mitigation: an e2e test per supported monitor version, and a note in the monitor repo.
* **Image hygiene.** The main image is no longer refreshed by FDB upgrades. Documentation must say that users are
  responsible for bumping `monitorVersion`, and that FDB upgrades alone will not deliver base-image security fixes.
* **Tooling version skew.** Other binaries in the main image (`fdbcli`, `fdbbackup`, plugin exec paths) are from the
  pinned version, not the running server version. Needs an audit of all consumers; `fdbcli` from the shared directory
  is already copied by the sidecar and may be preferable.
* **Shared volume size.** Old binaries accumulate until pruned (launcher pruning, see "Future monitor versions"). Without it, document that the
  volume and its `sizeLimit` must account for the versions that will be staged over the lifetime of a Pod, and that
  recreating the Pod resets the volume.
* **Fake version file.** The version file content (`0.0.0`, in the ConfigMap only) means the launcher's own reporting of the baked-in version is not
  meaningful for capable Pods. The existing `running_version`/`desired_version` metrics report the version that is
  actually running and must be used instead.
* **Multi-operator `SynchronizationMode`.** The field is part of the spec, so it should be consistent across operator
  instances; verify nothing derives the main image locally.
* **`IgnoreLogGroupsForUpgrade`** and other upgrade-related options should be checked for assumptions that the main
  image tracks `spec.version`.
* **Open question: rolling restart of compatible upgrades.** The initial implementation restarts all processes that are
  ready in one call, like any other configuration change. Incompatible upgrades require this, but a compatible
  (patch) upgrade is a rolling Pod replacement today. A rolling restart (per process group or in batches, with the
  existing safety checks) would avoid additional recoveries. This is not implemented yet.
* **Stragglers.** If a process misses the restart of a version incompatible upgrade, nothing restarts it implicitly: in
  the default mode all Pods are recreated at the end of the upgrade, with a pinned image they are not. The existing
  `removeIncompatibleProcesses` reconciler recreates Pods of processes in `incompatible_connections`, which requires a
  status from the database. If the database is unavailable because of stranded processes (for example no cluster
  controller can be elected), the operator cannot find them. A status independent detection needs the monitor to report
  the version of the running processes, and an operator step that restarts the process (isolate annotation toggle or
  Pod deletion). It is deferred until a monitor release contains the reporting. Until then the manual recovery is to
  delete the stranded Pods. See E27 to E29 in the test plan.
* **Relation to the deferral draft.** This design makes the deferral draft unnecessary for users who opt in. The
  deferral draft remains relevant only for users who want to keep the default model. Decision: close the deferral draft
  if this one is accepted.

## Implementation Plan

Staged so that each stage is useful on its own:

1. **Stage 1: API, validation and hash handling.** New field, deepcopy/CRD generation, validation tests (unified only),
   image resolution, `--main-container-version = monitorVersion`, pinned-mode hash and the annotation re-stamp. Tests
   in `internal/pod_models_test.go` and `internal/pod_helper_test.go`: specs and hashes without `monitorVersion` are
   unchanged; with `monitorVersion == spec.version` the spec is identical to today's; the hash is identical for two
   different `spec.version` values; the re-stamp happens only if the legacy hash matches.
2. **Stage 2: in-place incompatible upgrades.** `updateSidecarVersions` converging the sidecar in pinned mode,
   `updatePods` queuing nothing after the upgrade, status/verification. Controller tests: `updatePods` queues no Pod
   for deletion after a version change; no Pod is rolled when the mode is enabled. E2E: enable the mode and perform an
   incompatible upgrade, verifying no Pod recreation and the correct running version.
3. **Stage 3: patch upgrades.** Capable Pod spec (version file key, `--version-file`, no `--main-container-version`),
   per-Pod capability detection, fallback to the rolling update for non-capable Pods, restart of compatible upgrades
   in place through the regular bounce (implemented, a rolling restart is still open). E2E: first patch upgrade rolls
   once, the next patch upgrade does not; a Pod restart mid-upgrade (not implemented yet).
4. **Stage 4: docs.** Manual section on the mode, trade-offs and the process for rolling the pinned image forward.
5. **Later (optional):** adopt the features of future monitor versions (`useSharedBinary`, retry, pruning) behind a
   minimum `monitorVersion`.

## Related Links

* [Design on Combining the Main Container and the Sidecar Container on Kubernetes](unified_sidecar_and_process_monitor.md)
