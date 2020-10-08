/*
 * pod_models.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2019 Apple Inc. and the FoundationDB project authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package controllers

import (
	ctx "context"
	"fmt"
	"regexp"
	"strings"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var processClassSanitizationPattern = regexp.MustCompile("[^a-z0-9-]")

// getInstanceID generates an ID for an instance.
//
// This will return the pod name and the instance ID.
func getInstanceID(cluster *fdbtypes.FoundationDBCluster, processClass string, idNum int) (string, string) {
	var instanceID string
	if cluster.Spec.InstanceIDPrefix != "" {
		instanceID = fmt.Sprintf("%s-%s-%d", cluster.Spec.InstanceIDPrefix, processClass, idNum)
	} else {
		instanceID = fmt.Sprintf("%s-%d", processClass, idNum)
	}
	return fmt.Sprintf("%s-%s-%d", cluster.Name, processClassSanitizationPattern.ReplaceAllString(processClass, "-"), idNum), instanceID
}

// GetPod builds a pod for a new instance
func GetPod(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, processClass string, idNum int, kubeClient client.Client) (*corev1.Pod, error) {
	name, id := getInstanceID(cluster, processClass, idNum)

	owner := buildOwnerReference(cluster.TypeMeta, cluster.ObjectMeta)
	spec, err := GetPodSpec(cluster, processClass, idNum)
	if err != nil {
		return nil, err
	}

	specHash, err := GetPodSpecHash(cluster, processClass, idNum, spec)
	if err != nil {
		return nil, err
	}

	metadata := getPodMetadata(cluster, processClass, id, specHash)
	metadata.Name = name
	metadata.OwnerReferences = owner

	return &corev1.Pod{
		ObjectMeta: metadata,
		Spec:       *spec,
	}, nil
}

// GetPodSpec builds a pod spec for a FoundationDB pod
func GetPodSpec(cluster *fdbtypes.FoundationDBCluster, processClass string, idNum int) (*corev1.PodSpec, error) {
	processSettings := cluster.GetProcessSettings(processClass)
	podSpec := processSettings.PodTemplate.Spec.DeepCopy()

	var mainContainer *corev1.Container
	var sidecarContainer *corev1.Container
	var initContainer *corev1.Container

	for index, container := range podSpec.Containers {
		if container.Name == "foundationdb" {
			mainContainer = &podSpec.Containers[index]
		} else if container.Name == "foundationdb-kubernetes-sidecar" {
			sidecarContainer = &podSpec.Containers[index]
		}
	}

	for index, container := range podSpec.InitContainers {
		if container.Name == "foundationdb-kubernetes-init" {
			initContainer = &podSpec.InitContainers[index]
		}
	}

	podName, instanceID := getInstanceID(cluster, processClass, idNum)

	if cluster.Spec.MainContainer.ImageName != "" {
		mainContainer.Image = cluster.Spec.MainContainer.ImageName
	}
	if mainContainer.Image == "" {
		mainContainer.Image = "foundationdb/foundationdb"
	}

	versionString := cluster.Status.RunningVersion
	if versionString == "" {
		versionString = cluster.Spec.Version
	}

	mainContainer.Image = fmt.Sprintf("%s:%s", mainContainer.Image, versionString)

	version, err := fdbtypes.ParseFdbVersion(versionString)
	if err != nil {
		return nil, err
	}

	extendEnv(mainContainer, corev1.EnvVar{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"})

	useCustomCAs := len(cluster.Spec.TrustedCAs) > 0
	if useCustomCAs {
		extendEnv(mainContainer, corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/dynamic-conf/ca.pem"})
	}

	logGroup := cluster.Spec.LogGroup
	if logGroup == "" {
		logGroup = cluster.Name
	}

	mainContainer.Command = []string{"sh", "-c"}
	mainContainer.Args = []string{
		"fdbmonitor --conffile /var/dynamic-conf/fdbmonitor.conf" +
			" --lockfile /var/dynamic-conf/fdbmonitor.lockfile" +
			" --loggroup " + logGroup +
			" >> /var/log/fdb-trace-logs/fdbmonitor-$(date '+%Y-%m-%d').log 2>&1",
	}

	if cluster.Spec.Resources != nil {
		mainContainer.Resources = *cluster.Spec.Resources
	}

	if mainContainer.Resources.Requests == nil {
		mainContainer.Resources.Requests = corev1.ResourceList{
			"cpu":    resource.MustParse("1"),
			"memory": resource.MustParse("1Gi"),
		}
	}

	if mainContainer.Resources.Limits == nil {
		mainContainer.Resources.Limits = mainContainer.Resources.Requests
	}

	mainContainer.VolumeMounts = append(mainContainer.VolumeMounts,
		corev1.VolumeMount{Name: "data", MountPath: "/var/fdb/data"},
		corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/dynamic-conf"},
		corev1.VolumeMount{Name: "fdb-trace-logs", MountPath: "/var/log/fdb-trace-logs"},
	)

	var readOnlyRootFilesystem = true
	if mainContainer.SecurityContext == nil {
		mainContainer.SecurityContext = &corev1.SecurityContext{}
	}
	mainContainer.SecurityContext.ReadOnlyRootFilesystem = &readOnlyRootFilesystem

	customizeContainer(mainContainer, cluster.Spec.MainContainer)

	customizeContainer(initContainer, cluster.Spec.SidecarContainer)
	err = configureSidecarContainerForCluster(cluster, initContainer, true, instanceID)
	if err != nil {
		return nil, err
	}

	customizeContainer(sidecarContainer, cluster.Spec.SidecarContainer)
	err = configureSidecarContainerForCluster(cluster, sidecarContainer, false, instanceID)
	if err != nil {
		return nil, err
	}

	var mainVolumeSource corev1.VolumeSource
	if usePvc(cluster, processClass) {
		var volumeClaimSourceName string
		if cluster.Spec.VolumeClaim != nil && cluster.Spec.VolumeClaim.Name != "" {
			volumeClaimSourceName = fmt.Sprintf("%s-%s", podName, cluster.Spec.VolumeClaim.Name)
		} else if processSettings.VolumeClaim != nil && processSettings.VolumeClaim.Name != "" {
			volumeClaimSourceName = fmt.Sprintf("%s-%s", podName, processSettings.VolumeClaim.Name)
		} else if processSettings.VolumeClaimTemplate != nil && processSettings.VolumeClaimTemplate.Name != "" {
			volumeClaimSourceName = fmt.Sprintf("%s-%s", podName, processSettings.VolumeClaimTemplate.Name)
		} else {
			volumeClaimSourceName = fmt.Sprintf("%s-data", podName)
		}
		mainVolumeSource.PersistentVolumeClaim = &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: volumeClaimSourceName,
		}
	} else {
		mainVolumeSource.EmptyDir = &corev1.EmptyDirVolumeSource{}
	}

	configMapItems := []corev1.KeyToPath{
		corev1.KeyToPath{Key: fmt.Sprintf("fdbmonitor-conf-%s", processClass), Path: "fdbmonitor.conf"},
		corev1.KeyToPath{Key: "cluster-file", Path: "fdb.cluster"},
	}

	if useCustomCAs {
		configMapItems = append(configMapItems, corev1.KeyToPath{Key: "ca-file", Path: "ca.pem"})
	}

	if !version.PrefersCommandLineArgumentsInSidecar() {
		configMapItems = append(configMapItems, corev1.KeyToPath{Key: "sidecar-conf", Path: "config.json"})
	}

	var configMapRefName string
	if cluster.Spec.ConfigMap != nil && cluster.Spec.ConfigMap.Name != "" {
		configMapRefName = fmt.Sprintf("%s-%s", cluster.Name, cluster.Spec.ConfigMap.Name)
	} else {
		configMapRefName = fmt.Sprintf("%s-config", cluster.Name)
	}

	volumes := []corev1.Volume{
		corev1.Volume{Name: "data", VolumeSource: mainVolumeSource},
		corev1.Volume{Name: "dynamic-conf", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		corev1.Volume{Name: "config-map", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: configMapRefName},
			Items:                configMapItems,
		}}},
		corev1.Volume{Name: "fdb-trace-logs", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}

	for _, volume := range cluster.Spec.Volumes {
		volumes = append(volumes, *volume.DeepCopy())
	}

	var affinity *corev1.Affinity

	faultDomainKey := cluster.Spec.FaultDomain.Key
	if faultDomainKey == "" {
		faultDomainKey = "kubernetes.io/hostname"
	}

	if faultDomainKey != "foundationdb.org/none" && faultDomainKey != "foundationdb.org/kubernetes-cluster" {
		affinity = &corev1.Affinity{
			PodAntiAffinity: &corev1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
					corev1.WeightedPodAffinityTerm{
						Weight: 1,
						PodAffinityTerm: corev1.PodAffinityTerm{
							TopologyKey: faultDomainKey,
							LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
								"fdb-cluster-name":  cluster.ObjectMeta.Name,
								"fdb-process-class": processClass,
							}},
						},
					},
				},
			},
		}
	}

	replaceContainers(podSpec.InitContainers, initContainer)
	podSpec.InitContainers = append(podSpec.InitContainers, cluster.Spec.InitContainers...)
	replaceContainers(podSpec.Containers, mainContainer, sidecarContainer)
	podSpec.Containers = append(podSpec.Containers, cluster.Spec.Containers...)
	podSpec.Volumes = append(podSpec.Volumes, volumes...)
	podSpec.Affinity = affinity

	if cluster.Spec.PodSecurityContext != nil {
		podSpec.SecurityContext = cluster.Spec.PodSecurityContext
	}
	if cluster.Spec.AutomountServiceAccountToken != nil {
		podSpec.AutomountServiceAccountToken = cluster.Spec.AutomountServiceAccountToken
	}

	headlessService, err := GetHeadlessService(cluster)
	if err != nil {
		return nil, err
	}

	if headlessService != nil {
		podSpec.Hostname = podName
		podSpec.Subdomain = headlessService.Name
	}

	return podSpec, nil
}

// configureSidecarContainerForCluster sets up a sidecar container for a sidecar
// in the FDB cluster.
func configureSidecarContainerForCluster(cluster *fdbtypes.FoundationDBCluster, container *corev1.Container, initMode bool, instanceID string) error {
	versionString := cluster.Status.RunningVersion
	if versionString == "" {
		versionString = cluster.Spec.Version
	}

	return configureSidecarContainer(container, initMode, instanceID, versionString, cluster.Spec.SidecarVariables, cluster.Spec.FaultDomain, cluster.Spec.SidecarContainer, cluster.Spec.SidecarVersions, cluster.Spec.SidecarVersion, len(cluster.Spec.TrustedCAs) > 0)
}

// configureSidecarContainerForBackup sets up a sidecar container for the init
// container for a backup process.
func configureSidecarContainerForBackup(backup *fdbtypes.FoundationDBBackup, container *corev1.Container) error {
	return configureSidecarContainer(container, true, "", backup.Spec.Version, nil, fdbtypes.FoundationDBClusterFaultDomain{}, fdbtypes.ContainerOverrides{}, nil, 0, false)
}

// configureSidecarContainer sets up a foundationdb-kubernetes-sidecar
// container.
func configureSidecarContainer(container *corev1.Container, initMode bool, instanceID string, versionString string, sidecarVariables []string, faultDomain fdbtypes.FoundationDBClusterFaultDomain, overrides fdbtypes.ContainerOverrides, sidecarVersions map[string]int, deprecatedSidecarVersion int, hasTrustedCAs bool) error {
	version, err := fdbtypes.ParseFdbVersion(versionString)
	if err != nil {
		return err
	}

	var sidecarVersion string
	if deprecatedSidecarVersion != 0 {
		sidecarVersion = fmt.Sprintf("%s-%d", versionString, deprecatedSidecarVersion)
	} else if sidecarVersions != nil && sidecarVersions[versionString] != 0 {
		sidecarVersion = fmt.Sprintf("%s-%d", versionString, sidecarVersions[versionString])
	} else {
		sidecarVersion = fmt.Sprintf("%s-1", versionString)
	}

	sidecarEnv := make([]corev1.EnvVar, 0, 4)

	fdbserverMode := instanceID != ""

	var sidecarArgs []string
	if version.PrefersCommandLineArgumentsInSidecar() {
		sidecarArgs = []string{
			"--copy-file", "fdb.cluster",
		}
		if hasTrustedCAs {
			sidecarArgs = append(sidecarArgs, "--copy-file", "ca.pem")
		}
		if fdbserverMode {
			sidecarArgs = append(sidecarArgs,
				"--input-monitor-conf", "fdbmonitor.conf",
				"--copy-binary", "fdbserver",
				"--copy-binary", "fdbcli",
			)
			if version.SupportsUsingBinariesFromMainContainer() {
				sidecarArgs = append(sidecarArgs,
					"--main-container-version", version.String(),
				)
			}
		}
	} else {
		sidecarArgs = make([]string, 0)
	}

	if version.HasSidecarCrashOnEmpty() && !fdbserverMode {
		sidecarArgs = append(sidecarArgs, "--require-not-empty")
		sidecarArgs = append(sidecarArgs, "fdb.cluster")
	}

	if !version.PrefersCommandLineArgumentsInSidecar() {
		if initMode {
			sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "COPY_ONCE", Value: "1"})
		}
		sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "SIDECAR_CONF_DIR", Value: "/var/input-files"})
	}

	if fdbserverMode {
		sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
		}})

		if version.PrefersCommandLineArgumentsInSidecar() {
			for _, substitution := range sidecarVariables {
				sidecarArgs = append(sidecarArgs, "--substitute-variable", substitution)
			}
			if !version.HasInstanceIDInSidecarSubstitutions() {
				sidecarArgs = append(sidecarArgs, "--substitute-variable", "FDB_INSTANCE_ID")
			}
		}

		faultDomainKey := faultDomain.Key
		if faultDomainKey == "" {
			faultDomainKey = "kubernetes.io/hostname"
		}

		faultDomainSource := faultDomain.ValueFrom
		if faultDomainSource == "" {
			faultDomainSource = "spec.nodeName"
		}

		if faultDomainKey == "foundationdb.org/none" {
			sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			}})
			sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			}})
		} else if faultDomainKey == "foundationdb.org/kubernetes-cluster" {
			sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
			}})
			sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "FDB_ZONE_ID", Value: faultDomain.Value})
		} else {
			sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
			}})
			if !strings.HasPrefix(faultDomainSource, "$") {
				sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: faultDomainSource},
				}})
			}
		}

		sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: instanceID})
	}

	if overrides.ImageName != "" {
		container.Image = overrides.ImageName
	}

	if version.PrefersCommandLineArgumentsInSidecar() && initMode {
		sidecarArgs = append(sidecarArgs, "--init-mode")
	}

	if container.Image == "" {
		container.Image = "foundationdb/foundationdb-kubernetes-sidecar"
	}

	extendEnv(container, sidecarEnv...)

	if overrides.EnableTLS && !initMode {
		sidecarArgs = append(sidecarArgs, "--tls")
	}

	if len(sidecarArgs) > 0 {
		container.Args = sidecarArgs
	}

	container.VolumeMounts = append(container.VolumeMounts,
		corev1.VolumeMount{Name: "config-map", MountPath: "/var/input-files"},
		corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/output-files"},
	)
	container.Image = fmt.Sprintf("%s:%s", container.Image, sidecarVersion)

	var readOnlyRootFilesystem = true
	if container.SecurityContext == nil {
		container.SecurityContext = &corev1.SecurityContext{}
	}
	container.SecurityContext.ReadOnlyRootFilesystem = &readOnlyRootFilesystem

	if initMode {
		return nil
	}

	extendEnv(container, corev1.EnvVar{Name: "FDB_TLS_VERIFY_PEERS", Value: overrides.PeerVerificationRules})

	if hasTrustedCAs {
		extendEnv(container, corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/input-files/ca.pem"})
	}

	if container.ReadinessProbe == nil {
		container.ReadinessProbe = &corev1.Probe{
			Handler: corev1.Handler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.IntOrString{IntVal: 8080},
				},
			},
		}
	}

	return nil
}

// usePvc determines whether we should attach a PVC to a pod.
func usePvc(cluster *fdbtypes.FoundationDBCluster, processClass string) bool {
	var storage *resource.Quantity
	processSettings := cluster.GetProcessSettings(processClass)
	if cluster.Spec.VolumeClaim != nil {
		requests := cluster.Spec.VolumeClaim.Spec.Resources.Requests
		if requests != nil {
			storageCopy := requests["storage"]
			storage = &storageCopy
		}
	}
	if processSettings.VolumeClaim != nil {
		requests := processSettings.VolumeClaim.Spec.Resources.Requests
		if requests != nil {
			storageCopy := requests["storage"]
			storage = &storageCopy
		}
	}
	if processSettings.VolumeClaimTemplate != nil {
		requests := processSettings.VolumeClaimTemplate.Spec.Resources.Requests
		if requests != nil {
			storageCopy := requests["storage"]
			storage = &storageCopy
		}
	}
	return cluster.Spec.VolumeSize != "0" && isStateful(processClass) &&
		(storage == nil || !storage.IsZero())
}

// isStateful determines whether a process class should store data.
func isStateful(processClass string) bool {
	return processClass == "storage" || processClass == "log" || processClass == "transaction"
}

// GetPvc builds a persistent volume claim for a FoundationDB instance.
func GetPvc(cluster *fdbtypes.FoundationDBCluster, processClass string, idNum int) (*corev1.PersistentVolumeClaim, error) {
	if !usePvc(cluster, processClass) {
		return nil, nil
	}
	name, id := getInstanceID(cluster, processClass, idNum)

	processSettings := cluster.GetProcessSettings(processClass)
	var pvc *corev1.PersistentVolumeClaim
	if cluster.Spec.VolumeClaim != nil {
		pvc = cluster.Spec.VolumeClaim.DeepCopy()
	} else if processSettings.VolumeClaim != nil {
		pvc = processSettings.VolumeClaim.DeepCopy()
	} else if processSettings.VolumeClaimTemplate != nil {
		pvc = processSettings.VolumeClaimTemplate.DeepCopy()
	} else {
		pvc = &corev1.PersistentVolumeClaim{}
	}

	pvc.ObjectMeta = getPvcMetadata(cluster, processClass, id)
	if pvc.ObjectMeta.Name == "" {
		pvc.ObjectMeta.Name = fmt.Sprintf("%s-data", name)
	} else {
		pvc.ObjectMeta.Name = fmt.Sprintf("%s-%s", name, pvc.ObjectMeta.Name)
	}

	if pvc.Spec.AccessModes == nil {
		pvc.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	}

	if pvc.Spec.Resources.Requests == nil {
		pvc.Spec.Resources.Requests = corev1.ResourceList{}
	}

	if cluster.Spec.VolumeSize != "" {
		size, err := resource.ParseQuantity(cluster.Spec.VolumeSize)
		if err != nil {
			return nil, err
		}
		pvc.Spec.Resources.Requests["storage"] = size
	}

	storage := pvc.Spec.Resources.Requests["storage"]
	if (&storage).IsZero() {
		pvc.Spec.Resources.Requests["storage"] = resource.MustParse("128G")
	}

	if cluster.Spec.StorageClass != nil {
		pvc.Spec.StorageClassName = cluster.Spec.StorageClass
	}

	specHash, err := GetJSONHash(pvc.Spec)
	if err != nil {
		return nil, err
	}

	if pvc.ObjectMeta.Annotations == nil {
		pvc.ObjectMeta.Annotations = make(map[string]string, 1)
	}
	pvc.ObjectMeta.Annotations[LastSpecKey] = specHash

	return pvc, nil
}

// replaceContainers overwrites the containers in a list with new containers
// that have the same name.
func replaceContainers(containers []corev1.Container, newContainers ...*corev1.Container) {
	for index, container := range containers {
		for _, newContainer := range newContainers {
			if container.Name == newContainer.Name {
				containers[index] = *newContainer
			}
		}
	}
}

// extendEnv adds environment variables to an existing environment, unless
// environment variables with the same name are already present.
func extendEnv(container *corev1.Container, env ...corev1.EnvVar) {
	existingVars := make(map[string]bool, len(container.Env))

	for _, envVar := range container.Env {
		existingVars[envVar.Name] = true
	}

	for _, envVar := range env {
		if !existingVars[envVar.Name] {
			container.Env = append(container.Env, envVar)
		}
	}
}

// customizeContainer adds container overrides from the cluster spec to a
// container.
func customizeContainer(container *corev1.Container, overrides fdbtypes.ContainerOverrides) {
	envOverrides := make(map[string]bool)

	fullEnv := []corev1.EnvVar{}

	for _, envVar := range overrides.Env {
		fullEnv = append(fullEnv, *envVar.DeepCopy())
		envOverrides[envVar.Name] = true
	}

	for _, envVar := range container.Env {
		if !envOverrides[envVar.Name] {
			fullEnv = append(fullEnv, envVar)
		}
	}

	container.Env = fullEnv

	for _, volume := range overrides.VolumeMounts {
		container.VolumeMounts = append(container.VolumeMounts, *volume.DeepCopy())
	}

	if overrides.SecurityContext != nil {
		container.SecurityContext = overrides.SecurityContext
	}
}

// GetBackupDeployment builds a deployment for backup agents for a cluster.
func GetBackupDeployment(context ctx.Context, backup *fdbtypes.FoundationDBBackup, kubeClient client.Client) (*appsv1.Deployment, error) {
	agentCount := int32(backup.GetDesiredAgentCount())
	if agentCount == 0 {
		return nil, nil
	}
	deploymentName := fmt.Sprintf("%s-backup-agents", backup.ObjectMeta.Name)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   backup.ObjectMeta.Namespace,
			Name:        deploymentName,
			Annotations: map[string]string{},
			Labels:      map[string]string{},
		},
	}
	deployment.Spec.Replicas = &agentCount
	deployment.ObjectMeta.OwnerReferences = buildOwnerReference(backup.TypeMeta, backup.ObjectMeta)

	if backup.Spec.BackupDeploymentMetadata != nil {
		for key, value := range backup.Spec.BackupDeploymentMetadata.Labels {
			deployment.ObjectMeta.Labels[key] = value
		}
		for key, value := range backup.Spec.BackupDeploymentMetadata.Annotations {
			deployment.ObjectMeta.Annotations[key] = value
		}
	}
	deployment.ObjectMeta.Labels[BackupDeploymentLabel] = string(backup.ObjectMeta.UID)

	var podTemplate *corev1.PodTemplateSpec
	if backup.Spec.PodTemplateSpec != nil {
		podTemplate = backup.Spec.PodTemplateSpec.DeepCopy()
	} else {
		podTemplate = &corev1.PodTemplateSpec{}
	}

	var mainContainer *corev1.Container
	var initContainer *corev1.Container

	for index, container := range podTemplate.Spec.Containers {
		if container.Name == "foundationdb" {
			mainContainer = &podTemplate.Spec.Containers[index]
		}
	}

	for index, container := range podTemplate.Spec.InitContainers {
		if container.Name == "foundationdb-kubernetes-init" {
			initContainer = &podTemplate.Spec.InitContainers[index]
		}
	}

	if mainContainer == nil {
		containers := []corev1.Container{}
		containers = append(containers, corev1.Container{Name: "foundationdb"})
		containers = append(containers, podTemplate.Spec.Containers...)
		mainContainer = &containers[0]
		podTemplate.Spec.Containers = containers
	}

	if mainContainer.Image == "" {
		mainContainer.Image = "foundationdb/foundationdb"
	}
	mainContainer.Image = fmt.Sprintf("%s:%s", mainContainer.Image, backup.Spec.Version)
	mainContainer.Command = []string{"backup_agent"}
	mainContainer.Args = []string{"--log", "--logdir", "/var/log/fdb-trace-logs"}
	if mainContainer.Env == nil {
		mainContainer.Env = make([]corev1.EnvVar, 0, 1)
	}

	extendEnv(mainContainer, corev1.EnvVar{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"})

	mainContainer.VolumeMounts = append(mainContainer.VolumeMounts,
		corev1.VolumeMount{Name: "logs", MountPath: "/var/log/fdb-trace-logs"},
		corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/dynamic-conf"},
	)

	if mainContainer.Resources.Requests == nil {
		mainContainer.Resources.Requests = corev1.ResourceList{
			"cpu":    resource.MustParse("1"),
			"memory": resource.MustParse("1Gi"),
		}
	}

	if mainContainer.Resources.Limits == nil {
		mainContainer.Resources.Limits = mainContainer.Resources.Requests
	}

	if initContainer == nil {
		podTemplate.Spec.InitContainers = append(podTemplate.Spec.InitContainers, corev1.Container{Name: "foundationdb-kubernetes-init"})
		initContainer = &podTemplate.Spec.InitContainers[0]
	}

	err := configureSidecarContainerForBackup(backup, initContainer)
	if err != nil {
		return nil, err
	}

	if podTemplate.ObjectMeta.Labels == nil {
		podTemplate.ObjectMeta.Labels = make(map[string]string, 1)
	}
	podTemplate.ObjectMeta.Labels["foundationdb.org/deployment-name"] = deployment.ObjectMeta.Name
	deployment.Spec.Selector = &metav1.LabelSelector{MatchLabels: map[string]string{
		"foundationdb.org/deployment-name": deployment.ObjectMeta.Name,
	}}

	podTemplate.Spec.Volumes = append(podTemplate.Spec.Volumes,
		corev1.Volume{Name: "logs", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		corev1.Volume{Name: "dynamic-conf", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		corev1.Volume{
			Name: "config-map",
			VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: fmt.Sprintf("%s-config", backup.Spec.ClusterName)},
				Items: []corev1.KeyToPath{
					{Key: "cluster-file", Path: "fdb.cluster"},
				},
			}},
		},
	)

	deployment.Spec.Template = *podTemplate

	specHash, err := GetJSONHash(deployment.Spec)
	if err != nil {
		return nil, err
	}

	deployment.ObjectMeta.Annotations[LastSpecKey] = specHash

	return deployment, nil
}

// GetHeadlessService builds a headless service for a FoundationDB cluster.
func GetHeadlessService(cluster *fdbtypes.FoundationDBCluster) (*corev1.Service, error) {
	headless := cluster.Spec.Services.Headless
	if headless == nil || !*headless {
		return nil, nil
	}

	service := &corev1.Service{
		ObjectMeta: getObjectMetadata(cluster, nil, "", ""),
	}
	service.ObjectMeta.Name = cluster.ObjectMeta.Name
	service.Spec.ClusterIP = "None"
	service.Spec.Selector = map[string]string{"fdb-cluster-name": cluster.Name}

	return service, nil
}

// ensureContainerPresent looks for a container by name from a list, and adds
// an empty container with that name if none is present.
func ensureContainerPresent(containers []corev1.Container, name string, insertIndex int) ([]corev1.Container, int) {
	for index, container := range containers {
		if container.Name == name {
			return containers, index
		}
	}

	if insertIndex < 0 || insertIndex >= len(containers) {
		containers = append(containers, corev1.Container{Name: name})
		return containers, len(containers) - 1
	}
	containerCount := 1 + len(containers)
	newContainers := make([]corev1.Container, 0, containerCount)
	for indexToCopy := 0; indexToCopy < len(containers); indexToCopy++ {
		if indexToCopy == insertIndex {
			newContainers = append(newContainers, corev1.Container{
				Name: name,
			})
		}
		newContainers = append(newContainers, containers[indexToCopy])
	}

	return newContainers, insertIndex
}

// customizeContainerFromList finds a container by name and runs a customization
// function on the container.
func customizeContainerFromList(containers []corev1.Container, name string, customizer func(*corev1.Container)) []corev1.Container {
	containers, index := ensureContainerPresent(containers, name, -1)
	container := containers[index]
	customizer(&container)
	containers[index] = container
	return containers
}

// NormalizeClusterSpec converts a cluster spec into an unambiguous,
// future-proof form, by applying any implicit defaults and moving configuration
// from deprecated fields into fully-supported fields.
func NormalizeClusterSpec(spec *fdbtypes.FoundationDBClusterSpec, defaults defaultsSelection) {
	if spec.PodTemplate != nil {
		if spec.Processes == nil {
			spec.Processes = make(map[string]fdbtypes.ProcessSettings)
		}
		generalSettings := spec.Processes["general"]
		if generalSettings.PodTemplate == nil {
			generalSettings.PodTemplate = spec.PodTemplate
		}
		spec.Processes["general"] = generalSettings
		spec.PodTemplate = nil
	}

	if !defaults.OnlyShowChanges {
		// Set up resource requirements for the main container.

		if spec.Processes == nil {
			spec.Processes = make(map[string]fdbtypes.ProcessSettings)
		}
		_, present := spec.Processes["general"]
		if !present {
			spec.Processes["general"] = fdbtypes.ProcessSettings{}
		}

		for processClass, settings := range spec.Processes {
			if settings.PodTemplate == nil {
				settings.PodTemplate = &corev1.PodTemplateSpec{}
			}

			settings.PodTemplate.Spec.Containers, _ = ensureContainerPresent(settings.PodTemplate.Spec.Containers, "foundationdb", 0)

			settings.PodTemplate.Spec.Containers = customizeContainerFromList(settings.PodTemplate.Spec.Containers, "foundationdb", func(container *corev1.Container) {
				if container.Resources.Requests == nil {
					container.Resources.Requests = corev1.ResourceList{
						"cpu":    resource.MustParse("1"),
						"memory": resource.MustParse("1Gi"),
					}
				}

				if container.Resources.Limits == nil {
					container.Resources.Limits = container.Resources.Requests
				}
			})

			spec.Processes[processClass] = settings
		}
	}

	// Apply changes between old and new defaults.
	// When we update the defaults in the next release, the following sections
	// should be moved under the `!OnlyShowChanges` section, and we should use
	// the latest defaults as the active defaults.

	// Set up sidecar resource requirements
	if spec.Processes == nil {
		spec.Processes = make(map[string]fdbtypes.ProcessSettings)
	}
	_, present := spec.Processes["general"]
	if !present {
		spec.Processes["general"] = fdbtypes.ProcessSettings{}
	}

	for processClass, settings := range spec.Processes {
		if settings.PodTemplate == nil {
			settings.PodTemplate = &corev1.PodTemplateSpec{}
		}

		sidecarUpdater := func(container *corev1.Container) {
			if defaults.UseFutureDefaults {
				if container.Resources.Requests == nil {
					container.Resources.Requests = corev1.ResourceList{
						"cpu":    resource.MustParse("100m"),
						"memory": resource.MustParse("256Mi"),
					}
				}
				if container.Resources.Limits == nil {
					container.Resources.Limits = container.Resources.Requests
				}
			} else {
				if container.Resources.Requests == nil {
					container.Resources.Requests = corev1.ResourceList{}
				}
				if container.Resources.Limits == nil {
					container.Resources.Limits = corev1.ResourceList{}
				}
			}
		}

		settings.PodTemplate.Spec.InitContainers = customizeContainerFromList(settings.PodTemplate.Spec.InitContainers, "foundationdb-kubernetes-init", sidecarUpdater)
		settings.PodTemplate.Spec.Containers = customizeContainerFromList(settings.PodTemplate.Spec.Containers, "foundationdb-kubernetes-sidecar", sidecarUpdater)

		spec.Processes[processClass] = settings
	}
}
