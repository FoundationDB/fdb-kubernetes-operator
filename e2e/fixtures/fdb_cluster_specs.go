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
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

// GenerateFDBClusterSpec will generate a *fdbv1beta2.FoundationDBCluster based on the input ClusterConfig.
func (factory *Factory) GenerateFDBClusterSpec(config *ClusterConfig) *fdbv1beta2.FoundationDBCluster {
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
	useUnifiedImage := pointer.BoolDeref(config.UseUnifiedImage, factory.options.featureOperatorUnifiedImage)
	imageType := fdbv1beta2.ImageTypeSplit
	if useUnifiedImage {
		imageType = fdbv1beta2.ImageTypeUnified
	}

	return &fdbv1beta2.FoundationDBCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.Name,
			Namespace: config.Namespace,
		},
		Spec: fdbv1beta2.FoundationDBClusterSpec{
			MinimumUptimeSecondsForBounce: 30,
			Version:                       factory.GetFDBVersionAsString(),
			Processes:                     factory.createProcesses(config),
			DatabaseConfiguration:         databaseConfiguration,
			StorageServersPerPod:          config.StorageServerPerPod,
			LogServersPerPod:              config.LogServersPerPod,
			LogGroup:                      config.Namespace + "-" + config.Name,
			MainContainer:                 factory.GetMainContainerOverrides(config.DebugSymbols, useUnifiedImage),
			ImageType:                     &imageType,
			SidecarContainer:              factory.GetSidecarContainerOverrides(config.DebugSymbols),
			FaultDomain: fdbv1beta2.FoundationDBClusterFaultDomain{
				Key: "foundationdb.org/none",
			},
			AutomationOptions: fdbv1beta2.FoundationDBClusterAutomationOptions{
				// We have to wait long enough to ensure the operator is not recreating too many Pods at the same time.
				WaitBetweenRemovalsSeconds: pointer.Int(0),
				Replacements: fdbv1beta2.AutomaticReplacementOptions{
					Enabled: pointer.Bool(true),
					// Setting this to 10 minutes is reasonable to prevent the operator recreating Pods when they wait for
					// new ec2 instances.
					FailureDetectionTimeSeconds: pointer.Int(600),
					// Setting TaintReplacementTimeSeconds as half of FailureDetectionTimeSeconds to make taint replacement faster
					TaintReplacementTimeSeconds: pointer.Int(150),
					MaxConcurrentReplacements:   pointer.Int(2),
				},
				MaintenanceModeOptions: fdbv1beta2.MaintenanceModeOptions{
					UseMaintenanceModeChecker: pointer.Bool(config.UseMaintenanceMode),
				},
				// Allow the operator to remove all Pods that are excluded and marked for deletion to remove at once.
				RemovalMode: fdbv1beta2.PodUpdateModeAll,
				IgnoreLogGroupsForUpgrade: []fdbv1beta2.LogGroup{
					"fdb-kubernetes-operator",
				},
				UseLocalitiesForExclusion: pointer.Bool(config.UseLocalityBasedExclusions),
			},
			Routing: fdbv1beta2.RoutingConfig{
				UseDNSInClusterFile: pointer.Bool(config.UseDNS),
				HeadlessService: pointer.Bool(
					true,
				), // to make switching between hostname <-> IP smooth
			},
		},
	}
}

func (factory *Factory) createPodTemplate(
	nodeSelector map[string]string,
	resources corev1.ResourceList,
	setLimits bool,
	sidecarResources corev1.ResourceList,
) *corev1.PodTemplateSpec {
	// the operator is causing this to not work as desired reference:
	// https://github.com/FoundationDB/fdb-kubernetes-operator/blob/main/internal/deprecations.go#L75-L77
	mainContainerResources := corev1.ResourceRequirements{
		Requests: resources,
	}

	// See: https://github.com/FoundationDB/fdb-kubernetes-operator/blob/main/docs/manual/warnings.md#resource-requirements otherwise
	// the operator will set the default limits.
	if !setLimits {
		mainContainerResources.Limits = corev1.ResourceList{
			"org.foundationdb/empty": resource.MustParse("0"),
		}
	}
	var initContainers []corev1.Container
	var annotations map[string]string
	// In the case of the unified image we don't need to use an init container.
	if !factory.options.featureOperatorUnifiedImage {
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

	return &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			// Tell the cluster autoscaler never to remove a node that an FDB pod is running on.
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: foundationdbServiceAccount,
			NodeSelector:       nodeSelector,
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
											Operator: metav1.LabelSelectorOpExists,
										},
									},
								},
							},
						},
					},
				},
			},
			InitContainers: initContainers,
			SecurityContext: &corev1.PodSecurityContext{
				FSGroup: pointer.Int64(4059),
			},
			Containers: []corev1.Container{
				{
					Name:            fdbv1beta2.MainContainerName,
					ImagePullPolicy: factory.getImagePullPolicy(),
					Resources:       mainContainerResources,
					SecurityContext: &corev1.SecurityContext{
						Privileged:               pointer.Bool(true),
						AllowPrivilegeEscalation: pointer.Bool(true), // for performance profiling
						ReadOnlyRootFilesystem: pointer.Bool(
							false,
						), // to allow I/O chaos to succeed
					},
					Env: []corev1.EnvVar{
						{
							Name:  "FDB_TLS_CERTIFICATE_FILE",
							Value: "/tmp/fdb-certs/tls.crt",
						},
						{
							Name:  "FDB_TLS_CA_FILE",
							Value: "/tmp/fdb-certs/ca.pem",
						},
						{
							Name:  "FDB_TLS_KEY_FILE",
							Value: "/tmp/fdb-certs/tls.key",
						},
						{
							Name:  "FDB_TLS_VERIFY_PEERS",
							Value: "I.CN=localhost,I.O=Example Inc.,S.CN=localhost,S.O=Example Inc.",
						},
						{
							Name:  "FDB_NETWORK_OPTION_TRACE_ENABLE",
							Value: "/var/log/fdb-trace-logs",
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
						//Privileged:               pointer.Bool(true),
						//AllowPrivilegeEscalation: pointer.Bool(true), // for performance profiling
						ReadOnlyRootFilesystem: pointer.Bool(
							false,
						), // to allow I/O chaos to succeed
					},
					Resources: corev1.ResourceRequirements{
						Requests: sidecarResources,
					},
					Env: []corev1.EnvVar{
						{
							Name:  "FDB_TLS_CERTIFICATE_FILE",
							Value: "/tmp/fdb-certs/tls.crt",
						},
						{
							Name:  "FDB_TLS_CA_FILE",
							Value: "/tmp/fdb-certs/ca.pem",
						},
						{
							Name:  "FDB_TLS_KEY_FILE",
							Value: "/tmp/fdb-certs/tls.key",
						},
						{
							Name:  "FDB_TLS_VERIFY_PEERS",
							Value: "I.CN=localhost,I.O=Example Inc.,S.CN=localhost,S.O=Example Inc.",
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
	sidecarResources := config.generateSidecarResources()

	return map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{
		fdbv1beta2.ProcessClassGeneral: {
			PodTemplate: factory.createPodTemplate(
				config.NodeSelector,
				config.generatePodResources(fdbv1beta2.ProcessClassGeneral),
				config.Performance,
				sidecarResources,
			),
			VolumeClaimTemplate: claimTemplate,
			CustomParameters:    config.getCustomParametersForProcessClass(fdbv1beta2.ProcessClassGeneral),
		},
		fdbv1beta2.ProcessClassStorage: {
			PodTemplate: factory.createPodTemplate(
				config.NodeSelector,
				config.generatePodResources(fdbv1beta2.ProcessClassStorage),
				config.Performance,
				sidecarResources,
			),
			VolumeClaimTemplate: claimTemplate,
			CustomParameters:    config.getCustomParametersForProcessClass(fdbv1beta2.ProcessClassStorage),
		},
	}
}
