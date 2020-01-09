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

package foundationdbcluster

import (
	ctx "context"
	"fmt"
	"regexp"
	"strings"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var processClassSanitizationPattern = regexp.MustCompile("[^a-z0-9-]")

// getInstanceId generates an ID for an instance.
func getInstanceId(cluster *fdbtypes.FoundationDBCluster, processClass string, idNum int) (string, string) {
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
	name, id := getInstanceId(cluster, processClass, idNum)

	owner, err := buildOwnerReference(context, cluster, kubeClient)
	if err != nil {
		return nil, err
	}
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
	var podSpec *corev1.PodSpec

	if cluster.Spec.PodTemplate != nil {
		podSpec = cluster.Spec.PodTemplate.Spec.DeepCopy()
	} else {
		podSpec = &corev1.PodSpec{}
	}

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
		containerCount := 1 + len(podSpec.Containers) + len(cluster.Spec.Containers)
		if sidecarContainer == nil {
			containerCount += 1
		}
		containers := make([]corev1.Container, 0, containerCount)
		containers = append(containers, corev1.Container{
			Name: "foundationdb",
		})
		containers = append(containers, podSpec.Containers...)
		podSpec.Containers = containers
		mainContainer = &podSpec.Containers[0]
	}

	if sidecarContainer == nil {
		podSpec.Containers = append(podSpec.Containers, corev1.Container{
			Name: "foundationdb-kubernetes-sidecar",
		})
		sidecarContainer = &podSpec.Containers[len(podSpec.Containers)-1]
	}

	if initContainer == nil {
		podSpec.InitContainers = append(podSpec.InitContainers, corev1.Container{
			Name: "foundationdb-kubernetes-init",
		})
		initContainer = &podSpec.InitContainers[len(podSpec.InitContainers)-1]
	}

	podName, instanceID := getInstanceId(cluster, processClass, idNum)

	if cluster.Spec.MainContainer.ImageName != "" {
		mainContainer.Image = cluster.Spec.MainContainer.ImageName
	}
	if mainContainer.Image == "" {
		mainContainer.Image = "foundationdb/foundationdb"
	}

	versionString := cluster.Spec.RunningVersion
	if versionString == "" {
		versionString = cluster.Spec.Version
	}

	mainContainer.Image = fmt.Sprintf("%s:%s", mainContainer.Image, versionString)

	version, err := fdbtypes.ParseFdbVersion(versionString)
	if err != nil {
		return nil, err
	}

	extendEnv(mainContainer,
		corev1.EnvVar{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
		corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/dynamic-conf/ca.pem"},
	)

	mainContainer.Command = []string{"sh", "-c"}
	mainContainer.Args = []string{
		"fdbmonitor --conffile /var/dynamic-conf/fdbmonitor.conf" +
			" --lockfile /var/fdb/fdbmonitor.lockfile",
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

	customizeContainer(mainContainer, cluster.Spec.MainContainer)

	sidecarInitEnv := make([]corev1.EnvVar, 0, 4)

	var sidecarInitArgs []string
	if version.PrefersCommandLineArgumentsInSidecar() {
		sidecarInitArgs = []string{
			"--copy-file", "fdb.cluster",
			"--copy-file", "ca.pem",
			"--input-monitor-conf", "fdbmonitor.conf",
			"--copy-binary", "fdbserver",
			"--copy-binary", "fdbcli",
		}
		if version.SupportsUsingBinariesFromMainContainer() {
			sidecarInitArgs = append(sidecarInitArgs,
				"--main-container-version", version.String(),
			)
		}
	} else {
		sidecarInitArgs = make([]string, 0)
	}

	if !version.PrefersCommandLineArgumentsInSidecar() {
		sidecarInitEnv = append(sidecarInitEnv, corev1.EnvVar{Name: "COPY_ONCE", Value: "1"})
		sidecarInitEnv = append(sidecarInitEnv, corev1.EnvVar{Name: "SIDECAR_CONF_DIR", Value: "/var/input-files"})
	}

	sidecarInitEnv = append(sidecarInitEnv, corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
		FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
	}})

	if version.PrefersCommandLineArgumentsInSidecar() {
		for _, substitution := range cluster.Spec.SidecarVariables {
			sidecarInitArgs = append(sidecarInitArgs, "--substitute-variable", substitution)
		}
		if !version.HasInstanceIdInSidecarSubstitutions() {
			sidecarInitArgs = append(sidecarInitArgs, "--substitute-variable", "FDB_INSTANCE_ID")
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
		sidecarInitEnv = append(sidecarInitEnv, corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
		}})
		sidecarInitEnv = append(sidecarInitEnv, corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
		}})
	} else if faultDomainKey == "foundationdb.org/kubernetes-cluster" {
		sidecarInitEnv = append(sidecarInitEnv, corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
		}})
		sidecarInitEnv = append(sidecarInitEnv, corev1.EnvVar{Name: "FDB_ZONE_ID", Value: cluster.Spec.FaultDomain.Value})
	} else {
		sidecarInitEnv = append(sidecarInitEnv, corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
		}})
		if !strings.HasPrefix(faultDomainSource, "$") {
			sidecarInitEnv = append(sidecarInitEnv, corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: faultDomainSource},
			}})
		}
	}

	sidecarInitEnv = append(sidecarInitEnv, corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: instanceID})

	if cluster.Spec.SidecarContainer.ImageName != "" {
		initContainer.Image = cluster.Spec.SidecarContainer.ImageName
	}

	if version.PrefersCommandLineArgumentsInSidecar() {
		sidecarInitArgs = append(sidecarInitArgs, "--init-mode")
	}

	if cluster.Spec.SidecarContainer.ImageName != "" {
		initContainer.Image = cluster.Spec.SidecarContainer.ImageName
	}
	if initContainer.Image == "" {
		initContainer.Image = "foundationdb/foundationdb-kubernetes-sidecar"
	}

	extendEnv(initContainer, sidecarInitEnv...)
	initContainer.Args = sidecarInitArgs
	initContainer.VolumeMounts = append(initContainer.VolumeMounts,
		corev1.VolumeMount{Name: "config-map", MountPath: "/var/input-files"},
		corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/output-files"},
	)
	initContainer.Image = fmt.Sprintf("%s:%s", initContainer.Image, cluster.GetFullSidecarVersion(true))

	sidecarArgs := make([]string, 0, len(sidecarInitArgs))
	sidecarEnv := make([]corev1.EnvVar, 0, len(sidecarInitEnv))

	if version.PrefersCommandLineArgumentsInSidecar() {
		sidecarArgs = append(sidecarArgs, sidecarInitArgs[:len(sidecarInitArgs)-1]...)
		sidecarEnv = append(sidecarEnv, sidecarInitEnv...)
	} else {
		sidecarArgs = append(sidecarArgs, sidecarInitArgs...)
		sidecarEnv = append(sidecarEnv, sidecarInitEnv[1:]...)
	}

	customizeContainer(initContainer, cluster.Spec.SidecarContainer)

	if cluster.Spec.SidecarContainer.ImageName != "" {
		sidecarContainer.Image = cluster.Spec.SidecarContainer.ImageName
	}
	if sidecarContainer.Image == "" {
		sidecarContainer.Image = "foundationdb/foundationdb-kubernetes-sidecar"
	}

	sidecarContainer.Image = fmt.Sprintf("%s:%s", sidecarContainer.Image, cluster.GetFullSidecarVersion(true))
	extendEnv(sidecarContainer, sidecarEnv...)
	extendEnv(sidecarContainer,
		corev1.EnvVar{Name: "FDB_TLS_VERIFY_PEERS", Value: cluster.Spec.SidecarContainer.PeerVerificationRules},
		corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/input-files/ca.pem"},
	)

	sidecarContainer.Args = append(sidecarContainer.Args, sidecarArgs...)
	if cluster.Spec.SidecarContainer.EnableTLS {
		sidecarContainer.Args = append(sidecarContainer.Args, "--tls")
	}

	sidecarContainer.VolumeMounts = append(sidecarContainer.VolumeMounts,
		corev1.VolumeMount{Name: "config-map", MountPath: "/var/input-files"},
		corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/output-files"},
	)
	if sidecarContainer.ReadinessProbe == nil {
		sidecarContainer.ReadinessProbe = &corev1.Probe{
			Handler: corev1.Handler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.IntOrString{IntVal: 8080},
				},
			},
		}
	}

	customizeContainer(sidecarContainer, cluster.Spec.SidecarContainer)

	var mainVolumeSource corev1.VolumeSource
	if usePvc(cluster, processClass) {
		mainVolumeSource.PersistentVolumeClaim = &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: fmt.Sprintf("%s-data", podName),
		}
	} else {
		mainVolumeSource.EmptyDir = &corev1.EmptyDirVolumeSource{}
	}

	configMapItems := []corev1.KeyToPath{
		corev1.KeyToPath{Key: fmt.Sprintf("fdbmonitor-conf-%s", processClass), Path: "fdbmonitor.conf"},
		corev1.KeyToPath{Key: "cluster-file", Path: "fdb.cluster"},
		corev1.KeyToPath{Key: "ca-file", Path: "ca.pem"},
	}

	if !version.PrefersCommandLineArgumentsInSidecar() {
		configMapItems = append(configMapItems, corev1.KeyToPath{Key: "sidecar-conf", Path: "config.json"})
	}

	volumes := []corev1.Volume{
		corev1.Volume{Name: "data", VolumeSource: mainVolumeSource},
		corev1.Volume{Name: "dynamic-conf", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		corev1.Volume{Name: "config-map", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: fmt.Sprintf("%s-config", cluster.Name)},
			Items:                configMapItems,
		}}},
		corev1.Volume{Name: "fdb-trace-logs", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}

	for _, volume := range cluster.Spec.Volumes {
		volumes = append(volumes, *volume.DeepCopy())
	}

	var affinity *corev1.Affinity

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

	return podSpec, nil
}

// usePvc determines whether we should attach a PVC to a pod.
func usePvc(cluster *fdbtypes.FoundationDBCluster, processClass string) bool {
	var storage *resource.Quantity
	if cluster.Spec.VolumeClaim != nil {
		requests := cluster.Spec.VolumeClaim.Spec.Resources.Requests
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
	name, id := getInstanceId(cluster, processClass, idNum)

	var pvc *corev1.PersistentVolumeClaim
	if cluster.Spec.VolumeClaim == nil {
		pvc = &corev1.PersistentVolumeClaim{}
	} else {
		pvc = cluster.Spec.VolumeClaim.DeepCopy()
	}

	pvc.ObjectMeta = getPvcMetadata(cluster, processClass, id)
	pvc.ObjectMeta.Name = fmt.Sprintf("%s-data", name)

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
