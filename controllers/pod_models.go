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
	"fmt"
	"reflect"
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

// getInstanceID generates an ID for an instance.
//
// This will return the pod name and the instance ID.
func getInstanceID(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, idNum int) (string, string) {
	var instanceID string
	if cluster.Spec.InstanceIDPrefix != "" {
		instanceID = fmt.Sprintf("%s-%s-%d", cluster.Spec.InstanceIDPrefix, processClass, idNum)
	} else {
		instanceID = fmt.Sprintf("%s-%d", processClass, idNum)
	}
	return fmt.Sprintf("%s-%s-%d", cluster.Name, processClassSanitizationPattern.ReplaceAllString(string(processClass), "-"), idNum), instanceID
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

// GetService builds a service for a new instance
func GetService(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, idNum int) (*corev1.Service, error) {
	name, id := getInstanceID(cluster, processClass, idNum)

	owner := buildOwnerReference(cluster.TypeMeta, cluster.ObjectMeta)

	metadata := getObjectMetadata(cluster, nil, processClass, id)
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
			Selector:                 getMinimalSinglePodLabels(cluster, id),
		},
	}, nil
}

// GetPod builds a pod for a new instance
func GetPod(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, idNum int) (*corev1.Pod, error) {
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

func getImage(imageName, curImage, defaultImage, versionString string) (string, error) {
	var resImage string
	if imageName != "" {
		resImage = imageName
	}

	if curImage != "" {
		resImage = curImage
	}

	if resImage == "" {
		resImage = defaultImage
	}

	// If the specified image contains a tag return an error
	res := strings.Split(resImage, ":")
	if len(res) > 1 {
		return "", fmt.Errorf("image should not contain a tag but contains the tag \"%s\", please remove the tag", res[1])
	}

	return fmt.Sprintf("%s:%s", resImage, versionString), nil
}

// GetPodSpec builds a pod spec for a FoundationDB pod
func GetPodSpec(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, idNum int) (*corev1.PodSpec, error) {
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

	if mainContainer == nil {
		return nil, fmt.Errorf("could not create main container")
	}

	if sidecarContainer == nil {
		return nil, fmt.Errorf("could not create sidecar container")
	}

	if initContainer == nil {
		return nil, fmt.Errorf("could not create init container")
	}

	podName, instanceID := getInstanceID(cluster, processClass, idNum)

	versionString := cluster.Status.RunningVersion
	if versionString == "" {
		versionString = cluster.Spec.Version
	}

	image, err := getImage(cluster.Spec.MainContainer.ImageName, mainContainer.Image, "foundationdb/foundationdb", versionString)
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

	mainContainer.Command = []string{"sh", "-c"}
	mainContainer.Args = []string{
		"fdbmonitor --conffile /var/dynamic-conf/fdbmonitor.conf" +
			" --lockfile /var/dynamic-conf/fdbmonitor.lockfile" +
			" --loggroup " + logGroup +
			" >> /var/log/fdb-trace-logs/fdbmonitor-$(date '+%Y-%m-%d').log 2>&1",
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

	if processClass == fdbtypes.ProcessClassStorage && cluster.GetStorageServersPerPod() > 1 {
		sidecarContainer.Env = append(sidecarContainer.Env, corev1.EnvVar{Name: "STORAGE_SERVERS_PER_POD", Value: fmt.Sprintf("%d", cluster.GetStorageServersPerPod())})
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

	monitorConf := fmt.Sprintf("fdbmonitor-conf-%s", processClass)
	if processClass == fdbtypes.ProcessClassStorage && cluster.GetStorageServersPerPod() > 1 {
		monitorConf = fmt.Sprintf("fdbmonitor-conf-%s-density-%d", processClass, cluster.GetStorageServersPerPod())
	}

	configMapItems := []corev1.KeyToPath{
		{Key: monitorConf, Path: "fdbmonitor.conf"},
		{Key: "cluster-file", Path: "fdb.cluster"},
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
		{Name: "dynamic-conf", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		{Name: "config-map", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: configMapRefName},
			Items:                configMapItems,
		}}},
		{Name: "fdb-trace-logs", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
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
					{
						Weight: 1,
						PodAffinityTerm: corev1.PodAffinityTerm{
							TopologyKey: faultDomainKey,
							LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
								FDBClusterLabel:      cluster.ObjectMeta.Name,
								FDBProcessClassLabel: string(processClass),
							}},
						},
					},
				},
			},
		}
	}

	for _, noScheduleInstanceID := range cluster.Spec.Buggify.NoSchedule {
		if instanceID == noScheduleInstanceID {
			if affinity == nil {
				affinity = &corev1.Affinity{}
			}
			if affinity.NodeAffinity == nil {
				affinity.NodeAffinity = &corev1.NodeAffinity{}
			}
			if affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
				affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = &corev1.NodeSelector{}
			}
			affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = append(affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms, corev1.NodeSelectorTerm{
				MatchExpressions: []corev1.NodeSelectorRequirement{{
					Key: NodeSelectorNoScheduleLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"true"},
				}},
			})
		}
	}

	replaceContainers(podSpec.InitContainers, initContainer)
	replaceContainers(podSpec.Containers, mainContainer, sidecarContainer)
	podSpec.Volumes = append(podSpec.Volumes, volumes...)
	podSpec.Affinity = affinity

	headlessService := GetHeadlessService(cluster)

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

	return configureSidecarContainer(container, initMode, instanceID, versionString, cluster)
}

// configureSidecarContainerForBackup sets up a sidecar container for the init
// container for a backup process.
func configureSidecarContainerForBackup(backup *fdbtypes.FoundationDBBackup, container *corev1.Container) error {
	return configureSidecarContainer(container, true, "", backup.Spec.Version, nil)
}

// configureSidecarContainer sets up a foundationdb-kubernetes-sidecar
// container.
func configureSidecarContainer(container *corev1.Container, initMode bool, instanceID string, versionString string, optionalCluster *fdbtypes.FoundationDBCluster) error {
	version, err := fdbtypes.ParseFdbVersion(versionString)
	if err != nil {
		return err
	}

	var sidecarVersion string

	if optionalCluster != nil && optionalCluster.Spec.SidecarVersions[versionString] != 0 {
		sidecarVersion = fmt.Sprintf("%s-%d", versionString, optionalCluster.Spec.SidecarVersions[versionString])
	} else {
		sidecarVersion = fmt.Sprintf("%s-1", versionString)
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

		publicIPSource := cluster.Spec.Services.PublicIPSource
		usePublicIPFromService := publicIPSource != nil && *publicIPSource == fdbtypes.PublicIPSourceService

		var publicIPKey string
		if usePublicIPFromService {
			publicIPKey = fmt.Sprintf("metadata.annotations['%s']", PublicIPAnnotation)
		} else {
			publicIPKey = "status.podIP"
		}
		sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: publicIPKey},
		}})

		if cluster.NeedsExplicitListenAddress() {
			sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
			}})

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

		faultDomainKey := cluster.Spec.FaultDomain.Key
		if faultDomainKey == "" {
			faultDomainKey = "kubernetes.io/hostname"
		}

		faultDomainSource := cluster.Spec.FaultDomain.ValueFrom
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
			sidecarEnv = append(sidecarEnv, corev1.EnvVar{Name: "FDB_ZONE_ID", Value: cluster.Spec.FaultDomain.Value})
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

	if version.PrefersCommandLineArgumentsInSidecar() && initMode {
		sidecarArgs = append(sidecarArgs, "--init-mode")
	}

	extendEnv(container, sidecarEnv...)

	var overrides fdbtypes.ContainerOverrides

	if optionalCluster != nil {
		overrides = optionalCluster.Spec.SidecarContainer
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

	image, err := getImage(overrides.ImageName, container.Image, "foundationdb/foundationdb-kubernetes-sidecar", sidecarVersion)
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
	return isStateful(processClass) && (storage == nil || !storage.IsZero())
}

// isStateful determines whether a process class should store data.
func isStateful(processClass fdbtypes.ProcessClass) bool {
	return processClass == fdbtypes.ProcessClassStorage || processClass == fdbtypes.ProcessClassLog || processClass == fdbtypes.ProcessClassTransaction
}

// GetPvc builds a persistent volume claim for a FoundationDB instance.
func GetPvc(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, idNum int) (*corev1.PersistentVolumeClaim, error) {
	if !usePvc(cluster, processClass) {
		return nil, nil
	}
	name, id := getInstanceID(cluster, processClass, idNum)

	processSettings := cluster.GetProcessSettings(processClass)
	var pvc *corev1.PersistentVolumeClaim
	if processSettings.VolumeClaimTemplate != nil {
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
		containers := []corev1.Container{
			{Name: "foundationdb"},
		}
		containers = append(containers, podTemplate.Spec.Containers...)
		mainContainer = &containers[0]
		podTemplate.Spec.Containers = containers
	}

	image, err := getImage(mainContainer.Image, mainContainer.Image, "foundationdb/foundationdb", backup.Spec.Version)
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

// ValidateCustomParameters ensures that no duplicate values are set and that no
// protected/forbidden parameters are set. Theoretically we could also check if FDB
// supports the given parameter.
func ValidateCustomParameters(customParameters []string) error {
	protectedParameters := map[string]bool{"datadir": true}
	parameters := make(map[string]bool)
	violations := make([]string, 0)

	for _, parameter := range customParameters {
		parameterName := strings.Split(parameter, "=")[0]
		parameterName = strings.TrimSpace(parameterName)

		if _, ok := parameters[parameterName]; !ok {
			parameters[parameterName] = true
		} else {
			violations = append(violations, fmt.Sprintf("found duplicated customParameter: %v", parameterName))
		}

		if _, ok := protectedParameters[parameterName]; ok {
			violations = append(violations, fmt.Sprintf("found protected customParameter: %v, please remove this parameter from the customParameters list", parameterName))
		}
	}

	if len(violations) > 0 {
		return fmt.Errorf("found the following customParameters violations:\n%s", strings.Join(violations, "\n"))
	}

	return nil
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

// ensurePodTemplatePresent defines a pod template in the general process
// settings.
func ensurePodTemplatePresent(spec *fdbtypes.FoundationDBClusterSpec) {
	if spec.Processes == nil {
		spec.Processes = make(map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings)
	}
	generalSettings := spec.Processes[fdbtypes.ProcessClassGeneral]
	if generalSettings.PodTemplate == nil {
		generalSettings.PodTemplate = &corev1.PodTemplateSpec{}
	}
	spec.Processes[fdbtypes.ProcessClassGeneral] = generalSettings
}

// ensureCustomParametersPresent defines custom parameters for the general process
// settings.
func ensureCustomParametersPresent(spec *fdbtypes.FoundationDBClusterSpec) {
	if spec.Processes == nil {
		spec.Processes = make(map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings)
	}
	generalSettings := spec.Processes[fdbtypes.ProcessClassGeneral]
	if generalSettings.CustomParameters == nil {
		params := make([]string, 0)
		generalSettings.CustomParameters = &params
	}
	spec.Processes[fdbtypes.ProcessClassGeneral] = generalSettings
}

// ensureVolumeClaimPresent defines a volume claim in the general process
// settings.
func ensureVolumeClaimPresent(spec *fdbtypes.FoundationDBClusterSpec) {
	if spec.Processes == nil {
		spec.Processes = make(map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings)
	}
	generalSettings := spec.Processes[fdbtypes.ProcessClassGeneral]
	if generalSettings.VolumeClaimTemplate == nil {
		generalSettings.VolumeClaimTemplate = &corev1.PersistentVolumeClaim{}
	}
	spec.Processes[fdbtypes.ProcessClassGeneral] = generalSettings
}

// ensureConfigMapPresent defines a config map in the cluster spec.
func ensureConfigMapPresent(spec *fdbtypes.FoundationDBClusterSpec) {
	if spec.ConfigMap == nil {
		spec.ConfigMap = &corev1.ConfigMap{}
	}
}

// mergeLabels merges labels from another part of the cluster spec into the
// object metadata.
func mergeLabels(metadata *metav1.ObjectMeta, labels map[string]string) {
	if metadata.Labels == nil {
		metadata.Labels = make(map[string]string, len(labels))
	}
	for key, value := range labels {
		_, present := metadata.Labels[key]
		if !present {
			metadata.Labels[key] = value
		}
	}
}

// updateProcessSettings runs a customization function on all of the process
// settings in the cluster spec.
func updateProcessSettings(spec *fdbtypes.FoundationDBClusterSpec, customizer func(*fdbtypes.ProcessSettings)) {
	for processClass, settings := range spec.Processes {
		customizer(&settings)
		spec.Processes[processClass] = settings
	}
}

// updatePodTemplates updates all of the pod templates in the cluster spec.
// This will also ensure that the general settings contain a pod template, so
// the customization function is guaranteed to be called at least once.
func updatePodTemplates(spec *fdbtypes.FoundationDBClusterSpec, customizer func(*corev1.PodTemplateSpec)) {
	ensurePodTemplatePresent(spec)
	updateProcessSettings(spec, func(settings *fdbtypes.ProcessSettings) {
		if settings.PodTemplate == nil {
			return
		}
		customizer(settings.PodTemplate)
	})
}

// updateVolumeClaims updates all of the volume claims in the cluster spec.
// This will also ensure that the general settings contain a volume claim, so
// the customization function is guaranteed to be called at least once.
func updateVolumeClaims(spec *fdbtypes.FoundationDBClusterSpec, customizer func(*corev1.PersistentVolumeClaim)) {
	ensureVolumeClaimPresent(spec)
	updateProcessSettings(spec, func(settings *fdbtypes.ProcessSettings) {
		if settings.VolumeClaimTemplate == nil {
			return
		}
		customizer(settings.VolumeClaimTemplate)
	})
}

// NormalizeClusterSpec converts a cluster spec into an unambiguous,
// future-proof form, by applying any implicit defaults and moving configuration
// from deprecated fields into fully-supported fields.
func NormalizeClusterSpec(spec *fdbtypes.FoundationDBClusterSpec, options DeprecationOptions) error {
	if spec.PodTemplate != nil {
		ensurePodTemplatePresent(spec)
		generalSettings := spec.Processes[fdbtypes.ProcessClassGeneral]
		if reflect.DeepEqual(generalSettings.PodTemplate, &corev1.PodTemplateSpec{}) {
			generalSettings.PodTemplate = spec.PodTemplate
		}
		spec.Processes[fdbtypes.ProcessClassGeneral] = generalSettings
		spec.PodTemplate = nil
	}

	if spec.SidecarVersion != 0 {
		if spec.SidecarVersions == nil {
			spec.SidecarVersions = make(map[string]int)
		}
		spec.SidecarVersions[spec.Version] = spec.SidecarVersion
		spec.SidecarVersion = 0
	}

	if spec.VolumeClaim != nil {
		if spec.Processes == nil {
			spec.Processes = make(map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings)
		}
		generalSettings := spec.Processes[fdbtypes.ProcessClassGeneral]
		if generalSettings.VolumeClaimTemplate == nil {
			generalSettings.VolumeClaimTemplate = spec.VolumeClaim.DeepCopy()
		}
		spec.Processes[fdbtypes.ProcessClassGeneral] = generalSettings
		spec.VolumeClaim = nil
	}

	if spec.Processes != nil {
		for key, settings := range spec.Processes {
			if settings.VolumeClaim != nil && settings.VolumeClaimTemplate == nil {
				settings.VolumeClaimTemplate = settings.VolumeClaim
				settings.VolumeClaim = nil
				spec.Processes[key] = settings
			}
		}
	}

	if spec.NextInstanceID != 0 {
		spec.NextInstanceID = 0
	}

	if spec.PodLabels != nil {
		updatePodTemplates(spec, func(template *corev1.PodTemplateSpec) {
			mergeLabels(&template.ObjectMeta, spec.PodLabels)
		})

		updateVolumeClaims(spec, func(claim *corev1.PersistentVolumeClaim) {
			mergeLabels(&claim.ObjectMeta, spec.PodLabels)
		})

		ensureConfigMapPresent(spec)
		mergeLabels(&spec.ConfigMap.ObjectMeta, spec.PodLabels)

		spec.PodLabels = nil
	}

	if spec.Resources != nil {
		updatePodTemplates(spec, func(template *corev1.PodTemplateSpec) {
			template.Spec.Containers, _ = ensureContainerPresent(template.Spec.Containers, "foundationdb", 0)

			template.Spec.Containers = customizeContainerFromList(template.Spec.Containers, "foundationdb", func(container *corev1.Container) {
				container.Resources = *spec.Resources
			})
		})

		spec.Resources = nil
	}

	if spec.InitContainers != nil {
		updatePodTemplates(spec, func(template *corev1.PodTemplateSpec) {
			template.Spec.InitContainers = append(template.Spec.InitContainers, spec.InitContainers...)
		})

		spec.InitContainers = nil
	}

	if spec.Containers != nil {
		updatePodTemplates(spec, func(template *corev1.PodTemplateSpec) {
			template.Spec.Containers = append(template.Spec.Containers, spec.Containers...)
		})

		spec.Containers = nil
	}

	if spec.Volumes != nil {
		updatePodTemplates(spec, func(template *corev1.PodTemplateSpec) {
			if template.Spec.Volumes == nil {
				template.Spec.Volumes = make([]corev1.Volume, 0, len(spec.Volumes))
			}
			template.Spec.Volumes = append(template.Spec.Volumes, spec.Volumes...)
		})
		spec.Volumes = nil
	}

	if spec.PodSecurityContext != nil {
		updatePodTemplates(spec, func(template *corev1.PodTemplateSpec) {
			template.Spec.SecurityContext = spec.PodSecurityContext
		})
		spec.PodSecurityContext = nil
	}

	if spec.AutomountServiceAccountToken != nil {
		updatePodTemplates(spec, func(template *corev1.PodTemplateSpec) {
			template.Spec.AutomountServiceAccountToken = spec.AutomountServiceAccountToken
		})
		spec.AutomountServiceAccountToken = nil
	}

	if spec.StorageClass != nil {
		updateVolumeClaims(spec, func(volumeClaim *corev1.PersistentVolumeClaim) {
			volumeClaim.Spec.StorageClassName = spec.StorageClass
		})
		spec.StorageClass = nil
	}

	if spec.VolumeSize != "" {
		storageQuantity, err := resource.ParseQuantity(spec.VolumeSize)
		if err != nil {
			return err
		}
		updateVolumeClaims(spec, func(volumeClaim *corev1.PersistentVolumeClaim) {
			if volumeClaim.Spec.Resources.Requests == nil {
				volumeClaim.Spec.Resources.Requests = corev1.ResourceList{}
			}
			volumeClaim.Spec.Resources.Requests[corev1.ResourceStorage] = storageQuantity
		})
		spec.VolumeSize = ""
	}

	if spec.RunningVersion != "" {
		spec.RunningVersion = ""
	}

	if spec.ConnectionString != "" {
		spec.ConnectionString = ""
	}

	if len(spec.CustomParameters) > 0 {
		params := spec.CustomParameters
		ensureCustomParametersPresent(spec)
		for processClass, settings := range spec.Processes {
			settings.CustomParameters = &params
			spec.Processes[processClass] = settings
		}
		spec.CustomParameters = nil
	}

	// Validate customParameters
	for processClass := range spec.Processes {
		if setting, ok := spec.Processes[processClass]; ok {
			if setting.CustomParameters == nil {
				continue
			}

			err := ValidateCustomParameters(*setting.CustomParameters)
			if err != nil {
				return err
			}
		}
	}

	if !options.OnlyShowChanges {
		// Set up resource requirements for the main container.
		updatePodTemplates(spec, func(template *corev1.PodTemplateSpec) {
			template.Spec.Containers, _ = ensureContainerPresent(template.Spec.Containers, "foundationdb", 0)

			template.Spec.Containers = customizeContainerFromList(template.Spec.Containers, "foundationdb", func(container *corev1.Container) {
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
		})

		if spec.Services.PublicIPSource == nil {
			source := fdbtypes.PublicIPSourcePod
			spec.Services.PublicIPSource = &source
		}

		if spec.AutomationOptions.Replacements.Enabled == nil {
			enabled := false
			spec.AutomationOptions.Replacements.Enabled = &enabled
		}

		if spec.AutomationOptions.Replacements.FailureDetectionTimeSeconds == nil {
			duration := 1800
			spec.AutomationOptions.Replacements.FailureDetectionTimeSeconds = &duration
		}
	}

	// Apply changes between old and new defaults.
	// When we update the defaults in the next release, the following sections
	// should be moved under the `!OnlyShowChanges` section, and we should use
	// the latest defaults as the active defaults.

	// Set up sidecar resource requirements

	zeroQuantity, err := resource.ParseQuantity("0")
	if err != nil {
		return err
	}

	updatePodTemplates(spec, func(template *corev1.PodTemplateSpec) {
		sidecarUpdater := func(container *corev1.Container) {
			if options.UseFutureDefaults {
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
				if options.OnlyShowChanges {
					if container.Resources.Requests == nil {
						container.Resources.Requests = corev1.ResourceList{
							"org.foundationdb/empty": zeroQuantity,
						}
					}
					if container.Resources.Limits == nil {
						container.Resources.Limits = corev1.ResourceList{
							"org.foundationdb/empty": zeroQuantity,
						}
					}
				}
			}
		}

		template.Spec.InitContainers = customizeContainerFromList(template.Spec.InitContainers, "foundationdb-kubernetes-init", sidecarUpdater)
		template.Spec.Containers = customizeContainerFromList(template.Spec.Containers, "foundationdb-kubernetes-sidecar", sidecarUpdater)
	})
	return nil
}

func getStorageServersPerPodForInstance(instance *FdbInstance) (int, error) {
	return getStorageServersPerPodForPod(instance.Pod)
}

func getStorageServersPerPodForPod(pod *corev1.Pod) (int, error) {
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
