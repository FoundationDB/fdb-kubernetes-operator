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

package internal

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
)

var processClassSanitizationPattern = regexp.MustCompile("[^a-z0-9-]")

// GetProcessGroupIDFromPodName returns the process group ID for a given Pod name.
func GetProcessGroupIDFromPodName(cluster *fdbv1beta2.FoundationDBCluster, podName string) fdbv1beta2.ProcessGroupID {
	tmpName := strings.ReplaceAll(podName, cluster.Name, "")[1:]

	if cluster.Spec.ProcessGroupIDPrefix != "" {
		return fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-%s", cluster.Spec.ProcessGroupIDPrefix, tmpName))
	}

	return fdbv1beta2.ProcessGroupID(tmpName)
}

// GetProcessGroupID generates an ID for a process group.
//
// This will return the pod name and the processGroupID.
func GetProcessGroupID(cluster *fdbv1beta2.FoundationDBCluster, processClass fdbv1beta2.ProcessClass, idNum int) (string, fdbv1beta2.ProcessGroupID) {
	var processGroupID fdbv1beta2.ProcessGroupID
	if cluster.Spec.ProcessGroupIDPrefix != "" {
		processGroupID = fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-%s-%d", cluster.Spec.ProcessGroupIDPrefix, processClass, idNum))
	} else {
		processGroupID = fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-%d", processClass, idNum))
	}
	return fmt.Sprintf("%s-%s-%d", cluster.Name, processClassSanitizationPattern.ReplaceAllString(string(processClass), "-"), idNum), processGroupID
}

func generateServicePorts(processesPerPod int) []corev1.ServicePort {
	ports := make([]corev1.ServicePort, 0, processesPerPod*2)

	for i := 1; i <= processesPerPod; i++ {
		tlsPortName := "tls"
		nonTlSPortName := "non-tls"

		// We keep the current behaviour and only add the process number to ports for
		// processes > 1.
		if i != 1 {
			tlsPortName = fmt.Sprintf("%s-%d", tlsPortName, i)
			nonTlSPortName = fmt.Sprintf("%s-%d", nonTlSPortName, i)
		}

		ports = append(ports, corev1.ServicePort{
			Name: tlsPortName,
			Port: int32(fdbv1beta2.GetProcessPort(i, true)),
		}, corev1.ServicePort{
			Name: nonTlSPortName,
			Port: int32(fdbv1beta2.GetProcessPort(i, false)),
		})
	}

	return ports
}

// GetService builds a service for a new process group
func GetService(cluster *fdbv1beta2.FoundationDBCluster, processClass fdbv1beta2.ProcessClass, idNum int) (*corev1.Service, error) {
	name, id := GetProcessGroupID(cluster, processClass, idNum)

	owner := BuildOwnerReference(cluster.TypeMeta, cluster.ObjectMeta)
	metadata := GetObjectMetadata(cluster, nil, processClass, id)
	metadata.Name = name
	metadata.OwnerReferences = owner

	processesPerPod := 1
	if processClass == fdbv1beta2.ProcessClassStorage {
		processesPerPod = cluster.GetStorageServersPerPod()
	}

	return &corev1.Service{
		ObjectMeta: metadata,
		Spec: corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeClusterIP,
			Ports:                    generateServicePorts(processesPerPod),
			PublishNotReadyAddresses: true,
			Selector:                 GetPodMatchLabels(cluster, "", string(id)),
		},
	}, nil
}

// GetPod builds a pod for a new process group
func GetPod(cluster *fdbv1beta2.FoundationDBCluster, processClass fdbv1beta2.ProcessClass, idNum int) (*corev1.Pod, error) {
	name, id := GetProcessGroupID(cluster, processClass, idNum)

	owner := BuildOwnerReference(cluster.TypeMeta, cluster.ObjectMeta)
	spec, err := GetPodSpec(cluster, processClass, idNum)
	if err != nil {
		return nil, err
	}

	specHash, err := GetPodSpecHash(cluster, processClass, idNum, spec)
	if err != nil {
		return nil, err
	}

	metadata := GetPodMetadata(cluster, processClass, id, specHash)
	metadata.Name = name
	metadata.OwnerReferences = owner

	return &corev1.Pod{
		ObjectMeta: metadata,
		Spec:       *spec,
	}, nil
}

// GetImage returns the image for container
func GetImage(image string, configs []fdbv1beta2.ImageConfig, versionString string, allowTagOverride bool) (string, error) {
	if image != "" {
		imageComponents := strings.Split(image, ":")
		if len(imageComponents) > 1 {
			if allowTagOverride {
				return image, nil
			}
			// If the specified image contains a tag and allowOverride is false return an error
			return "", fmt.Errorf("image should not contain a tag but contains the tag \"%s\", please remove the tag", imageComponents[1])
		}
		configs = append([]fdbv1beta2.ImageConfig{{BaseImage: image}}, configs...)
	}

	return fdbv1beta2.SelectImageConfig(configs, versionString).Image(), nil
}

// getInitContainer returns the init container based on the provided PodSpec. If useUnifiedImages is true the init container
// will be an empty container struct.
func getInitContainer(useUnifiedImages bool, podSpec *corev1.PodSpec) (*corev1.Container, error) {
	if useUnifiedImages {
		return &corev1.Container{}, nil
	}

	for index, container := range podSpec.InitContainers {
		if container.Name == fdbv1beta2.InitContainerName {
			return &podSpec.InitContainers[index], nil
		}
	}

	return nil, fmt.Errorf("could not create init container")
}

// getContainers returns the main and the sidecar container or an error if one of these is empty.
func getContainers(podSpec *corev1.PodSpec) (*corev1.Container, *corev1.Container, error) {
	var mainContainer *corev1.Container
	var sidecarContainer *corev1.Container

	for index, container := range podSpec.Containers {
		if container.Name == fdbv1beta2.MainContainerName {
			mainContainer = &podSpec.Containers[index]
		} else if container.Name == fdbv1beta2.SidecarContainerName {
			sidecarContainer = &podSpec.Containers[index]
		}
	}

	if mainContainer == nil {
		return nil, nil, fmt.Errorf("could not create main container")
	}

	if sidecarContainer == nil {
		return nil, nil, fmt.Errorf("could not create sidecar container")
	}

	return mainContainer, sidecarContainer, nil
}

func configureContainersForUnifiedImages(cluster *fdbv1beta2.FoundationDBCluster, mainContainer *corev1.Container, sidecarContainer *corev1.Container, processGroupID fdbv1beta2.ProcessGroupID, processClass fdbv1beta2.ProcessClass) error {
	mainContainer.Args = []string{
		"--input-dir", "/var/dynamic-conf",
		"--log-path", "/var/log/fdb-trace-logs/monitor.log",
	}

	if cluster.Spec.StorageServersPerPod > 1 && processClass == fdbv1beta2.ProcessClassStorage {
		storageServers := strconv.Itoa(cluster.Spec.StorageServersPerPod)
		mainContainer.Args = append(mainContainer.Args, "--process-count", storageServers)
		mainContainer.Env = append(mainContainer.Env, corev1.EnvVar{Name: "STORAGE_SERVERS_PER_POD", Value: storageServers})
	}

	if cluster.Spec.LogProcessesPerPod > 1 && processClass == fdbv1beta2.ProcessClassLog {
		logServers := strconv.Itoa(cluster.Spec.LogProcessesPerPod)
		mainContainer.Args = append(mainContainer.Args, "--process-count", logServers)
		mainContainer.Env = append(mainContainer.Env, corev1.EnvVar{Name: "LOG_SERVERS_PER_POD", Value: logServers})
	}

	mainContainer.VolumeMounts = append(mainContainer.VolumeMounts,
		corev1.VolumeMount{Name: "data", MountPath: "/var/fdb/data"},
		corev1.VolumeMount{Name: "config-map", MountPath: "/var/dynamic-conf"},
		corev1.VolumeMount{Name: "shared-binaries", MountPath: "/var/fdb/shared-binaries"},
		corev1.VolumeMount{Name: "fdb-trace-logs", MountPath: "/var/log/fdb-trace-logs"},
	)

	mainContainer.Env = append(mainContainer.Env, getEnvForMonitorConfigSubstitution(cluster, processGroupID)...)
	mainContainer.Env = append(mainContainer.Env, corev1.EnvVar{Name: "FDB_IMAGE_TYPE", Value: string(FDBImageTypeUnified)})
	mainContainer.Env = append(mainContainer.Env, corev1.EnvVar{Name: "FDB_POD_NAME", ValueFrom: &corev1.EnvVarSource{
		FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
	}})
	mainContainer.Env = append(mainContainer.Env, corev1.EnvVar{Name: "FDB_POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{
		FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
	}})

	// Configure sidecar
	sidecarImage, err := GetImage(sidecarContainer.Image, cluster.Spec.MainContainer.ImageConfigs, cluster.GetRunningVersion(), false)
	if err != nil {
		return err
	}

	sidecarContainer.Image = sidecarImage
	sidecarContainer.Args = []string{
		"--mode", "sidecar",
		"--output-dir", "/var/fdb/shared-binaries",
		"--main-container-version", cluster.GetRunningVersion(),
		"--copy-binary", "fdbserver",
		"--copy-binary", "fdbcli",
		"--log-path", "/var/log/fdb-trace-logs/monitor.log",
	}

	sidecarContainer.VolumeMounts = append(sidecarContainer.VolumeMounts,
		corev1.VolumeMount{Name: "shared-binaries", MountPath: "/var/fdb/shared-binaries"},
		corev1.VolumeMount{Name: "fdb-trace-logs", MountPath: "/var/log/fdb-trace-logs"},
	)

	for _, crashObjs := range cluster.Spec.Buggify.CrashLoopContainers {
		for _, pid := range crashObjs.Targets {
			if pid == processGroupID || pid == "*" {
				if crashObjs.ContainerName == mainContainer.Name {
					mainContainer.Command = []string{"crash-loop"}
					mainContainer.Args = []string{"crash-loop"}
				} else if crashObjs.ContainerName == sidecarContainer.Name {
					sidecarContainer.Command = []string{"crash-loop"}
					sidecarContainer.Args = []string{"crash-loop"}
				}
			}
		}
	}

	return nil
}

// ensureSecurityContextIsPresent sets the SecurityContext for a container is absent and ensures the ReadOnlyRootFilesystem
// is set to true if not set.
func ensureSecurityContextIsPresent(container *corev1.Container) {
	if container.SecurityContext == nil {
		container.SecurityContext = &corev1.SecurityContext{}
	}

	if container.SecurityContext.ReadOnlyRootFilesystem == nil {
		container.SecurityContext.ReadOnlyRootFilesystem = pointer.Bool(true)
	}
}

func setAffinityForFaultDomain(cluster *fdbv1beta2.FoundationDBCluster, podSpec *corev1.PodSpec, processClass fdbv1beta2.ProcessClass) {
	faultDomainKey := cluster.Spec.FaultDomain.Key
	if faultDomainKey == "" {
		faultDomainKey = corev1.LabelHostname
	}

	if faultDomainKey != fdbv1beta2.NoneFaultDomainKey && faultDomainKey != "foundationdb.org/kubernetes-cluster" {
		if podSpec.Affinity == nil {
			podSpec.Affinity = &corev1.Affinity{}
		}

		if podSpec.Affinity.PodAntiAffinity == nil {
			podSpec.Affinity.PodAntiAffinity = &corev1.PodAntiAffinity{}
		}

		labelSelectors := make(map[string]string, len(cluster.GetMatchLabels())+1)
		for key, value := range cluster.GetMatchLabels() {
			labelSelectors[key] = value
		}

		processClassLabel := cluster.GetProcessClassLabel()
		labelSelectors[processClassLabel] = string(processClass)

		podSpec.Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution = append(podSpec.Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution,
			corev1.WeightedPodAffinityTerm{
				Weight: 1,
				PodAffinityTerm: corev1.PodAffinityTerm{
					TopologyKey:   faultDomainKey,
					LabelSelector: &metav1.LabelSelector{MatchLabels: labelSelectors},
				},
			})
	}
}

func getServersPerPod(cluster *fdbv1beta2.FoundationDBCluster, pClass fdbv1beta2.ProcessClass) int {
	if pClass == fdbv1beta2.ProcessClassStorage {
		return cluster.GetStorageServersPerPod()
	} else if pClass == fdbv1beta2.ProcessClassLog {
		return cluster.GetLogServersPerPod()
	}
	return 1
}

func configureVolumesForContainers(cluster *fdbv1beta2.FoundationDBCluster, podSpec *corev1.PodSpec, volumeClaimTemplate *corev1.PersistentVolumeClaim, podName string, processClass fdbv1beta2.ProcessClass) {
	useUnifiedImages := pointer.BoolDeref(cluster.Spec.UseUnifiedImage, false)
	monitorConfKey := GetConfigMapMonitorConfEntry(processClass, GetDesiredImageType(cluster), getServersPerPod(cluster, processClass))

	var monitorConfFile string
	if useUnifiedImages {
		monitorConfFile = "config.json"
	} else {
		monitorConfFile = "fdbmonitor.conf"
	}

	configMapItems := []corev1.KeyToPath{
		{Key: monitorConfKey, Path: monitorConfFile},
		{Key: ClusterFileKey, Path: "fdb.cluster"},
	}

	if len(cluster.Spec.TrustedCAs) > 0 {
		configMapItems = append(configMapItems, corev1.KeyToPath{Key: "ca-file", Path: "ca.pem"})
	}

	var configMapRefName string
	if cluster.Spec.ConfigMap != nil && cluster.Spec.ConfigMap.Name != "" {
		configMapRefName = fmt.Sprintf("%s-%s", cluster.Name, cluster.Spec.ConfigMap.Name)
	} else {
		configMapRefName = fmt.Sprintf("%s-config", cluster.Name)
	}

	var mainVolumeSource corev1.VolumeSource
	if usePvc(cluster, processClass) {
		var volumeClaimSourceName string
		if volumeClaimTemplate != nil && volumeClaimTemplate.Name != "" {
			volumeClaimSourceName = fmt.Sprintf("%s-%s", podName, volumeClaimTemplate.Name)
		} else {
			volumeClaimSourceName = fmt.Sprintf("%s-data", podName)
		}
		mainVolumeSource.PersistentVolumeClaim = &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: volumeClaimSourceName,
		}
	} else {
		mainVolumeSource.EmptyDir = &corev1.EmptyDirVolumeSource{}
	}

	volumes := []corev1.Volume{
		{Name: "data", VolumeSource: mainVolumeSource},
	}

	if useUnifiedImages {
		volumes = append(volumes, corev1.Volume{Name: "shared-binaries", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}})
	} else {
		volumes = append(volumes, corev1.Volume{Name: "dynamic-conf", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}})
	}
	volumes = append(volumes,
		corev1.Volume{Name: "config-map", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: configMapRefName},
			Items:                configMapItems,
		}}},
		corev1.Volume{Name: "fdb-trace-logs", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	)

	podSpec.Volumes = append(podSpec.Volumes, volumes...)
}

func configureNoSchedule(podSpec *corev1.PodSpec, processGroupID fdbv1beta2.ProcessGroupID, noSchedules []fdbv1beta2.ProcessGroupID) {
	for _, noSchedulePID := range noSchedules {
		if processGroupID != noSchedulePID {
			continue
		}

		if podSpec.Affinity == nil {
			podSpec.Affinity = &corev1.Affinity{}
		}

		if podSpec.Affinity.NodeAffinity == nil {
			podSpec.Affinity.NodeAffinity = &corev1.NodeAffinity{}
		}

		if podSpec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
			podSpec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = &corev1.NodeSelector{}
		}

		podSpec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = []corev1.NodeSelectorTerm{
			{
				MatchExpressions: []corev1.NodeSelectorRequirement{{
					Key:      fdbv1beta2.NodeSelectorNoScheduleLabel,
					Operator: corev1.NodeSelectorOpIn,
					Values:   []string{"true"},
				}},
			},
		}
	}
}

// GetPodSpec builds a pod spec for a FoundationDB pod
func GetPodSpec(cluster *fdbv1beta2.FoundationDBCluster, processClass fdbv1beta2.ProcessClass, idNum int) (*corev1.PodSpec, error) {
	processSettings := cluster.GetProcessSettings(processClass)
	podSpec := processSettings.PodTemplate.Spec.DeepCopy()
	useUnifiedImages := pointer.BoolDeref(cluster.Spec.UseUnifiedImage, false)

	mainContainer, sidecarContainer, err := getContainers(podSpec)
	if err != nil {
		return nil, err
	}

	initContainer, err := getInitContainer(useUnifiedImages, podSpec)
	if err != nil {
		return nil, err
	}

	podName, processGroupID := GetProcessGroupID(cluster, processClass, idNum)
	desiredVersion := cluster.GetRunningVersion()
	if cluster.VersionCompatibleUpgradeInProgress() {
		desiredVersion = cluster.Spec.Version
	}

	image, err := GetImage(mainContainer.Image, cluster.Spec.MainContainer.ImageConfigs, desiredVersion, false)
	if err != nil {
		return nil, err
	}
	mainContainer.Image = image

	extendEnv(mainContainer, corev1.EnvVar{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"})

	if len(cluster.Spec.TrustedCAs) > 0 {
		extendEnv(mainContainer, corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/dynamic-conf/ca.pem"})
	}

	logGroup := cluster.Spec.LogGroup
	if logGroup == "" {
		logGroup = cluster.Name
	}

	if useUnifiedImages {
		err = configureContainersForUnifiedImages(cluster, mainContainer, sidecarContainer, processGroupID, processClass)
		if err != nil {
			return nil, err
		}
	} else {
		mainContainer.Command = []string{"sh", "-c"}

		args := "fdbmonitor --conffile /var/dynamic-conf/fdbmonitor.conf" +
			" --lockfile /var/dynamic-conf/fdbmonitor.lockfile" +
			" --loggroup " + logGroup +
			" >> /var/log/fdb-trace-logs/fdbmonitor-$(date '+%Y-%m-%d').log 2>&1"

		for _, crashObjs := range cluster.Spec.Buggify.CrashLoopContainers {
			for _, pid := range crashObjs.Targets {
				if (pid == processGroupID || pid == "*") && crashObjs.ContainerName == mainContainer.Name {
					args = "crash-loop"
					break
				}
			}
		}

		mainContainer.Args = []string{args}
		mainContainer.VolumeMounts = append(mainContainer.VolumeMounts,
			corev1.VolumeMount{Name: "data", MountPath: "/var/fdb/data"},
			corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/dynamic-conf"},
			corev1.VolumeMount{Name: "fdb-trace-logs", MountPath: "/var/log/fdb-trace-logs"},
		)

		err = configureSidecarContainerForCluster(cluster, podName, initContainer, true, processGroupID, desiredVersion)
		if err != nil {
			return nil, err
		}

		err = configureSidecarContainerForCluster(cluster, podName, sidecarContainer, false, processGroupID, desiredVersion)
		if err != nil {
			return nil, err
		}

		if processClass == fdbv1beta2.ProcessClassStorage && cluster.GetStorageServersPerPod() > 1 {
			sidecarContainer.Env = append(sidecarContainer.Env, corev1.EnvVar{Name: "STORAGE_SERVERS_PER_POD", Value: fmt.Sprintf("%d", cluster.GetStorageServersPerPod())})
		}

		if processClass == fdbv1beta2.ProcessClassLog && cluster.GetLogServersPerPod() > 1 {
			sidecarContainer.Env = append(sidecarContainer.Env, corev1.EnvVar{Name: "LOG_SERVERS_PER_POD", Value: fmt.Sprintf("%d", cluster.GetLogServersPerPod())})
		}
	}

	ensureSecurityContextIsPresent(mainContainer)
	ensureSecurityContextIsPresent(sidecarContainer)
	setAffinityForFaultDomain(cluster, podSpec, processClass)
	configureVolumesForContainers(cluster, podSpec, processSettings.VolumeClaimTemplate, podName, processClass)
	configureNoSchedule(podSpec, processGroupID, cluster.Spec.Buggify.NoSchedule)

	if !useUnifiedImages {
		replaceContainers(podSpec.InitContainers, initContainer)
	}
	replaceContainers(podSpec.Containers, mainContainer, sidecarContainer)

	headlessService := GetHeadlessService(cluster)

	if headlessService != nil {
		podSpec.Hostname = podName
		podSpec.Subdomain = headlessService.Name
	}

	return podSpec, nil
}

// configureSidecarContainerForCluster sets up a sidecar container for a sidecar
// in the FDB cluster.
func configureSidecarContainerForCluster(cluster *fdbv1beta2.FoundationDBCluster, podName string, container *corev1.Container, initMode bool, processGroupID fdbv1beta2.ProcessGroupID, fdbVersion string) error {
	return configureSidecarContainer(container, initMode, processGroupID, podName, fdbVersion, cluster, cluster.Spec.SidecarContainer.ImageConfigs, false)
}

// configureSidecarContainerForBackup sets up a sidecar container for the init
// container for a backup process.
func configureSidecarContainerForBackup(backup *fdbv1beta2.FoundationDBBackup, container *corev1.Container) error {
	return configureSidecarContainer(container, true, "", "", backup.Spec.Version, nil, backup.Spec.SidecarContainer.ImageConfigs, pointer.BoolDeref(backup.Spec.AllowTagOverride, false))
}

// configureSidecarContainer sets up a foundationdb-kubernetes-sidecar container.
func configureSidecarContainer(container *corev1.Container, initMode bool, processGroupID fdbv1beta2.ProcessGroupID, podName string, versionString string, optionalCluster *fdbv1beta2.FoundationDBCluster, imageConfigs []fdbv1beta2.ImageConfig, allowTagOverride bool) error {
	sidecarEnv := make([]corev1.EnvVar, 0, 4)

	hasTrustedCAs := optionalCluster != nil && len(optionalCluster.Spec.TrustedCAs) > 0

	var sidecarArgs []string

	sidecarArgs = []string{
		"--copy-file", "fdb.cluster",
	}
	if hasTrustedCAs {
		sidecarArgs = append(sidecarArgs, "--copy-file", "ca.pem")
	}
	if optionalCluster != nil {
		sidecarArgs = append(sidecarArgs,
			"--input-monitor-conf", "fdbmonitor.conf",
			"--copy-binary", "fdbserver",
			"--copy-binary", "fdbcli",
			"--main-container-version", versionString,
		)
	}

	if optionalCluster == nil {
		sidecarArgs = append(sidecarArgs, "--require-not-empty")
		sidecarArgs = append(sidecarArgs, "fdb.cluster")
	}

	if optionalCluster != nil {
		cluster := optionalCluster

		if cluster.Spec.Routing.PodIPFamily != nil {
			sidecarArgs = append(sidecarArgs, "--public-ip-family")
			sidecarArgs = append(sidecarArgs, fmt.Sprint(*cluster.Spec.Routing.PodIPFamily))
		}

		if cluster.NeedsExplicitListenAddress() {
			sidecarArgs = append(sidecarArgs, "--substitute-variable", "FDB_POD_IP")
		}

		for _, substitution := range cluster.Spec.SidecarVariables {
			sidecarArgs = append(sidecarArgs, "--substitute-variable", substitution)
		}

		sidecarEnv = append(sidecarEnv, getEnvForMonitorConfigSubstitution(cluster, processGroupID)...)

		if cluster.DefineDNSLocalityFields() {
			sidecarArgs = append(sidecarArgs, "--substitute-variable", "FDB_DNS_NAME")
			sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "FDB_DNS_NAME", Value: GetPodDNSName(cluster, podName)})
		}

		if !initMode {
			if cluster.GetSidecarContainerEnableLivenessProbe() && container.LivenessProbe == nil {
				// We can't use a HTTP handler here since the server
				// requires a client certificate
				container.LivenessProbe = &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.IntOrString{IntVal: 8080},
						},
					},
					TimeoutSeconds:   1,
					PeriodSeconds:    30,
					FailureThreshold: 5,
				}
			}

			if cluster.GetSidecarContainerEnableReadinessProbe() && container.ReadinessProbe == nil {
				container.ReadinessProbe = &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.IntOrString{IntVal: 8080},
						},
					},
				}
			}
		}
	}

	if initMode {
		sidecarArgs = append(sidecarArgs, "--init-mode")
	}

	extendEnv(container, sidecarEnv...)

	var overrides fdbv1beta2.ContainerOverrides

	if optionalCluster != nil {
		overrides = optionalCluster.Spec.SidecarContainer
	}

	if len(imageConfigs) > 0 {
		overrides.ImageConfigs = imageConfigs
	} else {
		overrides.ImageConfigs = []fdbv1beta2.ImageConfig{{BaseImage: "foundationdb/foundationdb-kubernetes-sidecar", TagSuffix: "-1"}}
	}

	if overrides.EnableTLS && !initMode {
		sidecarArgs = append(sidecarArgs, "--tls")
	}

	if optionalCluster != nil {
		for _, crashObjs := range optionalCluster.Spec.Buggify.CrashLoopContainers {
			for _, pid := range crashObjs.Targets {
				if (pid == "*" || pid == processGroupID) && crashObjs.ContainerName == container.Name {
					sidecarArgs = []string{"crash-loop"}
				}
			}
		}
	}

	if len(sidecarArgs) > 0 {
		container.Args = sidecarArgs
	}

	container.VolumeMounts = append(container.VolumeMounts,
		corev1.VolumeMount{Name: "config-map", MountPath: "/var/input-files"},
		corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/output-files"},
	)

	image, err := GetImage(container.Image, overrides.ImageConfigs, versionString, allowTagOverride)
	if err != nil {
		return err
	}
	container.Image = image

	ensureSecurityContextIsPresent(container)

	if initMode {
		return nil
	}

	extendEnv(container, corev1.EnvVar{Name: "FDB_TLS_VERIFY_PEERS", Value: overrides.PeerVerificationRules})

	if hasTrustedCAs {
		extendEnv(container, corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/input-files/ca.pem"})
	}

	return nil
}

// getEnvForMonitorConfigSubstitution provides the environment variables that
// are used for substituting variables into the monitor config.
func getEnvForMonitorConfigSubstitution(cluster *fdbv1beta2.FoundationDBCluster, processGroupID fdbv1beta2.ProcessGroupID) []corev1.EnvVar {
	env := make([]corev1.EnvVar, 0)

	publicIPSource := cluster.Spec.Routing.PublicIPSource
	usePublicIPFromService := publicIPSource != nil && *publicIPSource == fdbv1beta2.PublicIPSourceService

	var publicIPKey string
	if usePublicIPFromService {
		publicIPKey = fmt.Sprintf("metadata.annotations['%s']", fdbv1beta2.PublicIPAnnotation)
	} else {
		family := cluster.Spec.Routing.PodIPFamily
		if family == nil {
			publicIPKey = "status.podIP"
		} else {
			publicIPKey = "status.podIPs"
		}
	}
	env = append(env, corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
		FieldRef: &corev1.ObjectFieldSelector{FieldPath: publicIPKey},
	}})

	if cluster.NeedsExplicitListenAddress() {
		podIPKey := ""
		family := cluster.Spec.Routing.PodIPFamily
		if family == nil {
			podIPKey = "status.podIP"
		} else {
			podIPKey = "status.podIPs"
		}
		env = append(env, corev1.EnvVar{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: podIPKey},
		}})
	}

	faultDomainKey := cluster.Spec.FaultDomain.Key
	if faultDomainKey == "" {
		faultDomainKey = corev1.LabelHostname
	}

	faultDomainSource := cluster.Spec.FaultDomain.ValueFrom
	if faultDomainSource == "" {
		faultDomainSource = "spec.nodeName"
	}

	if faultDomainKey == fdbv1beta2.NoneFaultDomainKey {
		env = append(env, corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
		}})
		env = append(env, corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
		}})
	} else if faultDomainKey == "foundationdb.org/kubernetes-cluster" {
		env = append(env, corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
		}})
		env = append(env, corev1.EnvVar{Name: "FDB_ZONE_ID", Value: cluster.Spec.FaultDomain.Value})
	} else {
		env = append(env, corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
		}})
		if !strings.HasPrefix(faultDomainSource, "$") {
			env = append(env, corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: faultDomainSource},
			}})
		}
	}

	env = append(env, corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: string(processGroupID)})

	return env
}

// usePvc determines whether we should attach a PVC to a pod.
func usePvc(cluster *fdbv1beta2.FoundationDBCluster, processClass fdbv1beta2.ProcessClass) bool {
	var storage *resource.Quantity
	processSettings := cluster.GetProcessSettings(processClass)

	if processSettings.VolumeClaimTemplate != nil {
		requests := processSettings.VolumeClaimTemplate.Spec.Resources.Requests
		if requests != nil {
			storageCopy := requests[corev1.ResourceStorage]
			storage = &storageCopy
		}
	}
	return processClass.IsStateful() && (storage == nil || !storage.IsZero())
}

// GetPvc builds a persistent volume claim for a FoundationDB process group.
func GetPvc(cluster *fdbv1beta2.FoundationDBCluster, processClass fdbv1beta2.ProcessClass, idNum int) (*corev1.PersistentVolumeClaim, error) {
	if !usePvc(cluster, processClass) {
		return nil, nil
	}
	name, id := GetProcessGroupID(cluster, processClass, idNum)

	processSettings := cluster.GetProcessSettings(processClass)
	var pvc *corev1.PersistentVolumeClaim
	if processSettings.VolumeClaimTemplate != nil {
		pvc = processSettings.VolumeClaimTemplate.DeepCopy()
	} else {
		pvc = &corev1.PersistentVolumeClaim{}
	}

	pvc.ObjectMeta = GetPvcMetadata(cluster, processClass, id)
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

	storage := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	if (&storage).IsZero() {
		pvc.Spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse("128G")
	}

	specHash, err := GetJSONHash(pvc.Spec)
	if err != nil {
		return nil, err
	}

	if pvc.ObjectMeta.Annotations == nil {
		pvc.ObjectMeta.Annotations = make(map[string]string, 1)
	}
	pvc.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey] = specHash

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

// GetBackupDeployment builds a deployment for backup agents for a cluster.
func GetBackupDeployment(backup *fdbv1beta2.FoundationDBBackup) (*appsv1.Deployment, error) {
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
	deployment.ObjectMeta.OwnerReferences = BuildOwnerReference(backup.TypeMeta, backup.ObjectMeta)

	if backup.Spec.BackupDeploymentMetadata != nil {
		for key, value := range backup.Spec.BackupDeploymentMetadata.Labels {
			deployment.ObjectMeta.Labels[key] = value
		}
		for key, value := range backup.Spec.BackupDeploymentMetadata.Annotations {
			deployment.ObjectMeta.Annotations[key] = value
		}
	}
	deployment.ObjectMeta.Labels[fdbv1beta2.BackupDeploymentLabel] = string(backup.ObjectMeta.UID)

	var podTemplate *corev1.PodTemplateSpec
	if backup.Spec.PodTemplateSpec != nil {
		podTemplate = backup.Spec.PodTemplateSpec.DeepCopy()
	} else {
		podTemplate = &corev1.PodTemplateSpec{}
	}

	var mainContainer *corev1.Container
	var initContainer *corev1.Container

	for index, container := range podTemplate.Spec.Containers {
		if container.Name == fdbv1beta2.MainContainerName {
			mainContainer = &podTemplate.Spec.Containers[index]
		}
	}

	for index, container := range podTemplate.Spec.InitContainers {
		if container.Name == fdbv1beta2.InitContainerName {
			initContainer = &podTemplate.Spec.InitContainers[index]
		}
	}

	if mainContainer == nil {
		containers := []corev1.Container{
			{
				Name: fdbv1beta2.MainContainerName,
			},
		}
		containers = append(containers, podTemplate.Spec.Containers...)
		mainContainer = &containers[0]
		podTemplate.Spec.Containers = containers
	}

	if len(backup.Spec.MainContainer.ImageConfigs) == 0 {
		backup.Spec.MainContainer.ImageConfigs = []fdbv1beta2.ImageConfig{
			{BaseImage: "foundationdb/foundationdb"},
		}
	}

	image, err := GetImage(mainContainer.Image, backup.Spec.MainContainer.ImageConfigs, backup.Spec.Version, pointer.BoolDeref(backup.Spec.AllowTagOverride, false))
	if err != nil {
		return nil, err
	}
	mainContainer.Image = image
	mainContainer.Command = []string{"backup_agent"}
	args := []string{"--log", "--logdir", "/var/log/fdb-trace-logs"}

	if len(backup.Spec.CustomParameters) > 0 {
		err := backup.Spec.CustomParameters.ValidateCustomParameters()
		if err != nil {
			return nil, err
		}

		for _, customParameter := range backup.Spec.CustomParameters {
			args = append(args, fmt.Sprintf("--%s", customParameter))
		}
	}

	mainContainer.Args = args
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
		podTemplate.Spec.InitContainers = append(podTemplate.Spec.InitContainers, corev1.Container{Name: fdbv1beta2.InitContainerName})
		initContainer = &podTemplate.Spec.InitContainers[0]
	}

	err = configureSidecarContainerForBackup(backup, initContainer)
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
					{Key: ClusterFileKey, Path: "fdb.cluster"},
				},
			}},
		},
	)

	deployment.Spec.Template = *podTemplate

	specHash, err := GetJSONHash(deployment.Spec)
	if err != nil {
		return nil, err
	}

	deployment.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey] = specHash

	return deployment, nil
}

// GetServersPerPodForPod returns the count of servers per Pod based on the processClass from the sidecar or 1
func GetServersPerPodForPod(pod *corev1.Pod, pClass fdbv1beta2.ProcessClass) (int, error) {
	// If not specified we will default to 1
	serversPerPod := 1
	if pod == nil {
		return serversPerPod, nil
	}

	for _, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			if env.Name == fmt.Sprintf("%s_SERVERS_PER_POD", strings.ToUpper(string(pClass))) {
				return strconv.Atoi(env.Value)
			}
		}
	}

	return serversPerPod, nil
}

// GetPodMetadata returns the metadata for a specific Pod
func GetPodMetadata(cluster *fdbv1beta2.FoundationDBCluster, processClass fdbv1beta2.ProcessClass, id fdbv1beta2.ProcessGroupID, specHash string) metav1.ObjectMeta {
	var customMetadata *metav1.ObjectMeta

	processSettings := cluster.GetProcessSettings(processClass)
	if processSettings.PodTemplate != nil {
		customMetadata = &processSettings.PodTemplate.ObjectMeta
	} else {
		customMetadata = nil
	}

	metadata := GetObjectMetadata(cluster, customMetadata, processClass, id)

	if metadata.Annotations == nil {
		metadata.Annotations = make(map[string]string)
	}
	metadata.Annotations[fdbv1beta2.LastSpecKey] = specHash
	metadata.Annotations[fdbv1beta2.PublicIPSourceAnnotation] = string(cluster.GetPublicIPSource())

	return metadata
}

// GetObjectMetadata returns the ObjectMetadata for a process
func GetObjectMetadata(cluster *fdbv1beta2.FoundationDBCluster, base *metav1.ObjectMeta, processClass fdbv1beta2.ProcessClass, id fdbv1beta2.ProcessGroupID) metav1.ObjectMeta {
	var metadata *metav1.ObjectMeta

	if base != nil {
		metadata = base.DeepCopy()
	} else {
		metadata = &metav1.ObjectMeta{}
	}
	metadata.Namespace = cluster.Namespace

	if metadata.Labels == nil {
		metadata.Labels = make(map[string]string)
	}

	for label, value := range GetPodLabels(cluster, processClass, string(id)) {
		metadata.Labels[label] = value
	}

	for label, value := range cluster.GetResourceLabels() {
		metadata.Labels[label] = value
	}

	return *metadata
}

// GetPodDNSName determines the fully qualified DNS name for a pod.
func GetPodDNSName(cluster *fdbv1beta2.FoundationDBCluster, podName string) string {
	return fmt.Sprintf("%s.%s.%s.svc.%s", podName, cluster.Name, cluster.Namespace, cluster.GetDNSDomain())
}

// ContainsPod checks if the given Pod is part of the cluster or not.
func ContainsPod(cluster *fdbv1beta2.FoundationDBCluster, pod corev1.Pod) bool {
	clusterMatchingLabels := cluster.GetMatchLabels()
	podLabels := pod.GetLabels()
	if len(clusterMatchingLabels) > len(podLabels) {
		return false
	}
	for clusterLabelKey, clusterLabelValue := range clusterMatchingLabels {
		if podLabelValue, found := podLabels[clusterLabelKey]; !found || clusterLabelValue != podLabelValue {
			return false
		}
	}
	return true
}
