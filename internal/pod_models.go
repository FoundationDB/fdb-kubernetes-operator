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

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var processClassSanitizationPattern = regexp.MustCompile("[^a-z0-9-]")

// GetProcessGroupID generates an ID for a process group.
//
// This will return the pod name and the processGroupID ID.
func GetProcessGroupID(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, idNum int) (string, string) {
	var processGroupID string
	if cluster.Spec.ProcessGroupIDPrefix != "" {
		processGroupID = fmt.Sprintf("%s-%s-%d", cluster.Spec.ProcessGroupIDPrefix, processClass, idNum)
	} else if cluster.Spec.InstanceIDPrefix != "" {
		processGroupID = fmt.Sprintf("%s-%s-%d", cluster.Spec.InstanceIDPrefix, processClass, idNum)
	} else {
		processGroupID = fmt.Sprintf("%s-%d", processClass, idNum)
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
			Port: int32(fdbtypes.GetProcessPort(i, true)),
		}, corev1.ServicePort{
			Name: nonTlSPortName,
			Port: int32(fdbtypes.GetProcessPort(i, false)),
		})
	}

	return ports
}

// GetService builds a service for a new process group
func GetService(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, idNum int) (*corev1.Service, error) {
	name, id := GetProcessGroupID(cluster, processClass, idNum)

	owner := BuildOwnerReference(cluster.TypeMeta, cluster.ObjectMeta)
	metadata := GetObjectMetadata(cluster, nil, processClass, id)
	metadata.Name = name
	metadata.OwnerReferences = owner

	processesPerPod := 1
	if processClass == fdbtypes.ProcessClassStorage {
		processesPerPod = cluster.GetStorageServersPerPod()
	}

	return &corev1.Service{
		ObjectMeta: metadata,
		Spec: corev1.ServiceSpec{
			Type:                     corev1.ServiceTypeClusterIP,
			Ports:                    generateServicePorts(processesPerPod),
			PublishNotReadyAddresses: true,
			Selector:                 GetPodMatchLabels(cluster, "", id),
		},
	}, nil
}

// GetPod builds a pod for a new process group
func GetPod(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, idNum int) (*corev1.Pod, error) {
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
func GetImage(image string, configs []fdbtypes.ImageConfig, versionString string, allowOverride bool) (string, error) {
	if image != "" {
		imageComponents := strings.Split(image, ":")
		if len(imageComponents) > 1 {
			if !allowOverride {
				// If the specified image contains a tag and allowOverride is false return an error
				return "", fmt.Errorf("image should not contain a tag but contains the tag \"%s\", please remove the tag", imageComponents[1])
			}
			return image, nil
		}
		configs = append([]fdbtypes.ImageConfig{{BaseImage: image}}, configs...)
	}

	return fdbtypes.SelectImageConfig(configs, versionString).Image(), nil
}

// GetPodSpec builds a pod spec for a FoundationDB pod
func GetPodSpec(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, idNum int) (*corev1.PodSpec, error) {
	processSettings := cluster.GetProcessSettings(processClass)
	podSpec := processSettings.PodTemplate.Spec.DeepCopy()
	useUnifiedImages := cluster.Spec.UseUnifiedImage != nil && *cluster.Spec.UseUnifiedImage

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

	if mainContainer == nil {
		return nil, fmt.Errorf("could not create main container")
	}

	if sidecarContainer == nil {
		return nil, fmt.Errorf("could not create sidecar container")
	}

	if useUnifiedImages {
		initContainer = &corev1.Container{}
	} else {
		for index, container := range podSpec.InitContainers {
			if container.Name == "foundationdb-kubernetes-init" {
				initContainer = &podSpec.InitContainers[index]
			}
		}

		if initContainer == nil {
			return nil, fmt.Errorf("could not create init container")
		}
	}

	podName, processGroupID := GetProcessGroupID(cluster, processClass, idNum)

	versionString := cluster.Status.RunningVersion
	if versionString == "" {
		versionString = cluster.Spec.Version
	}

	image, err := GetImage(mainContainer.Image, cluster.Spec.MainContainer.ImageConfigs, versionString, processSettings.GetAllowTagOverride())
	if err != nil {
		return nil, err
	}
	mainContainer.Image = image

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

	if useUnifiedImages {
		mainContainer.Args = []string{
			"--input-dir", "/var/dynamic-conf",
			"--log-path", "/var/log/fdb-trace-logs/monitor.log",
		}

		mainContainer.VolumeMounts = append(mainContainer.VolumeMounts,
			corev1.VolumeMount{Name: "data", MountPath: "/var/fdb/data"},
			corev1.VolumeMount{Name: "config-map", MountPath: "/var/dynamic-conf"},
			corev1.VolumeMount{Name: "shared-binaries", MountPath: "/var/fdb/shared-binaries"},
			corev1.VolumeMount{Name: "fdb-trace-logs", MountPath: "/var/log/fdb-trace-logs"},
		)

		mainContainer.Env = append(mainContainer.Env, getEnvForMonitorConfigSubstitution(cluster, instanceID)...)
		mainContainer.Env = append(mainContainer.Env, corev1.EnvVar{Name: "FDB_IMAGE_TYPE", Value: string(FDBImageTypeUnified)})
		mainContainer.Env = append(mainContainer.Env, corev1.EnvVar{Name: "FDB_POD_NAME", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
		}})
		mainContainer.Env = append(mainContainer.Env, corev1.EnvVar{Name: "FDB_POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
		}})
	} else {
		mainContainer.Command = []string{"sh", "-c"}

		args := "fdbmonitor --conffile /var/dynamic-conf/fdbmonitor.conf" +
			" --lockfile /var/dynamic-conf/fdbmonitor.lockfile" +
			" --loggroup " + logGroup +
			" >> /var/log/fdb-trace-logs/fdbmonitor-$(date '+%Y-%m-%d').log 2>&1"

		for _, crashLoopID := range cluster.Spec.Buggify.CrashLoop {
			if pID == crashLoopID || crashLoopID == "*" {
				args = "crash-loop"
			}
		}
		mainContainer.Args = []string{args}

		mainContainer.VolumeMounts = append(mainContainer.VolumeMounts,
			corev1.VolumeMount{Name: "data", MountPath: "/var/fdb/data"},
			corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/dynamic-conf"},
			corev1.VolumeMount{Name: "fdb-trace-logs", MountPath: "/var/log/fdb-trace-logs"},
		)
	}

	var readOnlyRootFilesystem = true
	if mainContainer.SecurityContext == nil {
		mainContainer.SecurityContext = &corev1.SecurityContext{}
	}

	if mainContainer.SecurityContext.ReadOnlyRootFilesystem == nil {
		mainContainer.SecurityContext.ReadOnlyRootFilesystem = &readOnlyRootFilesystem
	}

	if useUnifiedImages {
		sidecarVersionString := cluster.Status.RunningVersion
		if sidecarVersionString == "" {
			sidecarVersionString = cluster.Spec.Version
		}

		sidecarImage, err := GetImage(sidecarContainer.Image, cluster.Spec.MainContainer.ImageConfigs, sidecarVersionString, processSettings.GetAllowTagOverride())
		if err != nil {
			return nil, err
		}

		sidecarContainer.Image = sidecarImage
		sidecarContainer.Args = []string{
			"--mode", "sidecar",
			"--main-container-version", versionString,
			"--copy-binary", "fdbserver",
			"--copy-binary", "fdbcli",
			"--log-path", "/var/log/fdb-trace-logs/monitor.log",
		}

		sidecarContainer.VolumeMounts = append(sidecarContainer.VolumeMounts,
			corev1.VolumeMount{Name: "shared-binaries", MountPath: "/var/fdb/shared-binaries"},
			corev1.VolumeMount{Name: "fdb-trace-logs", MountPath: "/var/log/fdb-trace-logs"},
		)

		if sidecarContainer.SecurityContext == nil {
			sidecarContainer.SecurityContext = &corev1.SecurityContext{}
		}
		if sidecarContainer.SecurityContext.ReadOnlyRootFilesystem == nil {
			sidecarContainer.SecurityContext.ReadOnlyRootFilesystem = &readOnlyRootFilesystem
		}
	} else {
		err = configureSidecarContainerForCluster(cluster, initContainer, true, instanceID, processSettings.GetAllowTagOverride())
		if err != nil {
			return nil, err
		}

		err = configureSidecarContainerForCluster(cluster, sidecarContainer, false, instanceID, processSettings.GetAllowTagOverride())
		if err != nil {
			return nil, err
		}

		if processClass == fdbtypes.ProcessClassStorage && cluster.GetStorageServersPerPod() > 1 {
			sidecarContainer.Env = append(sidecarContainer.Env, corev1.EnvVar{Name: "STORAGE_SERVERS_PER_POD", Value: fmt.Sprintf("%d", cluster.GetStorageServersPerPod())})
		}
	}

	var mainVolumeSource corev1.VolumeSource
	if usePvc(cluster, processClass) {
		var volumeClaimSourceName string
		if processSettings.VolumeClaimTemplate != nil && processSettings.VolumeClaimTemplate.Name != "" {
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

	monitorConfKey := GetConfigMapMonitorConfEntry(processClass, GetDesiredImageType(cluster), cluster.GetStorageServersPerPod())

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

	faultDomainKey := cluster.Spec.FaultDomain.Key
	if faultDomainKey == "" {
		faultDomainKey = "kubernetes.io/hostname"
	}

	if faultDomainKey != "foundationdb.org/none" && faultDomainKey != "foundationdb.org/kubernetes-cluster" {
		if podSpec.Affinity == nil {
			podSpec.Affinity = &corev1.Affinity{}
		}

		if podSpec.Affinity.PodAntiAffinity == nil {
			podSpec.Affinity.PodAntiAffinity = &corev1.PodAntiAffinity{}
		}

		labelSelectors := make(map[string]string, len(cluster.Spec.LabelConfig.MatchLabels)+1)
		for key, value := range cluster.Spec.LabelConfig.MatchLabels {
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

	for _, noSchedulePID := range cluster.Spec.Buggify.NoSchedule {
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
					Key: fdbtypes.NodeSelectorNoScheduleLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"true"},
				}},
			},
		}
	}

	if !useUnifiedImages {
		replaceContainers(podSpec.InitContainers, initContainer)
	}
	replaceContainers(podSpec.Containers, mainContainer, sidecarContainer)

	podSpec.Volumes = append(podSpec.Volumes, volumes...)

	headlessService := GetHeadlessService(cluster)

	if headlessService != nil {
		podSpec.Hostname = podName
		podSpec.Subdomain = headlessService.Name
	}

	return podSpec, nil
}

// configureSidecarContainerForCluster sets up a sidecar container for a sidecar
// in the FDB cluster.
func configureSidecarContainerForCluster(cluster *fdbtypes.FoundationDBCluster, container *corev1.Container, initMode bool, processGroupID string, allowOverride bool) error {
	versionString := cluster.Status.RunningVersion
	if versionString == "" {
		versionString = cluster.Spec.Version
	}

	return configureSidecarContainer(container, initMode, processGroupID, versionString, cluster, allowOverride)
}

// configureSidecarContainerForBackup sets up a sidecar container for the init
// container for a backup process.
func configureSidecarContainerForBackup(backup *fdbtypes.FoundationDBBackup, container *corev1.Container) error {
	return configureSidecarContainer(container, true, "", backup.Spec.Version, nil, backup.Spec.GetAllowTagOverride())
}

// configureSidecarContainer sets up a foundationdb-kubernetes-sidecar
// container.
func configureSidecarContainer(container *corev1.Container, initMode bool, processGroupID string, versionString string, optionalCluster *fdbtypes.FoundationDBCluster, allowOverride bool) error {
	version, err := fdbtypes.ParseFdbVersion(versionString)
	if err != nil {
		return err
	}

	sidecarEnv := make([]corev1.EnvVar, 0, 4)

	hasTrustedCAs := optionalCluster != nil && len(optionalCluster.Spec.TrustedCAs) > 0

	var sidecarArgs []string
	if version.PrefersCommandLineArgumentsInSidecar() {
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

	if version.HasSidecarCrashOnEmpty() && optionalCluster == nil {
		sidecarArgs = append(sidecarArgs, "--require-not-empty")
		sidecarArgs = append(sidecarArgs, "fdb.cluster")
	}

	if !version.PrefersCommandLineArgumentsInSidecar() {
		if initMode {
			sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "COPY_ONCE", Value: "1"})
		}
		sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "SIDECAR_CONF_DIR", Value: "/var/input-files"})
	}

	if optionalCluster != nil {
		cluster := optionalCluster

		if cluster.Spec.Routing.PodIPFamily != nil {
			sidecarArgs = append(sidecarArgs, "--public-ip-family")
			sidecarArgs = append(sidecarArgs, fmt.Sprint(*cluster.Spec.Routing.PodIPFamily))
		}

		if cluster.NeedsExplicitListenAddress() {
			if version.PrefersCommandLineArgumentsInSidecar() {
				sidecarArgs = append(sidecarArgs, "--substitute-variable", "FDB_POD_IP")
			}
		}

		if version.PrefersCommandLineArgumentsInSidecar() {
			for _, substitution := range cluster.Spec.SidecarVariables {
				sidecarArgs = append(sidecarArgs, "--substitute-variable", substitution)
			}
			if !version.HasInstanceIDInSidecarSubstitutions() {
				sidecarArgs = append(sidecarArgs, "--substitute-variable", "FDB_INSTANCE_ID")
			}
		}

		sidecarEnv = append(sidecarEnv, getEnvForMonitorConfigSubstitution(cluster, processGroupID)...)

		if !initMode && *cluster.Spec.SidecarContainer.EnableLivenessProbe && container.LivenessProbe == nil {
			// We can't use a HTTP handler here since the server
			// requires a client certificate
			container.LivenessProbe = &corev1.Probe{
				Handler: corev1.Handler{
					TCPSocket: &corev1.TCPSocketAction{
						Port: intstr.IntOrString{IntVal: 8080},
					},
				},
				TimeoutSeconds:   1,
				PeriodSeconds:    30,
				FailureThreshold: 5,
			}
		}

		if !initMode && *cluster.Spec.SidecarContainer.EnableReadinessProbe && container.ReadinessProbe == nil {
			container.ReadinessProbe = &corev1.Probe{
				Handler: corev1.Handler{
					TCPSocket: &corev1.TCPSocketAction{
						Port: intstr.IntOrString{IntVal: 8080},
					},
				},
			}
		}
	}

	if version.PrefersCommandLineArgumentsInSidecar() && initMode {
		sidecarArgs = append(sidecarArgs, "--init-mode")
	}

	extendEnv(container, sidecarEnv...)

	var overrides fdbtypes.ContainerOverrides

	if optionalCluster != nil {
		overrides = optionalCluster.Spec.SidecarContainer
	} else {
		overrides.ImageConfigs = []fdbtypes.ImageConfig{{BaseImage: "foundationdb/foundationdb-kubernetes-sidecar", TagSuffix: "-1"}}
	}

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

	image, err := GetImage(container.Image, overrides.ImageConfigs, versionString, allowOverride)
	if err != nil {
		return err
	}
	container.Image = image

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

	return nil
}

// getEnvForMonitorConfigSubstitution provides the environment variables that
// are used for substituting variables into the monitor config.
func getEnvForMonitorConfigSubstitution(cluster *fdbtypes.FoundationDBCluster, instanceID string) []corev1.EnvVar {
	env := make([]corev1.EnvVar, 0)

	publicIPSource := cluster.Spec.Routing.PublicIPSource
	usePublicIPFromService := publicIPSource != nil && *publicIPSource == fdbtypes.PublicIPSourceService

	var publicIPKey string
	if usePublicIPFromService {
		publicIPKey = fmt.Sprintf("metadata.annotations['%s']", fdbtypes.PublicIPAnnotation)
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
		faultDomainKey = "kubernetes.io/hostname"
	}

	faultDomainSource := cluster.Spec.FaultDomain.ValueFrom
	if faultDomainSource == "" {
		faultDomainSource = "spec.nodeName"
	}

	if faultDomainKey == "foundationdb.org/none" {
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

	env = append(env, corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: instanceID})

	return env
}

// usePvc determines whether we should attach a PVC to a pod.
func usePvc(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass) bool {
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
func GetPvc(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, idNum int) (*corev1.PersistentVolumeClaim, error) {
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
	pvc.ObjectMeta.Annotations[fdbtypes.LastSpecKey] = specHash

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
func GetBackupDeployment(backup *fdbtypes.FoundationDBBackup) (*appsv1.Deployment, error) {
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
	deployment.ObjectMeta.Labels[fdbtypes.BackupDeploymentLabel] = string(backup.ObjectMeta.UID)

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
		containers := []corev1.Container{
			{Name: "foundationdb"},
		}
		containers = append(containers, podTemplate.Spec.Containers...)
		mainContainer = &containers[0]
		podTemplate.Spec.Containers = containers
	}

	image, err := GetImage(mainContainer.Image, []fdbtypes.ImageConfig{{BaseImage: "foundationdb/foundationdb"}}, backup.Spec.Version, backup.Spec.GetAllowTagOverride())
	if err != nil {
		return nil, err
	}
	mainContainer.Image = image
	mainContainer.Command = []string{"backup_agent"}
	args := []string{"--log", "--logdir", "/var/log/fdb-trace-logs"}

	if len(backup.Spec.CustomParameters) > 0 {
		err := ValidateCustomParameters(backup.Spec.CustomParameters)
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
		podTemplate.Spec.InitContainers = append(podTemplate.Spec.InitContainers, corev1.Container{Name: "foundationdb-kubernetes-init"})
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

	deployment.ObjectMeta.Annotations[fdbtypes.LastSpecKey] = specHash

	return deployment, nil
}

// GetStorageServersPerPodForPod returns the value of STORAGE_SERVERS_PER_POD from the sidecar or 1
func GetStorageServersPerPodForPod(pod *corev1.Pod) (int, error) {
	// If not specified we will default to 1
	storageServersPerPod := 1
	if pod == nil {
		return storageServersPerPod, nil
	}

	for _, container := range pod.Spec.Containers {
		for _, env := range container.Env {
			if env.Name == "STORAGE_SERVERS_PER_POD" {
				return strconv.Atoi(env.Value)
			}
		}
	}

	return storageServersPerPod, nil
}

// GetPodMetadata returns the metadata for a specific Pod
func GetPodMetadata(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, id string, specHash string) metav1.ObjectMeta {
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
	metadata.Annotations[fdbtypes.LastSpecKey] = specHash
	metadata.Annotations[fdbtypes.PublicIPSourceAnnotation] = string(*cluster.Spec.Routing.PublicIPSource)

	return metadata
}

// GetObjectMetadata returns the ObjectMetadata for a process
func GetObjectMetadata(cluster *fdbtypes.FoundationDBCluster, base *metav1.ObjectMeta, processClass fdbtypes.ProcessClass, id string) metav1.ObjectMeta {
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
	for label, value := range GetPodLabels(cluster, processClass, id) {
		metadata.Labels[label] = value
	}
	for label, value := range cluster.Spec.LabelConfig.ResourceLabels {
		metadata.Labels[label] = value
	}

	return *metadata
}
