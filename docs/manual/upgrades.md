# FoundationDB Upgrades

## Overview

The FDB operator supports FDB clusters upgrades by adjusting the `version` setting in the `FoundationDBCluster` spec to the new desired version.
It also offers limited support for downgrades, the limitation is that the operator only supports downgrades to an older patch version of the running FDB version.
Upgrades of FoundationDB running on Kubernetes are challenging and the operator provides a way to handle a lot of the complexity on itself.
The document will describe the upgrade process in details and some cases the operator cannot handle currently.

FoundationDB upgrades for major and minor versions are incompatible and require that all processes are updated in a short time period.

By default the operator recreates all Pods at the end of an upgrade, to move them to the image of the new version.
Clusters that use the unified image can opt in to a [pinned main container image](#pinned-main-container-image), where the image of the main container does not change with the FDB version and upgrades are performed in-place without recreating the Pods.

### Upgrade Process Details

This document describes the upgrade process based on the operator version `v1.14.0` for older versions there might be a slight difference in how the operator is handling the upgrades.
In addition to that we are currently working on reducing the complexity for patch upgrades to reduce the upgrade steps to the [Recreation of old Pods](#recreation-of-pods-phase) phase.

The graphic below represents the different stages in a Pod during the upgrade:

<img src="./imgs/upgrade_mechanism.svg">

The first state on the left is the initial state before the operator is doing any work.
The second state in the middle is the state of the Pod during the [Staging Phase](#staging-phase) and the [Restart Phase](#restart-phase).
The third and last state is the state after the [Recreation of Pods Phase](#recreation-of-pods-phase) where the according Pod was recreated with the new desired image version.


#### Pre Upgrade Check Phase

Before the upgrade is initiated the operator will perform a compatibility check for all clients of this cluster.
This check will ensure that a cluster is only upgraded if all clients support the new version, for patch upgrades this check is skipped.
More information about this reconciler can be found in the [technical design](technical_design.md#checkclientcompatibility).
Clients not supporting the new version will be reported in the logs of the operator with the message `Deferring reconciliation due to unsupported clients` and in addition the operator will emit a Kubernetes event.
This prevents upgrading a database until all clients have been updated with a compatible client library.

#### Staging Phase

This phase ensures that all Pods are in a state that the operator can restart all `fdbserver` processes and they will be restarting with the new FDB version.
The first step is the `UpdateSidecarVersions`, this subreconciler updates the image of the `foundationdb-kubernetes-sidecar` container to match the desired FDB version.
Updating the image of a single container doesn't require the recreation of the Pod and keeps the main container that runs the `fdbserver` process running.
The sidecar container will copy the new binary inside the main container over a shared volume.
In the main container the new binaries will be present in `/var/dynamic-conf/bin/$fdb_version`, this version will be used once the `fdbserver` process is restarted.
The `UpdatePodConfig` subreconciler will ensure that all Pods have the new configuration present and that the new binary is present in the shared volume.
This configuration ensures that the `fdbmonitor` will restart processes with the new binary located at `/var/dynamic-conf/bin/$fdb_version`.
Once all Pods have the new configuration and binaries present the operator can move to the next phase.

A potentional blocker for moving towards the next Phase can be an issue with a subset of the Pods e.g. if the sidecar is unreachable.
If the sidecar is unreachable the operator is not able to confirm that the new configuration and binary is in place.
All Process Groups affected by such an issue can be discovered with the `SidecarUnreachable` condition, until this issue is resolved the upgrade will be blocked.
The best way forward for this is to replace the affected Pods with the following [kubectl fdb plugin](../../kubectl-fdb/Readme.md) command:

```bash
kubectl fdb remove process-group -c cluster pod-1
```

If the whole Pod is affected by the networking issue the operator will replace the Pod automatically if automatic replacements are enabled, which is the default.
In order to replace the Pod the operator will wait for the Process Group to have a [failure condition](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/v1.14.0/api/v1beta2/foundationdbcluster_types.go#L65) for at least the defined [FailureDetectionTimeSeconds](../cluster_spec.md#automaticreplacementoptions), by default those are 7200 seconds (2 hours).

#### Restart Phase

The `BounceProcesses` subreconciler will handle the restart of all `fdbserver` processes.
For FoundationDB clusters that are spanned across multiple Kubernetes clusters the operator will follow a special process which is documented in the [technical design](technical_design.md#bounceprocesses).
The subreconciler will also ensure to wait until all `fdbserver` processes are ready to be restarted to prevent cases where only a subset of processes are restarted.
In this case ready means that all Pods have the new `fdbmonitor` configuration present and that the new binary is present in the shared volume at `/var/dynamic-conf/bin/$fdb_version`.
After the `kill` command the operator will initiate a new reconciliation loop to detect the new, upgraded running version, this is handled in the `UpdateState` subreconciler and the version is detected based on the output of the [cluster status json](https://apple.github.io/foundationdb/mr-status.html).

#### Recreation of Pods Phase

This phase does not happen for clusters with a [pinned main container image](#pinned-main-container-image), unless `monitorVersion` is changed together with the version.

Technically the upgrade is now done and all the pods in the cluster are running on the new version but the operator has to do some cleanup.
The cleanup includes the recreation of all Pods to ensure they use the new FoundationDB image for the desired version and the `fdbmonitor` configuration will be updated to point again to 
the binary included in the image at `/bin/$fdb_version/fdb-server`, instead of the one at `/var/dynamic-conf/bin/$fdb_version` in the original, technically-upgraded pod.
Depending on the defined [Pod update strategy](../cluster_spec.md#podupdatestrategy) the operator will handle the recreation of the Pods differently.
In the default setting `ReplaceTransactionSystem` the operator will recreate the storage Pods zone by zone and replaces Pods of the transaction system (log and stateless processes).
The replacement of the transaction system has the benefit that the recoveries of the transaction system is reduced to potentially one recovery.
During this phase the operator will also run the `RemoveIncompatibleProcesses` subreconciler, this reconciler will recreate all Pods that host a `fdbserver` process that is in the list of the `incompatible_connections` and is missing the the process list of the [cluster status json](https://apple.github.io/foundationdb/mr-status.html).
This reconciler should ensure that the cluster is moving faster towards the desired configuration if some processes were not restarted properly.

Once all Pods are updated to the new image the upgrade is done and the cluster status of the FoundationDB cluster resource in Kubernetes should show that the reconciliation is done.

#### Note on Recreation vs Replacement

Do note that both "recreation" (also referred to as "deletion" of a pod/process group) and "replacement" have a specific meaning here,
see [Replacements and Deletions](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/main/docs/manual/replacements_and_deletions.md#replacements-and-deletions)
for more information.

### Pinned Main Container Image

With the [unified image](../design/unified_sidecar_and_process_monitor.md) every Pod runs the `fdb-kubernetes-monitor` in the main container, which starts the `fdbserver` process, and a sidecar container that copies the binaries of its own image into a shared volume.
By default the image of the main container follows the FDB version, which means that the [Recreation of Pods Phase](#recreation-of-pods-phase) recreates all Pods at the end of every upgrade.
For clusters with a lot of stateful Pods this is expensive: volumes must be reattached, the operator waits for the cluster to be healthy again and the recreation can cause additional recoveries.
For protocol compatible (patch) upgrades the recreation is the upgrade itself, for version incompatible upgrades it is only cleanup after the processes are already running on the new version.

#### Why this mode exists

The recreation at the end of an upgrade is only needed to move the main container to the image of the new version.
The unified image makes this unnecessary, because the sidecar already copies the `fdbserver` binary of its own version into a volume that is shared with the main container.
The pinned main container image uses this: the sidecar is the carrier of the binaries for the version of the cluster and the main container only provides the `fdb-kubernetes-monitor` that starts them.
This keeps the cost of an upgrade independent of the number of Pods that have to be recreated and makes updating the main image (base image or monitor fixes) an explicit decision of the user, instead of a side effect of every FDB upgrade.
The design, the interaction with the monitor and the open questions are described in the [pinned main container image design](../design/pinned_main_container_image.md).
The [test plan](../design/pinned_main_container_image_test_plan.md) lists what is verified for this mode.

The mode is opt-in.
Clusters that do not set `monitorVersion` behave as described in the [upgrade process details](#upgrade-process-details).

#### Enabling the mode

If `monitorVersion` is set in the main container settings, the main container image stays at this version and `fdbserver` runs from the binaries that the sidecar stages in the shared volume (`/var/fdb/shared-binaries/bin/$fdb_version`).
Only the image of the sidecar container is updated in-place when the FDB version changes.

```yaml
apiVersion: apps.foundationdb.org/v1beta2
kind: FoundationDBCluster
metadata:
  name: sample-cluster
spec:
  version: 7.4.10
  imageType: unified
  mainContainer:
    monitorVersion: 7.4.10
```

`monitorVersion` must be a valid FDB version (the same format as `version`) and is only supported with the unified image.
The version the main container image is using is shown in `status.monitorVersion` and in the `MonitorVersion` column of `kubectl get fdb`.
If `monitorVersion` is not set, the status shows the `version` of the cluster.

#### Upgrade flows compared

The phases have the same names as in the [upgrade process details](#upgrade-process-details).
The first diagram shows the classic flow for a version incompatible upgrade, the second one the same upgrade with a pinned main container image.

```mermaid
flowchart TD
    A["Pre upgrade check<br/>all clients support the new version"] --> B["Staging phase<br/>sidecar image updated in-place<br/>new binary and configuration in place"]
    B --> C["Restart phase<br/>wait until all Pods are ready<br/>restart all processes"]
    C --> D["Recreation of Pods phase<br/>all Pods are recreated with the image of the new version"]
    D --> E["Upgrade done"]
    D -.->|"process did not restart"| F["RemoveIncompatibleProcesses<br/>recreates the Pod"]
    F --> E
```

With a pinned main container image there is no recreation phase.
The restart only starts once the monitor of every Pod has accepted the configuration of the new version, which is only possible after the sidecar has staged the binary.

```mermaid
flowchart TD
    A["Pre upgrade check<br/>all clients support the new version"] --> B["Staging phase<br/>sidecar image updated in-place<br/>binary staged, configuration pushed"]
    B --> C{"Monitor of every Pod accepted<br/>the new configuration?"}
    C -->|"no, binary not staged yet"| B
    C -->|"yes"| D["Restart phase<br/>restart all processes that are ready"]
    D --> E["Upgrade done<br/>no Pod was recreated"]
    D -.->|"process did not restart"| F["RemoveIncompatibleProcesses<br/>recreates the Pod, needs a cluster status"]
    F --> E
```

For protocol compatible (patch) upgrades the flow depends on the version of the monitor in the main container:

```mermaid
flowchart TD
    A["Patch upgrade"] --> B{"monitorVersion supports<br/>the useSharedBinary option?<br/>7.4.10+, 8.0.2+, 8.1+"}
    B -->|"yes"| C["Staging phase<br/>sidecar image updated in-place<br/>useSharedBinary set in the configuration"]
    C --> D["Restart phase<br/>processes start from the staged binary"]
    D --> G["Upgrade done<br/>no Pod was recreated"]
    B -->|"no"| E{"Pods already use the<br/>staged binary?"}
    E -->|"yes, after an earlier patch upgrade"| H["Staging phase<br/>sidecar image updated in-place<br/>the Pods already start the staged binary"]
    H --> D
    E -->|"no, first patch upgrade"| F["Pods are replaced once<br/>new Pods always start the staged binary"]
    F --> G
```

#### Rollout

| Change | What the operator does |
|---|---|
| Set `monitorVersion` to the current `version` | Nothing is restarted or recreated. The operator only updates the `foundationdb.org/last-applied-spec` annotation of the Pods. Enable the setting before you start an upgrade. |
| Version incompatible upgrade (major or minor) | The [Staging Phase](#staging-phase) and the [Restart Phase](#restart-phase) run as before, the sidecar image is updated in-place and the processes are restarted with the staged binary. The Recreation of Pods Phase does not happen, no Pod is recreated. |
| Patch upgrade, `monitorVersion` of at least `7.4.10`, `8.0.2` or `8.1.0` | In-place as well: the operator writes the option `useSharedBinary` into the process configuration, updates the sidecar image and restarts the processes with the staged binary. |
| Patch upgrade, older `monitorVersion` | The first patch upgrade away from `monitorVersion` replaces the Pods once (according to the [Pod update strategy](../cluster_spec.md#podupdatestrategy)) with Pods that always start the staged binary. All further patch upgrades are in-place. |
| Set `monitorVersion` and `version` to the same new version | Normal upgrade with a rolling update of the Pods, like for a cluster without `monitorVersion`. Use this to roll the pinned image forward, for example for base image or monitor fixes. The following upgrades are in-place again. |
| Change only `monitorVersion` | Rolling update of the Pods to the new main container image. |
| Remove `monitorVersion` | Rolling update of the Pods back to the images of the `version`. |

The monitor versions that support the `useSharedBinary` option are `7.4.10` and newer 7.4 patches, `8.0.2` and newer 8.0 patches and `8.1.0` and newer.
They also retry to start the process if the sidecar has not finished staging the binary yet.
These changes are not available for `7.3` and older versions, which use the first patch upgrade rollout from the table.

During an in-place upgrade the operator only restarts the processes if the monitor of every Pod has accepted the configuration of the new version, and the monitor only does that once the binary exists.
The restart is done with the same `kill` command that is used for all other restarts, which means all processes that are ready are restarted at the same time.

#### Caveats

- **Image maintenance**: the main container image is not refreshed by upgrades anymore.
  Security fixes in the base image and bug fixes in the `fdb-kubernetes-monitor` only reach the Pods if you change `monitorVersion`.
  Plan a regular update of `monitorVersion`, see the [rollout table](#rollout).
- **Tools in the main container**: `fdbcli`, `fdbbackup` and other binaries in the main container image have the version of `monitorVersion`, not the version of the running `fdbserver`.
  Commands that exec into the main container, for example some [kubectl fdb plugin](../../kubectl-fdb/Readme.md) commands, can therefore use a different version than the cluster.
  The staged `fdbcli` of the running version is available in the shared volume at `/var/fdb/shared-binaries/bin/$fdb_version/fdbcli`.
- **Dependency on the sidecar**: a Pod can only start `fdbserver` if the sidecar has staged the binary.
  If the sidecar of a new Pod cannot copy the binary, the process of this Pod will not start.
  Monitors before `7.4.10` and `8.0.2` do not retry, the operator triggers a reload of the configuration until the monitor has accepted it.
  A Process Group with this problem has the `MissingProcesses` condition and the Pod has no `foundationdb.org/launcher-environment` annotation.
  If the operator does not do this, you can trigger the reload manually: `kubectl annotate pod pod-1 foundationdb.org/outdated-config-map-seen="$(date +%s)" --overwrite`.
- **Disk usage**: the staged binaries of old versions stay in the shared volume (an `emptyDir`) until the Pod is recreated, every upgrade adds the `fdbserver` and `fdbcli` binaries of the new version.
  Monitor versions with support for the `useSharedBinary` option remove the staged versions that are not used anymore.
  For other versions plan for the versions you expect to deploy during the life time of a Pod, the volume is reset when the Pod is recreated, for example when you change `monitorVersion`.
- **Processes that are not restarted**: in the classic flow the Recreation of Pods Phase restarts every process that missed the `kill` command, because all Pods are recreated.
  With a pinned image this phase does not exist, so a process that missed the restart keeps running on the old version.
  For version incompatible upgrades the operator recreates the Pods of processes that are listed in the `incompatible_connections` of the cluster status in the `RemoveIncompatibleProcesses` subreconciler.
  This needs a cluster status and does not work if the cluster is unavailable because of those processes, for example if the majority of the coordinators did not restart.
  In this case restart or recreate the affected Pods manually, see [Processes are not restarted during the restart Phase](#processes-are-not-restarted-during-the-restart-phase).
  For patch upgrades these processes are not a problem, since they are still part of the cluster.
- **Disabling the shared binary option**: if the `useSharedBinary` option causes problems you can set the annotation `foundationdb.org/disable-shared-binary-config: "true"` on the `FoundationDBCluster`.
  The operator then uses the mechanism for older monitors.
  Pods that need the staged binary but do not have the required settings will be replaced once.
- **Multiple regions and data centers**: the in-place upgrade uses the same coordination between the operators as the classic upgrade, see the [technical design](technical_design.md#bounceprocesses).
  Every operator needs the `fdbcli` of the new version.
  The pinned main container image has not been validated in multi-region setups yet.
- **Downgrades**: the same limitations as for the classic upgrade apply, only downgrades to an older patch version are supported.

### Known issues

There are a number of known issues that can occur during an upgrade of FoundationDB running on Kubernetes.
Most of the issues are only a challenge for version incompatible upgrades and most issues are handled automatically by the operator.
If an issue cannot be handled by the operator we will outline a way to manually resolve the issue.

#### Process restarted during staging Phase

During the staging phase it can happen that a `fdbserver` process gets restarted by an external event like the OOM killer or any issue that results in the `fdbserver` crashing.
In that case the `fdbmonitor` will restart the `fdbserver` process with the new version and for version incompatible upgrades, that means this process is not able to connect to the cluster.
The operator will mark this Process Group with the `MissingProcesses` condition, since the process will be missing in the [cluster status json](https://apple.github.io/foundationdb/mr-status.html).
If the process has this condition longer than the [IgnoreMissingProcessesSeconds](../cluster_spec.md#foundationdbclusterautomationoptions) setting it will be ignored in the [Restart Phase](#restart-phase) to ensure the operator can move forward with the upgrade.
This also means that for a short time the cluster might be running with less resources.

Processes that are restarted too early can be identified with the `MissingProcesses` condition.
Using `kubectl` to fetch the cluster status and searching for all Process Groups with this condition gives an overview of those processes restarted too early.

The operator will handle those cases automatically and there is no need for manual intervention as long as not too many processes are restarted from the outside.
In that case the best way forward is to restart all processes still running on the old version, this can either be done manually or by using the [kubectl fdb plugin](../../kubectl-fdb/Readme.md) with the following command:

```bash
kubectl fdb restart -c cluster --process-condition=IncorrectCommandLine
```

One special case here is when the majority of the coordinators are restarted and the rest of the processes are still running on the old version.
In this case the operator is not able to handle the upgrade correctly since the returned cluster status has no information besides the coordinators that are reachable.
During this time the FDB cluster will be unavailable.
You have to manually restart all processes with the [kubectl fdb plugin](../../kubectl-fdb/Readme.md) with the command from above.

#### Processes are not restarted during the restart Phase

This issue is similar to the one above with the difference that affected processes are not restarted and they are still running on the old `fdbserver` version.
For version incompatible upgrades that means that those processes are not reporting to the cluster.
The operator will mark this Process Group with the `MissingProcesses` condition, since the process will be missing in the [cluster status json](https://apple.github.io/foundationdb/mr-status.html).
If those processes are reported in the `incompatible_connections` field of the cluster status json the operator will recreate those Pods directly with the new image in the `RemoveIncompatibleProcesses` subreconciler.
Otherwise the Pod will recreate those Pods in the [recreation of Pods phase](#recreation-of-pods-phase) with the new image.
During that time the cluster might run with reduced capacity since not all processes are reporting to the cluster.

The operator can handle those cases automatically.
If you want to speed up this process you can restart the processes with the [kubectl fdb plugin](../../kubectl-fdb/Readme.md):

```bash
kubectl fdb restart -c cluster --process-condition=MissingProcesses
```

One special case here is when the majority of coordinators are not restarted and they are stuck on the old version.
In that case the operator is not able to handle this case automatically and you have to restart the coordinator processes manually with the [kubectl fdb plugin](../../kubectl-fdb/Readme.md).
Either by targeting directly the coordinator Pods (you can get the coordinator Pods by comparing the IPs of the connection string with the IP addresses of the Pods):

```bash
kubectl fdb -n default restart -c cluster pod-1 pod-2
```

or by restarting all processes of the cluster:

```bash
kubectl fdb restart -c cluster --all-processes
```

#### Detect correct version

The operator tries per default to read the [cluster status json](https://apple.github.io/foundationdb/mr-status.html) with the [multi-version](https://apple.github.io/foundationdb/api-general.html#multi-version-client) from the system key-space.
During an upgrade it can happen that the multi-version client tries to connect to the cluster with the wrong version especially if not all coordinators are upgraded.
In this case the operator will fallback using `fdbcli` for the different versions (the one that is desired and the current running version).
This requires that the majority of coordinators are upgraded to the new desired version.
In addition to that we will retry the command if the `fdbcli` hits a timeout responding.
The code in the [fdbclient package](https://github.com/FoundationDB/fdb-kubernetes-operator/blob/v1.14.0/fdbclient/admin_client.go#L337-L360) has some additional details on how we handle those cases.

#### Processes running on a different version

We discussed in [Process restarted during staging Phase](#process-restarted-during-staging-phase) and [Processes are not restarted during the restart Phase](#processes-are-not-restarted-during-the-restart-phase) cases where a process could be running in a different version than expected.
For version incompatible upgrades the operator has no way to differentiate between processes not reporting to the cluster and processes running in the incompatible version, both will be missing from the processes list.
The only difference is that the incompatible processes might be reported in the `incompatible_connections` field.

#### Processes are not able to join the FDB cluster

We've seen cases where a subset of `fdbserver` were not able to join the FDB cluster.
This can happen due to a race condition where a new set of coordinators is chosen.
The affected process types are stateless and transaction or log processes.
The newly created Pods for the transaction system will have the condition `MissingProcesses` and if you exec into one of those Pods you will see the `fdbserver` process running.
When you check the logs from `fdbmonitor` you will see the following logs:

```bash
$ cat /var/log/fdb-trace-logs/fdbmonitor-*
Severity="10" LogGroup="default" Process="fdbmonitor": Updated configuration for fdbserver.1
Severity="40" LogGroup="default" Process="fdbserver.1": Warning: FDBD has not joined the cluster after 5 seconds.
Severity="40" LogGroup="default" Process="fdbserver.1":   Check configuration and availability using the 'status' command with the fdbcli
```

You can resolve this issue by restarting all those processes with the kubectl plugin (or manually):

```bash
kubectl fdb -n aci-fdb-prod restart -c cluster --process-condition=MissingProcesses
```

There is a fix on FoundationDB side that should prevent this behaviour in the future: https://github.com/apple/foundationdb/pull/9814.

### How we test upgrades in the operator

For every PR against the operator repository we run a set of tests in addition to that we are running those tests on a regular basis with different versions to ensure we test major, minor and patch upgrades.

Those tests include:

- Upgrades without any failure injection
- Upgrading a cluster with a partitioned Pod
- Upgrading a cluster with a partitioned Pod which eventually gets replaced
- Upgrading a cluster with a random Pod deleted during rolling bounce phase
- Upgrading a cluster where one coordinator gets restarted during the staging phase
- Upgrading a cluster with a crash looping sidecar process
- Upgrading a cluster and one coordinator is not restarted
- Upgrading a cluster and multiple processes are not restarted
- Upgrading a cluster with link that drops some packets
- Upgrading a cluster and no coordinator is restarted

In addition we have some more tests that will run against a cluster in multi-region configuration.
Those tests are extended if some new edge cases are discovered.

## Next

You can continue on to the [next section](debugging.md) or go back to the [table of contents](index.md).
