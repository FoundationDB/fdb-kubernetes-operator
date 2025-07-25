/*
 * fdb_cluster_specs.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2023 Apple Inc. and the FoundationDB project authors
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

package fixtures

import (
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// GenerateFDBClusterSpec will generate a *fdbv1beta2.FoundationDBCluster based on the input ClusterConfig.
func (factory *Factory) GenerateFDBClusterSpec(
	config *ClusterConfig,
) *fdbv1beta2.FoundationDBCluster {
	config.SetDefaults(factory)

	return factory.createFDBClusterSpec(
		config,
		config.CreateDatabaseConfiguration())
}

// High Level Cluster Spec Supplied by Operator.
func (factory *Factory) createFDBClusterSpec(
	config *ClusterConfig,
	databaseConfiguration fdbv1beta2.DatabaseConfiguration,
) *fdbv1beta2.FoundationDBCluster {
	imageType := fdbv1beta2.ImageTypeSplit
	if config.GetUseUnifiedImage() {
		imageType = fdbv1beta2.ImageTypeUnified
	}

	faultDomain := fdbv1beta2.FoundationDBClusterFaultDomain{
		Key: "foundationdb.org/none",
	}

	if config.SimulateCustomFaultDomainEnv {
		faultDomain.ValueFrom = "$" + fdbv1beta2.EnvNameInstanceID
		faultDomain.Key = corev1.LabelHostname
	}

	if config.GetRedundancyMode() == fdbv1beta2.RedundancyModeThreeDataHall {
		faultDomain.Key = corev1.LabelHostname
		faultDomain.ValueFrom = ""
	}

	return &fdbv1beta2.FoundationDBCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.Name,
			Namespace: config.Namespace,
		},
		Spec: fdbv1beta2.FoundationDBClusterSpec{
			MinimumUptimeSecondsForBounce: 30,
			Version:                       config.GetVersion(),
			Processes:                     factory.createProcesses(config),
			DatabaseConfiguration:         databaseConfiguration,
			StorageServersPerPod:          config.StorageServerPerPod,
			LogServersPerPod:              config.LogServersPerPod,
			LogGroup:                      config.Namespace + "-" + config.Name,
			MainContainer: factory.GetMainContainerOverrides(
				config,
			),
			ImageType: &imageType,
			SidecarContainer: factory.GetSidecarContainerOverrides(
				config,
			),
			FaultDomain: faultDomain,
			AutomationOptions: fdbv1beta2.FoundationDBClusterAutomationOptions{
				// We have to wait long enough to ensure the operator is not recreating too many Pods at the same time.
				WaitBetweenRemovalsSeconds: ptr.To(0),
				Replacements: fdbv1beta2.AutomaticReplacementOptions{
					Enabled: ptr.To(true),
					// Setting this to 10 minutes is reasonable to prevent the operator recreating Pods when they wait for
					// new ec2 instances.
					FailureDetectionTimeSeconds: ptr.To(600),
					// Setting TaintReplacementTimeSeconds as half of FailureDetectionTimeSeconds to make taint replacement faster
					TaintReplacementTimeSeconds: ptr.To(150),
					MaxConcurrentReplacements:   ptr.To(2),
				},
				MaintenanceModeOptions: fdbv1beta2.MaintenanceModeOptions{
					UseMaintenanceModeChecker: ptr.To(config.UseMaintenanceMode),
				},
				// Allow the operator to remove all Pods that are excluded and marked for deletion to remove at once.
				RemovalMode: fdbv1beta2.PodUpdateModeAll,
				IgnoreLogGroupsForUpgrade: []fdbv1beta2.LogGroup{
					"fdb-kubernetes-operator",
				},
				UseLocalitiesForExclusion: config.UseLocalityBasedExclusions,
				SynchronizationMode:       ptr.To(string(config.SynchronizationMode)),
			},
			Routing: fdbv1beta2.RoutingConfig{
				UseDNSInClusterFile: config.UseDNS,
				HeadlessService: ptr.To(
					true,
				), // to make switching between hostname <-> IP smooth
			},
		},
	}
}

func (factory *Factory) createPodTemplate(
	processClass fdbv1beta2.ProcessClass,
	config *ClusterConfig,
) *corev1.PodTemplateSpec {
	// The operator is causing this to not work as desired reference:
	// https://github.com/FoundationDB/fdb-kubernetes-operator/v2/blob/main/internal/deprecations.go#L75-L77
	mainContainerResources := corev1.ResourceRequirements{
		Requests: config.generatePodResources(processClass),
	}

	// See: https://github.com/FoundationDB/fdb-kubernetes-operator/v2/blob/main/docs/manual/warnings.md#resource-requirements otherwise
	// the operator will set the default limits.
	if !config.Performance {
		mainContainerResources.Limits = corev1.ResourceList{
			"org.foundationdb/empty": resource.MustParse("0"),
		}
	}
	var initContainers []corev1.Container
	var annotations map[string]string
	// In the case of the unified image we don't need to use an init container.
	if !factory.UseUnifiedImage() {
		initContainers = []corev1.Container{
			{
				Name:            fdbv1beta2.InitContainerName,
				ImagePullPolicy: corev1.PullAlways,
				// We use fdbResources here to speed up the cluster creation and prevent some OOM kills when the sidecar
				// copies the binaries. In addition to that the init container won't count to the required resources
				// since the init container requires less than the running containers later (fdbResources + sideResources)
				Resources: mainContainerResources,
			},
		}
		annotations = map[string]string{
			"cluster-autoscaler.kubernetes.io/safe-to-evict": "false",
		}
	} else {
		// If we use the unified image allow prometheus to scrape the Prometheus endpoint.
		annotations = map[string]string{
			"cluster-autoscaler.kubernetes.io/safe-to-evict": "false",
			"prometheus.io/scrape":                           "true",
			"prometheus.io/port":                             "8081",
		}
	}

	var topologySpreadConstraints []corev1.TopologySpreadConstraint
	if config.GetRedundancyMode() == fdbv1beta2.RedundancyModeThreeDataHall {
		topologySpreadConstraints = []corev1.TopologySpreadConstraint{
			{
				MaxSkew:           1,
				TopologyKey:       corev1.LabelTopologyZone,
				WhenUnsatisfiable: corev1.DoNotSchedule,
				LabelSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						fdbv1beta2.FDBClusterLabel: config.Name,
					},
				},
			},
		}
	}

	return &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			// Tell the cluster autoscaler never to remove a node that an FDB pod is running on.
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: foundationdbServiceAccount,
			NodeSelector:       config.NodeSelector,
			// We add this Affinity to try to spread Pods across different nodes. Since we use the "fake"
			// fault domain the operator won't add this Affinity automatically. This requirement is optional and might
			// or might not be honoured.
			Affinity: &corev1.Affinity{
				PodAntiAffinity: &corev1.PodAntiAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
						{
							Weight: 100,
							PodAffinityTerm: corev1.PodAffinityTerm{
								TopologyKey: corev1.LabelHostname,
								LabelSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      fdbv1beta2.FDBClusterLabel,
											Values:   []string{config.Name},
											Operator: metav1.LabelSelectorOpIn,
										},
									},
								},
							},
						},
					},
				},
			},
			TopologySpreadConstraints: topologySpreadConstraints,
			InitContainers:            initContainers,
			SecurityContext: &corev1.PodSecurityContext{
				FSGroup: ptr.To[int64](4059),
			},
			Containers: []corev1.Container{
				{
					Name:            fdbv1beta2.MainContainerName,
					ImagePullPolicy: factory.getImagePullPolicy(),
					Resources:       mainContainerResources,
					SecurityContext: &corev1.SecurityContext{
						Privileged:               ptr.To(true),
						AllowPrivilegeEscalation: ptr.To(true), // for performance profiling
						ReadOnlyRootFilesystem: ptr.To(
							false,
						), // to allow I/O chaos to succeed
					},
					Env: []corev1.EnvVar{
						{
							Name:  fdbv1beta2.EnvNameTLSCert,
							Value: "/tmp/fdb-certs/tls.crt",
						},
						{
							Name:  fdbv1beta2.EnvNameTLSCaFile,
							Value: "/tmp/fdb-certs/ca.pem",
						},
						{
							Name:  fdbv1beta2.EnvNameTLSKeyFile,
							Value: "/tmp/fdb-certs/tls.key",
						},
						{
							Name:  fdbv1beta2.EnvNameTLSVerifyPeers,
							Value: config.TLSPeerVerification,
						},
						{
							Name:  fdbv1beta2.EnvNameFDBTraceLogDirPath,
							Value: "/var/log/fdb-trace-logs",
						},
						{
							Name:  "ENABLE_NODE_WATCH",
							Value: "true",
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "fdb-certs",
							ReadOnly:  true,
							MountPath: "/tmp/fdb-certs",
						},
					},
				},
				{
					Name:            fdbv1beta2.SidecarContainerName,
					ImagePullPolicy: factory.getImagePullPolicy(),
					SecurityContext: &corev1.SecurityContext{
						//Privileged:               ptr.To(true),
						//AllowPrivilegeEscalation: ptr.To(true), // for performance profiling
						ReadOnlyRootFilesystem: ptr.To(
							false,
						), // to allow I/O chaos to succeed
					},
					Resources: corev1.ResourceRequirements{
						Requests: config.generateSidecarResources(),
					},
					Env: []corev1.EnvVar{
						{
							Name:  fdbv1beta2.EnvNameTLSCert,
							Value: "/tmp/fdb-certs/tls.crt",
						},
						{
							Name:  fdbv1beta2.EnvNameTLSCaFile,
							Value: "/tmp/fdb-certs/ca.pem",
						},
						{
							Name:  fdbv1beta2.EnvNameTLSKeyFile,
							Value: "/tmp/fdb-certs/tls.key",
						},
						{
							Name:  fdbv1beta2.EnvNameTLSVerifyPeers,
							Value: config.TLSPeerVerification,
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "fdb-certs",
							ReadOnly:  true,
							MountPath: "/tmp/fdb-certs",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "fdb-certs",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: factory.GetSecretName(),
						},
					},
				},
			},
		},
	}
}

// createProcesses will generate the ProcessSettings for the cluster configuration.
func (factory *Factory) createProcesses(
	config *ClusterConfig,
) map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings {
	claimTemplate := config.generateVolumeClaimTemplate(factory.GetDefaultStorageClass())
	return map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{
		fdbv1beta2.ProcessClassGeneral: {
			PodTemplate: factory.createPodTemplate(
				fdbv1beta2.ProcessClassGeneral,
				config,
			),
			VolumeClaimTemplate: claimTemplate,
			CustomParameters: config.getCustomParametersForProcessClass(
				fdbv1beta2.ProcessClassGeneral,
			),
		},
		fdbv1beta2.ProcessClassStorage: {
			PodTemplate: factory.createPodTemplate(
				fdbv1beta2.ProcessClassStorage,
				config,
			),
			VolumeClaimTemplate: claimTemplate,
			CustomParameters: config.getCustomParametersForProcessClass(
				fdbv1beta2.ProcessClassStorage,
			),
		},
	}
}
