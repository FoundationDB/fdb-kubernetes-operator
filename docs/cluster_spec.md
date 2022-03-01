<br>
# API Docs
This Document documents the types introduced by the FoundationDB Operator to be consumed by users.
> Note this document is generated from code comments. When contributing a change to this document please do so by changing the code comments.

## Table of Contents
* [AutomaticReplacementOptions](#automaticreplacementoptions)
* [BuggifyConfig](#buggifyconfig)
* [ClusterGenerationStatus](#clustergenerationstatus)
* [ClusterHealth](#clusterhealth)
* [ConnectionString](#connectionstring)
* [ContainerOverrides](#containeroverrides)
* [CoordinatorSelectionSetting](#coordinatorselectionsetting)
* [DataCenter](#datacenter)
* [DatabaseConfiguration](#databaseconfiguration)
* [FoundationDBCluster](#foundationdbcluster)
* [FoundationDBClusterAutomationOptions](#foundationdbclusterautomationoptions)
* [FoundationDBClusterFaultDomain](#foundationdbclusterfaultdomain)
* [FoundationDBClusterList](#foundationdbclusterlist)
* [FoundationDBClusterSpec](#foundationdbclusterspec)
* [FoundationDBClusterStatus](#foundationdbclusterstatus)
* [ImageConfig](#imageconfig)
* [LabelConfig](#labelconfig)
* [LockDenyListEntry](#lockdenylistentry)
* [LockOptions](#lockoptions)
* [LockSystemStatus](#locksystemstatus)
* [PendingRemovalState](#pendingremovalstate)
* [ProcessAddress](#processaddress)
* [ProcessCounts](#processcounts)
* [ProcessGroupCondition](#processgroupcondition)
* [ProcessGroupStatus](#processgroupstatus)
* [ProcessSettings](#processsettings)
* [Region](#region)
* [RequiredAddressSet](#requiredaddressset)
* [RoleCounts](#rolecounts)
* [RoutingConfig](#routingconfig)
* [ServiceConfig](#serviceconfig)
* [VersionFlags](#versionflags)

## AutomaticReplacementOptions

AutomaticReplacementOptions controls options for automatically replacing failed processes.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| enabled | Enabled controls whether automatic replacements are enabled. The default is false. | *bool | false |
| failureDetectionTimeSeconds | FailureDetectionTimeSeconds controls how long a process must be failed or missing before it is automatically replaced. The default is 1800 seconds, or 30 minutes. | *int | false |
| maxConcurrentReplacements | MaxConcurrentReplacements controls how many automatic replacements are allowed to take part. This will take the list of current replacements and then calculate the difference between maxConcurrentReplacements and the size of the list. e.g. if currently 3 replacements are queued (e.g. in the processGroupsToRemove list) and maxConcurrentReplacements is 5 the operator is allowed to replace at most 2 process groups. Setting this to 0 will basically disable the automatic replacements. | *int | false |

[Back to TOC](#table-of-contents)

## BuggifyConfig

BuggifyConfig provides options for injecting faults into a cluster for testing.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| noSchedule | NoSchedule defines a list of process group IDs that should fail to schedule. | []string | false |
| crashLoop | CrashLoops defines a list of process group IDs that should be put into a crash looping state. | []string | false |
| emptyMonitorConf | EmptyMonitorConf instructs the operator to update all of the fdbmonitor.conf files to have zero fdbserver processes configured. | bool | false |

[Back to TOC](#table-of-contents)

## ClusterGenerationStatus

ClusterGenerationStatus stores information on which generations have reached different stages in reconciliation for the cluster.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| reconciled | Reconciled provides the last generation that was fully reconciled. | int64 | false |
| needsConfigurationChange | NeedsConfigurationChange provides the last generation that is pending a change to configuration. | int64 | false |
| needsCoordinatorChange | NeedsCoordinatorChange provides the last generation that is pending a change to its coordinators. | int64 | false |
| needsBounce | NeedsBounce provides the last generation that is pending a bounce of fdbserver. | int64 | false |
| needsPodDeletion | NeedsPodDeletion provides the last generation that is pending pods being deleted and recreated. | int64 | false |
| needsShrink | NeedsShrink provides the last generation that is pending pods being excluded and removed. | int64 | false |
| needsGrow | NeedsGrow provides the last generation that is pending pods being added. | int64 | false |
| needsMonitorConfUpdate | NeedsMonitorConfUpdate provides the last generation that needs an update through the fdbmonitor conf. | int64 | false |
| missingDatabaseStatus | DatabaseUnavailable provides the last generation that could not complete reconciliation due to the database being unavailable. | int64 | false |
| hasExtraListeners | HasExtraListeners provides the last generation that could not complete reconciliation because it has more listeners than it is supposed to. | int64 | false |
| needsServiceUpdate | NeedsServiceUpdate provides the last generation that needs an update to the service config. | int64 | false |
| needsBackupAgentUpdate | NeedsBackupAgentUpdate provides the last generation that could not complete reconciliation because the backup agent deployment needs to be updated. **Deprecated: This needs to get moved into FoundationDBBackup** | int64 | false |
| hasPendingRemoval | HasPendingRemoval provides the last generation that has pods that have been excluded but are pending being removed.  A cluster in this state is considered reconciled, but we track this in the status to allow users of the operator to track when the removal is fully complete. | int64 | false |
| hasFailingPods | HasFailingPods provides the last generation that has pods that are failing to start. **Deprecated: This is no longer used.** | int64 | false |
| hasUnhealthyProcess | HasUnhealthyProcess provides the last generation that has at least one process group with a negative condition. | int64 | false |
| needsLockConfigurationChanges | NeedsLockConfigurationChanges provides the last generation that is pending a change to the configuration of the locking system. | int64 | false |

[Back to TOC](#table-of-contents)

## ClusterHealth

ClusterHealth represents different views into health in the cluster status.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| available | Available reports whether the database is accepting reads and writes. | bool | false |
| healthy | Healthy reports whether the database is in a fully healthy state. | bool | false |
| fullReplication | FullReplication reports whether all data are fully replicated according to the current replication policy. | bool | false |
| dataMovementPriority | DataMovementPriority reports the priority of the highest-priority data movement in the cluster. | int | false |

[Back to TOC](#table-of-contents)

## ConnectionString

ConnectionString models the contents of a cluster file in a structured way

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| databaseName | DatabaseName provides an identifier for the database which persists across coordinator changes. | string | false |
| generationID | GenerationID provides a unique ID for the current generation of coordinators. | string | false |
| coordinators | Coordinators provides the addresses of the current coordinators. | []string | false |

[Back to TOC](#table-of-contents)

## ContainerOverrides

ContainerOverrides provides options for customizing a container created by the operator.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| enableLivenessProbe | EnableLivenessProbe defines if the sidecar should have a livenessProbe. This setting will be ignored on the main container. | *bool | false |
| enableReadinessProbe | EnableReadinessProbe defines if the sidecar should have a readinessProbe. This setting will be ignored on the main container. | *bool | false |
| enableTls | EnableTLS controls whether we should be listening on a TLS connection. | bool | false |
| peerVerificationRules | PeerVerificationRules provides the rules for what client certificates the process should accept. | string | false |
| imageConfigs | ImageConfigs allows customizing the image that we use for a container. | [][ImageConfig](#imageconfig) | false |
| env | Env provides environment variables.  **Deprecated: Use the PodTemplate field instead.** | [][corev1.EnvVar](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#envvar-v1-core) | false |
| volumeMounts | VolumeMounts provides volume mounts.  **Deprecated: Use the PodTemplate field instead.** | [][corev1.VolumeMount](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#volumemount-v1-core) | false |
| imageName | ImageName provides the name of the image to use for the container, without the version tag.  **Deprecated: Use the PodTemplate field instead.** | string | false |
| securityContext | SecurityContext provides the container's security context.  **Deprecated: Use the PodTemplate field instead.** | *[corev1.SecurityContext](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#securitycontext-v1-core) | false |

[Back to TOC](#table-of-contents)

## CoordinatorSelectionSetting

CoordinatorSelectionSetting defines the process class and the priority of it. A higher priority means that the process class is preferred over another.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| processClass |  | ProcessClass | false |
| priority |  | int | false |

[Back to TOC](#table-of-contents)

## DataCenter

DataCenter represents a data center in the region configuration

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| id | The ID of the data center. This must match the dcid locality field. | string | false |
| priority | The priority of this data center when we have to choose a location. Higher priorities are preferred over lower priorities. | int | false |
| satellite | Satellite indicates whether the data center is serving as a satellite for the region. A value of 1 indicates that it is a satellite, and a value of 0 indicates that it is not a satellite. | int | false |

[Back to TOC](#table-of-contents)

## DatabaseConfiguration

DatabaseConfiguration represents the configuration of the database

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| redundancy_mode | RedundancyMode defines the core replication factor for the database. | RedundancyMode | false |
| storage_engine | StorageEngine defines the storage engine the database uses. | StorageEngine | false |
| usable_regions | UsableRegions defines how many regions the database should store data in. | int | false |
| regions | Regions defines the regions that the database can replicate in. | [][Region](#region) | false |
| RoleCounts | RoleCounts defines how many processes the database should recruit for each role. | [RoleCounts](#rolecounts) | true |
| VersionFlags | VersionFlags defines internal flags for testing new features in the database. | [VersionFlags](#versionflags) | true |

[Back to TOC](#table-of-contents)

## FoundationDBCluster

FoundationDBCluster is the Schema for the foundationdbclusters API

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#objectmeta-v1-meta) | false |
| spec |  | [FoundationDBClusterSpec](#foundationdbclusterspec) | false |
| status |  | [FoundationDBClusterStatus](#foundationdbclusterstatus) | false |

[Back to TOC](#table-of-contents)

## FoundationDBClusterAutomationOptions

FoundationDBClusterAutomationOptions provides flags for enabling or disabling operations that can be performed on a cluster.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| configureDatabase | ConfigureDatabase defines whether the operator is allowed to reconfigure the database. | *bool | false |
| killProcesses | KillProcesses defines whether the operator is allowed to bounce fdbserver processes. | *bool | false |
| deletePods | DeletePods defines whether the operator is allowed to delete pods in order to recreate them. **Deprecated: Use DeletionMode with PodUpdateModeNone to prevent the operator from deleting Pods.** | *bool | false |
| replacements | Replacements contains options for automatically replacing failed processes. | [AutomaticReplacementOptions](#automaticreplacementoptions) | false |
| ignorePendingPodsDuration | IgnorePendingPodsDuration defines how long a Pod has to be in the Pending Phase before we ignore it during reconciliation. This prevents Pod that are stuck in Pending to block further reconciliation. | time.Duration | false |
| ignoreTerminatingPodsDuration | ignoreTerminatingPodsDuration defines how long a Pod has to be in the Terminating Phase before we ignore it during reconciliation. This prevents Pod that are stuck in Terminating to block further reconciliation. | *int | false |
| enforceFullReplicationForDeletion | EnforceFullReplicationForDeletion defines if the operator is only allowed to delete Pods if the cluster is fully replicated. If the cluster is not fully replicated the Operator won't delete any Pods that are marked for removal. Defaults to true. **Deprecated: Will be enforced by default in 1.0.0 without disabling.** | *bool | false |
| useNonBlockingExcludes | UseNonBlockingExcludes defines whether the operator is allowed to use non blocking exclude commands. The default is false. | *bool | false |
| maxConcurrentReplacements | MaxConcurrentReplacements defines how many process groups can be concurrently replaced if they are misconfigured. If the value will be set to 0 this will block replacements and these misconfigured Pods must be replaced manually or by another process. For each reconcile loop the operator calculates the maximum number of possible replacements by taken this value as the upper limit and removes all ongoing replacements that have not finished. Which means if the value is set to 5 and we have 4 ongoing replacements (process groups marked with remove but not excluded) the operator is allowed to replace on further process group. | *int | false |
| deletionMode | DeletionMode defines the deletion mode for this cluster. This can be PodUpdateModeNone, PodUpdateModeAll, PodUpdateModeZone or PodUpdateModeProcessGroup. The DeletionMode defines how Pods are deleted in order to update them or when they are removed. | PodUpdateMode | false |
| removalMode | RemovalMode defines the removal mode for this cluster. This can be PodUpdateModeNone, PodUpdateModeAll, PodUpdateModeZone or PodUpdateModeProcessGroup. The RemovalMode defines how process groups are deleted in order when they are marked for removal. | PodUpdateMode | false |
| waitBetweenRemovalsSeconds | WaitBetweenRemovalsSeconds defines how long to wait between the last removal and the next removal. This is only an upper limit if the process group and the according resources are deleted faster than the provided duration the operator will move on with the next removal. The idea is to prevent a race condition were the operator deletes a resource but the Kubernetes API is slower to trigger the actual deletion, and we are running into a situation where the fault tolerance check still includes the already deleted processes. Defaults to 60. | *int | false |
| podUpdateStrategy | PodUpdateStrategy defines how Pod spec changes are rolled out either by replacing Pods or by deleting Pods. The default for this is ReplaceTransactionSystem. | PodUpdateStrategy | false |

[Back to TOC](#table-of-contents)

## FoundationDBClusterFaultDomain

FoundationDBClusterFaultDomain describes the fault domain that a cluster is replicated across.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| key | Key provides a topology key for the fault domain to replicate across. | string | false |
| value | Value provides a harcoded value to use for the zoneid for the pods. | string | false |
| valueFrom | ValueFrom provides a field selector to use as the source of the fault domain. | string | false |
| zoneCount | ZoneCount provides the number of fault domains in the data center where these processes are running. This is only used in the `kubernetes-cluster` fault domain strategy. | int | false |
| zoneIndex | ZoneIndex provides the index of this Kubernetes cluster in the list of KCs in the data center. This is only used in the `kubernetes-cluster` fault domain strategy. | int | false |

[Back to TOC](#table-of-contents)

## FoundationDBClusterList

FoundationDBClusterList contains a list of FoundationDBCluster

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| metadata |  | [metav1.ListMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#listmeta-v1-meta) | false |
| items |  | [][FoundationDBCluster](#foundationdbcluster) | true |

[Back to TOC](#table-of-contents)

## FoundationDBClusterSpec

FoundationDBClusterSpec defines the desired state of a cluster.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| version | Version defines the version of FoundationDB the cluster should run. | string | true |
| sidecarVersions | SidecarVersions defines the build version of the sidecar to run. This maps an FDB version to the corresponding sidecar build version. **Deprecated: Use SidecarContainer.ImageConfigs instead.** | map[string]int | false |
| databaseConfiguration | DatabaseConfiguration defines the database configuration. | [DatabaseConfiguration](#databaseconfiguration) | false |
| processes | Processes defines process-level settings. | map[ProcessClass][ProcessSettings](#processsettings) | false |
| processCounts | ProcessCounts defines the number of processes to configure for each process class. You can generally omit this, to allow the operator to infer the process counts based on the database configuration. | [ProcessCounts](#processcounts) | false |
| seedConnectionString | SeedConnectionString provides a connection string for the initial reconciliation.  After the initial reconciliation, this will not be used. | string | false |
| partialConnectionString | PartialConnectionString provides a way to specify part of the connection string (e.g. the database name and coordinator generation) without specifying the entire string. This does not allow for setting the coordinator IPs. If `SeedConnectionString` is set, `PartialConnectionString` will have no effect. They cannot be used together. | [ConnectionString](#connectionstring) | false |
| faultDomain | FaultDomain defines the rules for what fault domain to replicate across. | [FoundationDBClusterFaultDomain](#foundationdbclusterfaultdomain) | false |
| instancesToRemove | InstancesToRemove defines the instances that we should remove from the cluster. This list contains the instance IDs. **Deprecated: Use ProcessGroupsToRemove instead.** | []string | false |
| processGroupsToRemove | ProcessGroupsToRemove defines the process groups that we should remove from the cluster. This list contains the process group IDs. | []string | false |
| instancesToRemoveWithoutExclusion | InstancesToRemoveWithoutExclusion defines the instances that we should remove from the cluster without excluding them. This list contains the instance IDs.  This should be used for cases where a pod does not have an IP address and you want to remove it and destroy its volume without confirming the data is fully replicated. **Deprecated: Use ProcessGroupsToRemoveWithoutExclusion instead.** | []string | false |
| processGroupsToRemoveWithoutExclusion | ProcessGroupsToRemoveWithoutExclusion defines the process groups that we should remove from the cluster without excluding them. This list contains the process group IDs.  This should be used for cases where a pod does not have an IP address and you want to remove it and destroy its volume without confirming the data is fully replicated. | []string | false |
| configMap | ConfigMap allows customizing the config map the operator creates. | *[corev1.ConfigMap](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#configmap-v1-core) | false |
| mainContainer | MainContainer defines customization for the foundationdb container. | [ContainerOverrides](#containeroverrides) | false |
| sidecarContainer | SidecarContainer defines customization for the foundationdb-kubernetes-sidecar container. | [ContainerOverrides](#containeroverrides) | false |
| trustedCAs | TrustedCAs defines a list of root CAs the cluster should trust, in PEM format. | []string | false |
| sidecarVariables | SidecarVariables defines Custom variables that the sidecar should make available for substitution in the monitor conf file. | []string | false |
| logGroup | LogGroup defines the log group to use for the trace logs for the cluster. | string | false |
| dataCenter | DataCenter defines the data center where these processes are running. | string | false |
| dataHall | DataHall defines the data hall where these processes are running. | string | false |
| automationOptions | AutomationOptions defines customization for enabling or disabling certain operations in the operator. | [FoundationDBClusterAutomationOptions](#foundationdbclusterautomationoptions) | false |
| instanceIDPrefix | InstanceIDPrefix defines a prefix to append to the instance IDs in the locality fields.  This must be a valid Kubernetes label value. See https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set for more details on that. **Deprecated: Use ProcessGroupIDPrefix instead.** | string | false |
| processGroupIDPrefix | ProcessGroupIDPrefix defines a prefix to append to the process group IDs in the locality fields.  This must be a valid Kubernetes label value. See https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#syntax-and-character-set for more details on that. | string | false |
| updatePodsByReplacement | UpdatePodsByReplacement determines whether we should update pod config by replacing the pods rather than deleting them. **Deprecated: use PodUpdateStrategy instead** | bool | false |
| lockOptions | LockOptions allows customizing how we manage locks for global operations. | [LockOptions](#lockoptions) | false |
| services | Services defines the configuration for services that sit in front of our pods. **Deprecated: Use Routing instead.** | [ServiceConfig](#serviceconfig) | false |
| routing | Routing defines the configuration for routing to our pods. | [RoutingConfig](#routingconfig) | false |
| ignoreUpgradabilityChecks | IgnoreUpgradabilityChecks determines whether we should skip the check for client compatibility when performing an upgrade. | bool | false |
| buggify | Buggify defines settings for injecting faults into a cluster for testing. | [BuggifyConfig](#buggifyconfig) | false |
| sidecarVersion | SidecarVersion defines the build version of the sidecar to use.  **Deprecated: Use SidecarVersions instead.** | int | false |
| podLabels | PodLabels defines custom labels to apply to the FDB pods.  **Deprecated: Use the PodTemplate field instead.** | map[string]string | false |
| resources | Resources defines the resource requirements for the foundationdb containers.  **Deprecated: Use the PodTemplate field instead.** | *[corev1.ResourceRequirements](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#resourcerequirements-v1-core) | false |
| initContainers | InitContainers defines custom init containers for the FDB pods.  **Deprecated: Use the PodTemplate field instead.** | [][corev1.Container](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#container-v1-core) | false |
| containers | Containers defines custom containers for the FDB pods.  **Deprecated: Use the PodTemplate field instead.** | [][corev1.Container](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#container-v1-core) | false |
| volumes | Volumes defines custom volumes for the FDB pods.  **Deprecated: Use the PodTemplate field instead.** | []corev1.Volume | false |
| podSecurityContext | PodSecurityContext defines the security context to apply to the FDB pods.  **Deprecated: Use the PodTemplate field instead.** | *[corev1.PodSecurityContext](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#podsecuritycontext-v1-core) | false |
| automountServiceAccountToken | AutomountServiceAccountToken defines whether we should automount the service account tokens in the FDB pods.  **Deprecated: Use the PodTemplate field instead.** | *bool | false |
| nextInstanceID | NextInstanceID defines the ID to use when creating the next instance.  **Deprecated: This is no longer used.** | int | false |
| storageClass | StorageClass defines the storage class for the volumes in the cluster.  **Deprecated: Use the VolumeClaim field instead.** | *string | false |
| volumeSize | VolumeSize defines the size of the volume to use for stateful processes.  **Deprecated: Use the VolumeClaim field instead.** | string | false |
| runningVersion | RunningVersion defines the version of FoundationDB that the cluster is currently running.  **Deprecated: Consult the running version in the status instead.** | string | false |
| connectionString | ConnectionString defines the contents of the cluster file.  **Deprecated: You can use SeedConnectionString for bootstrapping, and you can use the ConnectionString in the status to get the latest connection string.** | string | false |
| configured | Configured defines whether we have configured the database yet. **Deprecated: This field has been moved to the status.** | bool | false |
| podTemplate | PodTemplate allows customizing the FoundationDB pods. **Deprecated: use the Processes field instead.** | *[corev1.PodTemplateSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#podtemplatespec-v1-core) | false |
| volumeClaim | VolumeClaim allows customizing the persistent volume claim for the FoundationDB pods. **Deprecated: use the Processes field instead.** | *[corev1.PersistentVolumeClaim](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#persistentvolumeclaim-v1-core) | false |
| customParameters | CustomParameters defines additional parameters to pass to the fdbserver processes. **Deprecated: use the Processes field instead.** | FoundationDBCustomParameters | false |
| pendingRemovals | PendingRemovals defines the processes that are pending removal. This maps the name of a pod to its IP address. If a value is left blank, the controller will provide the pod's current IP.  **Deprecated: To indicate that a process should be removed, use the ProcessGroupsToRemove field. To get information about pending removals, use the PendingRemovals field in the status.** | map[string]string | false |
| storageServersPerPod | StorageServersPerPod defines how many Storage Servers should run in a single process group (Pod). This number defines the number of processes running in one Pod whereas the ProcessCounts defines the number of Pods created. This means that you end up with ProcessCounts[\"storage\"] * StorageServersPerPod storage processes | int | false |
| minimumUptimeSecondsForBounce | MinimumUptimeSecondsForBounce defines the minimum time, in seconds, that the processes in the cluster must have been up for before the operator can execute a bounce. | int | false |
| replaceInstancesWhenResourcesChange | ReplaceInstancesWhenResourcesChange defines if an instance should be replaced when the resource requirements are increased. This can be useful with the combination of local storage. | *bool | false |
| skip | Skip defines if the cluster should be skipped for reconciliation. This can be useful for investigating in issues or if the environment is unstable. | bool | false |
| coordinatorSelection | CoordinatorSelection defines which process classes are eligible for coordinator selection. If empty all stateful processes classes are equally eligible. A higher priority means that a process class is preferred over another process class. If the FoundationDB cluster is spans across multiple Kubernetes clusters or DCs the CoordinatorSelection must match in all FoundationDB cluster resources otherwise the coordinator selection process could conflict. | [][CoordinatorSelectionSetting](#coordinatorselectionsetting) | false |
| labels | LabelConfig allows customizing labels used by the operator. | [LabelConfig](#labelconfig) | false |
| useExplicitListenAddress | UseExplicitListenAddress determines if we should add a listen address that is separate from the public address. | *bool | false |
| useUnifiedImage | UseUnifiedImage determines if we should use the unified image rather than separate images for the main container and the sidecar container. | *bool | false |

[Back to TOC](#table-of-contents)

## FoundationDBClusterStatus

FoundationDBClusterStatus defines the observed state of FoundationDBCluster

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| processCounts | ProcessCounts defines the number of processes that are currently running in the cluster. **Deprecated: Use ProcessGroups instead.** | [ProcessCounts](#processcounts) | false |
| incorrectProcesses | IncorrectProcesses provides the processes that do not have the correct configuration.  This will map the process group ID to the timestamp when we observed the incorrect configuration. **Deprecated: Use ProcessGroups instead.** | map[string]int64 | false |
| incorrectPods | IncorrectPods provides the pods that do not have the correct spec.  This will contain the name of the pod. **Deprecated: Use ProcessGroups instead.** | []string | false |
| failingPods | FailingPods provides the pods that are not starting correctly.  This will contain the name of the pod. **Deprecated: Use ProcessGroups instead.** | []string | false |
| missingProcesses | MissingProcesses provides the processes that are not reporting to the cluster. This will map the names of the pod to the timestamp when we observed that the process was missing. **Deprecated: Use ProcessGroups instead.** | map[string]int64 | false |
| databaseConfiguration | DatabaseConfiguration provides the running configuration of the database. | [DatabaseConfiguration](#databaseconfiguration) | false |
| generations | Generations provides information about the latest generation to be reconciled, or to reach other stages at which reconciliation can halt. | [ClusterGenerationStatus](#clustergenerationstatus) | false |
| health | Health provides information about the health of the database. | [ClusterHealth](#clusterhealth) | false |
| requiredAddresses | RequiredAddresses define that addresses that we need to enable for the processes in the cluster. | [RequiredAddressSet](#requiredaddressset) | false |
| hasIncorrectConfigMap | HasIncorrectConfigMap indicates whether the latest config map is out of date with the cluster spec. | bool | false |
| hasIncorrectServiceConfig | HasIncorrectServiceConfig indicates whether the cluster has service config that is out of date with the cluster spec. | bool | false |
| needsNewCoordinators | NeedsNewCoordinators indicates whether the cluster needs to recruit new coordinators to fulfill its fault tolerance requirements. | bool | false |
| runningVersion | RunningVersion defines the version of FoundationDB that the cluster is currently running. | string | false |
| connectionString | ConnectionString defines the contents of the cluster file. | string | false |
| configured | Configured defines whether we have configured the database yet. | bool | false |
| hasListenIPsForAllPods | HasListenIPsForAllPods defines whether every pod has an environment variable for its listen address. | bool | false |
| pendingRemovals | PendingRemovals defines the processes that are pending removal. This maps the process group ID to its removal state. **Deprecated: Use ProcessGroups instead.** | map[string][PendingRemovalState](#pendingremovalstate) | false |
| needsSidecarConfInConfigMap | NeedsSidecarConfInConfigMap determines whether we need to include the sidecar conf in the config map even when the latest version should not require it. **Deprecated: will be removed in the next release.** | bool | false |
| storageServersPerDisk | StorageServersPerDisk defines the storageServersPerPod observed in the cluster. If there are more than one value in the slice the reconcile phase is not finished. | []int | false |
| imageTypes | ImageTypes defines the kinds of images that are in use in the cluster. If there is more than one value in the slice the reconcile phase is not finished. | []ImageType | false |
| processGroups | ProcessGroups contain information about a process group. This information is used in multiple places to trigger the according action. | []*[ProcessGroupStatus](#processgroupstatus) | false |
| locks | Locks contains information about the locking system. | [LockSystemStatus](#locksystemstatus) | false |

[Back to TOC](#table-of-contents)

## ImageConfig

ImageConfig provides a policy for customizing an image.  When multiple image configs are provided, they will be merged into a single config that will be used to define the final image. For each field, we select the value from the first entry in the config list that defines a value for that field, and matches the version of FoundationDB the image is for. Any config that specifies a different version than the one under consideration will be ignored for the purposes of defining that image.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| version | Version is the version of FoundationDB this policy applies to. If this is blank, the policy applies to all FDB versions. | string | false |
| baseImage | BaseImage specifies the part of the image before the tag. | string | false |
| tag | Tag specifies a full image tag. | string | false |
| tagSuffix | TagSuffix specifies a suffix that will be added after the version to form the full tag. | string | false |

[Back to TOC](#table-of-contents)

## LabelConfig

LabelConfig allows customizing labels used by the operator.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| matchLabels | MatchLabels provides the labels that the operator should use to identify resources owned by the cluster. These will automatically be applied to all resources the operator creates. | map[string]string | false |
| resourceLabels | ResourceLabels provides additional labels that the operator should apply to resources it creates. | map[string]string | false |
| processGroupIDLabels | ProcessGroupIDLabels provides the labels that we use for the process group ID field. The first label will be used by the operator when filtering resources. | []string | false |
| processClassLabels | ProcessClassLabels provides the labels that we use for the process class field. The first label will be used by the operator when filtering resources. | []string | false |
| filterOnOwnerReference | FilterOnOwnerReferences determines whether we should check that resources are owned by the cluster object, in addition to the constraints provided by the match labels. | *bool | false |

[Back to TOC](#table-of-contents)

## LockDenyListEntry

LockDenyListEntry models an entry in the deny list for the locking system.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| id | The ID of the operator instance this entry is targeting. | string | false |
| allow | Whether the instance is allowed to take locks. | bool | false |

[Back to TOC](#table-of-contents)

## LockOptions

LockOptions provides customization for locking global operations.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| disableLocks | DisableLocks determines whether we should disable locking entirely. | *bool | false |
| lockKeyPrefix | LockKeyPrefix provides a custom prefix for the keys in the database we use to store locks. | string | false |
| lockDurationMinutes | LockDurationMinutes determines the duration that locks should be valid for. | *int | false |
| denyList | DenyList manages configuration for whether an instance of the operator should be denied from taking locks. | [][LockDenyListEntry](#lockdenylistentry) | false |

[Back to TOC](#table-of-contents)

## LockSystemStatus

LockSystemStatus provides a summary of the status of the locking system.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| lockDenyList | DenyList contains a list of operator instances that are prevented from taking locks. | []string | false |

[Back to TOC](#table-of-contents)

## PendingRemovalState

PendingRemovalState holds information about a process that is being removed. **Deprecated: This is modeled in the process group status instead.**

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| podName | The name of the pod that is being removed. | string | false |
| address | The public address of the process. | string | false |
| exclusionStarted | Whether we have started the exclusion. **Deprecated: This field is no longer filled in.** | bool | false |
| exclusionComplete | Whether we have completed the exclusion. | bool | false |
| hadInstance | Whether this removal has ever corresponded to a real instance. | bool | false |

[Back to TOC](#table-of-contents)

## ProcessAddress

ProcessAddress provides a structured address for a process.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| address |  | net.IP | false |
| stringAddress |  | string | false |
| port |  | int | false |
| flags |  | map[string]bool | false |
| fromHostname |  | bool | false |

[Back to TOC](#table-of-contents)

## ProcessCounts

ProcessCounts represents the number of processes we have for each valid process class.  If one of the counts in the spec is set to 0, we will infer the process count for that class from the role counts. If one of the counts in the spec is set to -1, we will not create any processes for that class. See GetProcessCountsWithDefaults for more information on the rules for inferring process counts.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| unset |  | int | false |
| storage |  | int | false |
| transaction |  | int | false |
| resolution |  | int | false |
| tester |  | int | false |
| proxy |  | int | false |
| master |  | int | false |
| stateless |  | int | false |
| log |  | int | false |
| cluster_controller |  | int | false |
| router |  | int | false |
| fast_restore |  | int | false |
| data_distributor |  | int | false |
| coordinator |  | int | false |
| ratekeeper |  | int | false |
| storage_cache |  | int | false |
| backup |  | int | false |
| resolver | **Deprecated: This is unsupported and any processes with this process class will fail to start.** | int | false |

[Back to TOC](#table-of-contents)

## ProcessGroupCondition

ProcessGroupCondition represents a degraded condition that a process group is in.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| type | Name of the condition | ProcessGroupConditionType | false |
| timestamp | Timestamp when the Condition was observed | int64 | false |

[Back to TOC](#table-of-contents)

## ProcessGroupStatus

ProcessGroupStatus represents the status of a ProcessGroup.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| processGroupID | ProcessGroupID represents the ID of the process group | string | false |
| processClass | ProcessClass represents the class the process group has. | ProcessClass | false |
| addresses | Addresses represents the list of addresses the process group has been known to have. | []string | false |
| remove | Remove defines if the process group is marked for removal. **Deprecated: Use RemovalTimestamp instead.** | bool | false |
| removalTimestamp | RemoveTimestamp if not empty defines when the process group was marked for removal. | *metav1.Time | false |
| excluded | Excluded defines if the process group has been fully excluded. This is only used within the reconciliation process, and should not be considered authoritative. **Deprecated: Use ExclusionTimestamp instead.** | bool | false |
| exclusionTimestamp | ExcludedTimestamp defines when the process group has been fully excluded. This is only used within the reconciliation process, and should not be considered authoritative. | *metav1.Time | false |
| exclusionSkipped | ExclusionSkipped determines if exclusion has been skipped for a process, which will allow the process group to be removed without exclusion. | bool | false |
| processGroupConditions | ProcessGroupConditions represents a list of degraded conditions that the process group is in. | []*[ProcessGroupCondition](#processgroupcondition) | false |

[Back to TOC](#table-of-contents)

## ProcessSettings

ProcessSettings defines process-level settings.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| podTemplate | PodTemplate allows customizing the pod. If a container image with a tag is specified the operator will throw an error and stop processing the cluster. | *[corev1.PodTemplateSpec](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#podtemplatespec-v1-core) | false |
| volumeClaim | VolumeClaim allows customizing the persistent volume claim for the pod. **Deprecated: Use the VolumeClaimTemplate field instead.** | *[corev1.PersistentVolumeClaim](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#persistentvolumeclaim-v1-core) | false |
| volumeClaimTemplate | VolumeClaimTemplate allows customizing the persistent volume claim for the pod. | *[corev1.PersistentVolumeClaim](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.17/#persistentvolumeclaim-v1-core) | false |
| customParameters | CustomParameters defines additional parameters to pass to the fdbserver process. | FoundationDBCustomParameters | false |
| allowTagOverride | This setting defines if a user provided image can have it's own tag rather than getting the provided version appended. You have to ensure that the specified version in the Spec is compatible with the given version in your custom image. **Deprecated: Use ImageConfigs instead.** | *bool | false |

[Back to TOC](#table-of-contents)

## Region

Region represents a region in the database configuration

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| datacenters | The data centers in this region. | [][DataCenter](#datacenter) | false |
| satellite_logs | The number of satellite logs that we should recruit. | int | false |
| satellite_redundancy_mode | The replication strategy for satellite logs. | string | false |

[Back to TOC](#table-of-contents)

## RequiredAddressSet

RequiredAddressSet provides settings for which addresses we need to listen on.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| tls | TLS defines whether we need to listen on a TLS address. | bool | false |
| nonTLS | NonTLS defines whether we need to listen on a non-TLS address. | bool | false |

[Back to TOC](#table-of-contents)

## RoleCounts

RoleCounts represents the roles whose counts can be customized.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| storage |  | int | false |
| logs |  | int | false |
| proxies |  | int | false |
| resolvers |  | int | false |
| log_routers |  | int | false |
| remote_logs |  | int | false |

[Back to TOC](#table-of-contents)

## RoutingConfig

RoutingConfig allows configuring routing to our pods, and services that sit in front of them.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| headlessService | Headless determines whether we want to run a headless service for the cluster. | *bool | false |
| publicIPSource | PublicIPSource specifies what source a process should use to get its public IPs.  This supports the values `pod` and `service`. | *PublicIPSource | false |
| podIPFamily | PodIPFamily tells the pod which family of IP addresses to use. You can use 4 to represent IPv4, and 6 to represent IPv6. This feature is only supported in FDB 7.0 or later, and requires dual-stack support in your Kubernetes environment. | *int | false |
| useDNSInClusterFile | UseDNSInClusterFile determines whether to use DNS names rather than IP addresses to identify coordinators in the cluster file. NOTE: This is an experimental feature, and is not supported in the latest stable version of FoundationDB. | *bool | false |
| dnsDomain | DNSDomain defines the cluster domain used in a DNS name generated for a service. The default is `cluster.local`. | *string | false |

[Back to TOC](#table-of-contents)

## ServiceConfig

ServiceConfig allows configuring services that sit in front of our pods. **Deprecated: Use RoutingConfig instead.**

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| headless | Headless determines whether we want to run a headless service for the cluster. | *bool | false |
| publicIPSource | PublicIPSource specifies what source a process should use to get its public IPs.  This supports the values `pod` and `service`. | *PublicIPSource | false |

[Back to TOC](#table-of-contents)

## VersionFlags

VersionFlags defines internal flags for new features in the database.

| Field | Description | Scheme | Required |
| ----- | ----------- | ------ | -------- |
| log_spill |  | int | false |
| log_version |  | int | false |

[Back to TOC](#table-of-contents)
