/*
 * pod_models_test.go
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
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
)

var _ = Describe("pod_models", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var err error

	BeforeEach(func() {
		cluster = CreateDefaultCluster()
		Expect(NormalizeClusterSpec(cluster, DeprecationOptions{})).NotTo(HaveOccurred())
	})

	Describe("GetPod", func() {
		var pod *corev1.Pod

		Context("with a basic storage process group", func() {
			BeforeEach(func() {
				pod, err = GetPod(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should contain the process group's metadata", func() {
				Expect(pod.Namespace).To(Equal("my-ns"))
				Expect(pod.Name).To(Equal(fmt.Sprintf("%s-storage-1", cluster.Name)))
				Expect(pod.ObjectMeta.Labels).To(Equal(map[string]string{
					fdbv1beta2.FDBClusterLabel:        cluster.Name,
					fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
					fdbv1beta2.FDBProcessGroupIDLabel: "storage-1",
				}))
			})

			It("should contain the process group's pod spec", func() {
				spec, err := GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
				Expect(pod.Spec).To(Equal(*spec))
			})
		})

		Context("with custom resource labels", func() {
			BeforeEach(func() {
				cluster.Spec.LabelConfig = fdbv1beta2.LabelConfig{
					MatchLabels: map[string]string{
						"fdb-custom-name":         cluster.Name,
						"fdb-managed-by-operator": "true",
					},
					ResourceLabels: map[string]string{
						"fdb-new-custom-name": cluster.Name,
					},
				}
				err = NormalizeClusterSpec(cluster, DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())

				pod, err = GetPod(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should contain the process group's metadata", func() {
				Expect(pod.Namespace).To(Equal("my-ns"))
				Expect(pod.Name).To(Equal(fmt.Sprintf("%s-storage-1", cluster.Name)))
				Expect(pod.ObjectMeta.Labels).To(Equal(map[string]string{
					"fdb-custom-name":                 cluster.Name,
					"fdb-new-custom-name":             cluster.Name,
					"fdb-managed-by-operator":         "true",
					fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
					fdbv1beta2.FDBProcessGroupIDLabel: "storage-1",
				}))
			})
		})

		Context("with a cluster controller process group", func() {
			BeforeEach(func() {
				pod, err = GetPod(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassClusterController, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should contain the process group's metadata", func() {
				Expect(pod.Name).To(Equal(fmt.Sprintf("%s-cluster-controller-1", cluster.Name)))
				Expect(pod.ObjectMeta.Labels).To(Equal(map[string]string{
					fdbv1beta2.FDBClusterLabel:        cluster.Name,
					fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassClusterController),
					fdbv1beta2.FDBProcessGroupIDLabel: "cluster_controller-1",
				}))
			})

			It("should contain the process group's pod spec", func() {
				spec, err := GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassClusterController, 1))
				Expect(err).NotTo(HaveOccurred())
				Expect(pod.Spec).To(Equal(*spec))
			})
		})

		Context("with custom annotations", func() {
			BeforeEach(func() {
				cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.ObjectMeta = metav1.ObjectMeta{
					Annotations: map[string]string{
						"fdb-annotation": "value1",
					},
				}

				pod, err = GetPod(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should add the annotations to the metadata", func() {
				hash, err := GetPodSpecHash(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1), &pod.Spec)
				Expect(err).NotTo(HaveOccurred())
				Expect(pod.ObjectMeta.Annotations).To(Equal(map[string]string{
					"fdb-annotation":                    "value1",
					fdbv1beta2.LastSpecKey:              hash,
					fdbv1beta2.PublicIPSourceAnnotation: "pod",
				}))
			})
		})

		Context("with custom labels", func() {
			BeforeEach(func() {
				cluster = CreateDefaultCluster()
				cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {PodTemplate: &corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"fdb-label": "value2",
						},
					},
				}}}
				err := NormalizeClusterSpec(cluster, DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())

				pod, err = GetPod(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should add the labels to the metadata", func() {
				Expect(pod.ObjectMeta.Labels).To(Equal(map[string]string{
					fdbv1beta2.FDBClusterLabel:        cluster.Name,
					fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
					fdbv1beta2.FDBProcessGroupIDLabel: "storage-1",
					"fdb-label":                       "value2",
				}))
			})
		})
	})

	Describe("GetPodSpec", func() {
		var spec *corev1.PodSpec

		Context("with a version compatible upgrade in progress", func() {
			BeforeEach(func() {
				cluster.Spec.Version = fdbv1beta2.Versions.NextPatchVersion.String()
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should use the image tag defined in the desired version", func() {
				for _, container := range spec.Containers {
					if container.Name == fdbv1beta2.MainContainerName {
						Expect(container.Image).To(HaveSuffix(fdbv1beta2.Versions.NextPatchVersion.String()))
						continue
					}

					Expect(container.Image).To(HaveSuffix(fdbv1beta2.Versions.NextPatchVersion.String() + "-1"))
				}
			})
		})

		Context("with a version incompatible upgrade in progress", func() {
			BeforeEach(func() {
				cluster.Spec.Version = fdbv1beta2.Versions.NextMajorVersion.String()
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should use the image tag defined in the running version", func() {
				for _, container := range spec.Containers {
					if container.Name == fdbv1beta2.MainContainerName {
						Expect(container.Image).To(HaveSuffix(cluster.Status.RunningVersion))
						continue
					}

					Expect(container.Image).To(HaveSuffix(cluster.Status.RunningVersion + "-1"))
				}
			})
		})

		Context("with a basic storage process group", func() {
			BeforeEach(func() {
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should have the built-in init container", func() {
				Expect(len(spec.InitContainers)).To(Equal(1))
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal(fdbv1beta2.InitContainerName))
				Expect(initContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes-sidecar:%s-1", cluster.Spec.Version)))
				Expect(initContainer.Args).To(Equal([]string{
					"--copy-file",
					"fdb.cluster",
					"--input-monitor-conf",
					"fdbmonitor.conf",
					"--copy-binary",
					"fdbserver",
					"--copy-binary",
					"fdbcli",
					"--main-container-version",
					cluster.Spec.Version,
					"--substitute-variable",
					"FDB_POD_IP",
					"--init-mode",
				}))
				Expect(initContainer.Env).To(Equal([]corev1.EnvVar{
					{Name: fdbv1beta2.EnvNamePublicIP, ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
				}))
				Expect(initContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
					{Name: "config-map", MountPath: "/var/input-files"},
					{Name: "dynamic-conf", MountPath: "/var/output-files"},
				}))
				Expect(initContainer.ReadinessProbe).To(BeNil())
			})

			It("should have two containers", func() {
				Expect(len(spec.Containers)).To(Equal(2))
			})

			It("should have the main foundationdb container", func() {
				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal(fdbv1beta2.MainContainerName))
				Expect(mainContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb:%s", cluster.Spec.Version)))
				Expect(mainContainer.Command).To(Equal([]string{"sh", "-c"}))
				Expect(mainContainer.Args).To(Equal([]string{
					"fdbmonitor --conffile /var/dynamic-conf/fdbmonitor.conf" +
						" --lockfile /var/dynamic-conf/fdbmonitor.lockfile" +
						" --loggroup operator-test-1" +
						" >> /var/log/fdb-trace-logs/fdbmonitor-$(date '+%Y-%m-%d').log 2>&1",
				}))

				Expect(mainContainer.Env).To(Equal([]corev1.EnvVar{
					{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
				}))

				Expect(*mainContainer.Resources.Limits.Cpu()).To(Equal(resource.MustParse("1")))
				Expect(*mainContainer.Resources.Limits.Memory()).To(Equal(resource.MustParse("4Gi")))
				Expect(*mainContainer.Resources.Requests.Cpu()).To(Equal(resource.MustParse("1")))
				Expect(*mainContainer.Resources.Requests.Memory()).To(Equal(resource.MustParse("4Gi")))

				Expect(len(mainContainer.VolumeMounts)).To(Equal(3))

				Expect(mainContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
					{Name: "data", MountPath: "/var/fdb/data"},
					{Name: "dynamic-conf", MountPath: "/var/dynamic-conf"},
					{Name: "fdb-trace-logs", MountPath: "/var/log/fdb-trace-logs"},
				}))

				Expect(*mainContainer.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
			})

			It("should have the sidecar container", func() {
				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal(fdbv1beta2.SidecarContainerName))
				Expect(sidecarContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes-sidecar:%s-1", cluster.Spec.Version)))
				Expect(sidecarContainer.Args).To(Equal([]string{
					"--copy-file",
					"fdb.cluster",
					"--input-monitor-conf",
					"fdbmonitor.conf",
					"--copy-binary",
					"fdbserver",
					"--copy-binary",
					"fdbcli",
					"--main-container-version",
					cluster.Spec.Version,
					"--substitute-variable",
					"FDB_POD_IP",
				}))
				Expect(sidecarContainer.Env).To(Equal([]corev1.EnvVar{
					{Name: fdbv1beta2.EnvNamePublicIP, ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
					{Name: "FDB_TLS_VERIFY_PEERS", Value: ""},
				}))
				Expect(sidecarContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
					{Name: "config-map", MountPath: "/var/input-files"},
					{Name: "dynamic-conf", MountPath: "/var/output-files"},
				}))
				Expect(sidecarContainer.LivenessProbe).To(Equal(&corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.IntOrString{IntVal: 8080},
						},
					},
					PeriodSeconds:    30,
					FailureThreshold: 5,
					TimeoutSeconds:   1,
				}))
				Expect(sidecarContainer.ReadinessProbe).To(BeNil())

				Expect(*sidecarContainer.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
			})

			It("should have the built-in volumes", func() {
				Expect(len(spec.Volumes)).To(Equal(4))
				Expect(spec.Volumes[0]).To(Equal(corev1.Volume{
					Name: "data",
					VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: fmt.Sprintf("%s-storage-1-data", cluster.Name),
					}},
				}))
				Expect(spec.Volumes[1]).To(Equal(corev1.Volume{
					Name:         "dynamic-conf",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				}))
				Expect(spec.Volumes[2]).To(Equal(corev1.Volume{
					Name: "config-map",
					VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: fmt.Sprintf("%s-config", cluster.Name)},
						Items: []corev1.KeyToPath{
							{Key: "fdbmonitor-conf-storage", Path: "fdbmonitor.conf"},
							{Key: ClusterFileKey, Path: "fdb.cluster"},
						},
					}},
				}))
				Expect(spec.Volumes[3]).To(Equal(corev1.Volume{
					Name:         "fdb-trace-logs",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				}))
			})

			It("should have no affinity rules", func() {
				Expect(spec.Affinity).To(BeNil())
			})

			When("having a predefined affinity rules", func() {
				BeforeEach(func() {
					affinity := &corev1.Affinity{
						PodAntiAffinity: &corev1.PodAntiAffinity{
							PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
								{
									Weight: 1,
									PodAffinityTerm: corev1.PodAffinityTerm{
										TopologyKey: "test",
										LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
											"test": "test",
										}},
									},
								},
							},
						},
					}

					cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.Spec.Affinity = affinity
					cluster.Spec.FaultDomain = fdbv1beta2.FoundationDBClusterFaultDomain{
						Value: "",
						Key:   "kubernetes.io/hostname",
					}
					spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
					Expect(err).NotTo(HaveOccurred())
				})

				It("should have both affinity rules", func() {
					Expect(len(spec.Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution)).To(BeNumerically("==", 2))
				})
			})

			Context("with the livenessProbe enabled", func() {
				BeforeEach(func() {
					enabled := true
					cluster.Spec.SidecarContainer.EnableLivenessProbe = &enabled
					spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
					Expect(err).NotTo(HaveOccurred())
				})

				It("should have a livenessProbe for the sidecar", func() {
					sidecarContainer := spec.Containers[1]
					Expect(sidecarContainer.Name).To(Equal(fdbv1beta2.SidecarContainerName))
					Expect(sidecarContainer.LivenessProbe).NotTo(BeNil())
				})

				It("should not have a livenessProbe for the init container", func() {
					sidecarContainer := spec.InitContainers[0]
					Expect(sidecarContainer.Name).To(Equal(fdbv1beta2.InitContainerName))
					Expect(sidecarContainer.LivenessProbe).To(BeNil())
				})
			})

			Context("with the readinessProbe disabled", func() {
				BeforeEach(func() {
					enabled := false
					cluster.Spec.SidecarContainer.EnableReadinessProbe = &enabled
					spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
					Expect(err).NotTo(HaveOccurred())
				})

				It("should not have a readinessProbe for the sidecar", func() {
					sidecarContainer := spec.Containers[1]
					Expect(sidecarContainer.Name).To(Equal(fdbv1beta2.SidecarContainerName))
					Expect(sidecarContainer.ReadinessProbe).To(BeNil())
				})

				It("should not have a readinessProbe for the init container", func() {
					sidecarContainer := spec.InitContainers[0]
					Expect(sidecarContainer.Name).To(Equal(fdbv1beta2.InitContainerName))
					Expect(sidecarContainer.ReadinessProbe).To(BeNil())
				})
			})

			Context("with the readinessProbe enabled", func() {
				BeforeEach(func() {
					enabled := true
					cluster.Spec.SidecarContainer.EnableReadinessProbe = &enabled
					spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
					Expect(err).NotTo(HaveOccurred())
				})

				It("should have a readinessProbe for the sidecar", func() {
					sidecarContainer := spec.Containers[1]
					Expect(sidecarContainer.Name).To(Equal(fdbv1beta2.SidecarContainerName))
					Expect(sidecarContainer.ReadinessProbe).NotTo(BeNil())
				})

				It("should not have a readinessProbe for the init container", func() {
					sidecarContainer := spec.InitContainers[0]
					Expect(sidecarContainer.Name).To(Equal(fdbv1beta2.InitContainerName))
					Expect(sidecarContainer.ReadinessProbe).To(BeNil())
				})
			})
		})

		When("the unified image is enabled", func() {
			BeforeEach(func() {
				cluster = CreateDefaultCluster()
				imageType := fdbv1beta2.ImageTypeUnified
				cluster.Spec.ImageType = &imageType
				err = NormalizeClusterSpec(cluster, DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())

			})

			When("running one storage server per disk", func() {
				BeforeEach(func() {
					spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
					Expect(err).NotTo(HaveOccurred())
				})

				It("should have no init container", func() {
					Expect(spec.InitContainers).To(HaveLen(0))
				})

				It("should have two containers", func() {
					Expect(len(spec.Containers)).To(Equal(2))
				})

				It("should have the main foundationdb container", func() {
					mainContainer := spec.Containers[0]
					Expect(mainContainer.Name).To(Equal(fdbv1beta2.MainContainerName))
					Expect(mainContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes:%s", cluster.Spec.Version)))
					Expect(mainContainer.Command).To(BeNil())
					Expect(mainContainer.Args).To(Equal([]string{
						"--input-dir", "/var/dynamic-conf",
						"--log-path", "/var/log/fdb-trace-logs/monitor.log",
					}))

					Expect(mainContainer.Env).To(Equal([]corev1.EnvVar{
						{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
						{Name: fdbv1beta2.EnvNamePublicIP, ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
						}},
						{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
						}},
						{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
						}},
						{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
						}},
						{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
						{Name: fdbv1beta2.EnvNameImageType, Value: "unified"},
						{Name: "FDB_POD_NAME", ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
						}},
						{Name: "FDB_POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
						}},
						{Name: "FDB_NETWORK_OPTION_TRACE_LOG_GROUP", Value: cluster.Name},
						{Name: "FDB_NETWORK_OPTION_TRACE_ENABLE", Value: "/var/log/fdb-trace-logs"},
					}))

					Expect(*mainContainer.Resources.Limits.Cpu()).To(Equal(resource.MustParse("1")))
					Expect(*mainContainer.Resources.Limits.Memory()).To(Equal(resource.MustParse("4Gi")))
					Expect(*mainContainer.Resources.Requests.Cpu()).To(Equal(resource.MustParse("1")))
					Expect(*mainContainer.Resources.Requests.Memory()).To(Equal(resource.MustParse("4Gi")))

					Expect(mainContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
						{Name: "data", MountPath: "/var/fdb/data"},
						{Name: "config-map", MountPath: "/var/dynamic-conf"},
						{Name: "shared-binaries", MountPath: "/var/fdb/shared-binaries"},
						{Name: "fdb-trace-logs", MountPath: "/var/log/fdb-trace-logs"},
					}))

					Expect(*mainContainer.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
				})

				It("should have the sidecar container", func() {
					sidecarContainer := spec.Containers[1]
					Expect(sidecarContainer.Name).To(Equal(fdbv1beta2.SidecarContainerName))
					Expect(sidecarContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes:%s", cluster.Spec.Version)))
					Expect(sidecarContainer.Args).To(Equal([]string{
						"--mode", "sidecar",
						"--output-dir", "/var/fdb/shared-binaries",
						"--main-container-version", cluster.Spec.Version,
						"--copy-binary", "fdbserver",
						"--copy-binary", "fdbcli",
						"--log-path", "/var/log/fdb-trace-logs/monitor.log",
					}))
					Expect(sidecarContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
						{Name: "shared-binaries", MountPath: "/var/fdb/shared-binaries"},
						{Name: "fdb-trace-logs", MountPath: "/var/log/fdb-trace-logs"},
					}))
					Expect(sidecarContainer.Env).To(BeNil())
					Expect(sidecarContainer.ReadinessProbe).To(BeNil())
					Expect(*sidecarContainer.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
				})

				It("should have the built-in volumes", func() {
					Expect(len(spec.Volumes)).To(Equal(4))
					Expect(spec.Volumes[0]).To(Equal(corev1.Volume{
						Name: "data",
						VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: fmt.Sprintf("%s-storage-1-data", cluster.Name),
						}},
					}))
					Expect(spec.Volumes[1]).To(Equal(corev1.Volume{
						Name:         "shared-binaries",
						VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
					}))
					Expect(spec.Volumes[2]).To(Equal(corev1.Volume{
						Name: "config-map",
						VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: fmt.Sprintf("%s-config", cluster.Name)},
							Items: []corev1.KeyToPath{
								{Key: "fdbmonitor-conf-storage-json", Path: "config.json"},
								{Key: ClusterFileKey, Path: "fdb.cluster"},
							},
						}},
					}))
					Expect(spec.Volumes[3]).To(Equal(corev1.Volume{
						Name:         "fdb-trace-logs",
						VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
					}))
				})

				It("should have no affinity rules", func() {
					Expect(spec.Affinity).To(BeNil())
				})
			})

			When("running multiple storage servers per disk", func() {
				BeforeEach(func() {
					cluster.Spec.StorageServersPerPod = 2
					spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
					Expect(err).NotTo(HaveOccurred())
				})

				It("should pass the process count to the main container", func() {
					mainContainer := spec.Containers[0]
					Expect(mainContainer.Name).To(Equal(fdbv1beta2.MainContainerName))
					Expect(mainContainer.Args).To(Equal([]string{
						"--input-dir", "/var/dynamic-conf",
						"--log-path", "/var/log/fdb-trace-logs/monitor.log",
						"--process-count", "2",
					}))

					Expect(mainContainer.Env).To(Equal([]corev1.EnvVar{
						{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
						{Name: "STORAGE_SERVERS_PER_POD", Value: "2"},
						{Name: fdbv1beta2.EnvNamePublicIP, ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
						}},
						{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
						}},
						{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
						}},
						{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
						}},
						{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
						{Name: fdbv1beta2.EnvNameImageType, Value: "unified"},
						{Name: "FDB_POD_NAME", ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
						}},
						{Name: "FDB_POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
						}},
						{Name: "FDB_NETWORK_OPTION_TRACE_LOG_GROUP", Value: cluster.Name},
						{Name: "FDB_NETWORK_OPTION_TRACE_ENABLE", Value: "/var/log/fdb-trace-logs"},
					}))
				})

				It("mounts the multiple-storage config map", func() {
					Expect(spec.Volumes[2]).To(Equal(corev1.Volume{
						Name: "config-map",
						VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: fmt.Sprintf("%s-config", cluster.Name)},
							Items: []corev1.KeyToPath{
								{Key: "fdbmonitor-conf-storage-json", Path: "config.json"},
								{Key: ClusterFileKey, Path: "fdb.cluster"},
							},
						}},
					}))
				})
			})

			When("creating a log with multiple storage servers per disk", func() {
				BeforeEach(func() {
					cluster.Spec.StorageServersPerPod = 2
					spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassLog, 1))
					Expect(err).NotTo(HaveOccurred())
				})

				It("should not pass the process count to the main container", func() {
					mainContainer := spec.Containers[0]
					Expect(mainContainer.Name).To(Equal(fdbv1beta2.MainContainerName))
					Expect(mainContainer.Args).To(Equal([]string{
						"--input-dir", "/var/dynamic-conf",
						"--log-path", "/var/log/fdb-trace-logs/monitor.log",
					}))

					Expect(mainContainer.Env).To(Equal([]corev1.EnvVar{
						{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
						{Name: fdbv1beta2.EnvNamePublicIP, ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
						}},
						{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
						}},
						{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
						}},
						{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
						}},
						{Name: "FDB_INSTANCE_ID", Value: "log-1"},
						{Name: fdbv1beta2.EnvNameImageType, Value: "unified"},
						{Name: "FDB_POD_NAME", ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
						}},
						{Name: "FDB_POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
						}},
						{Name: "FDB_NETWORK_OPTION_TRACE_LOG_GROUP", Value: cluster.Name},
						{Name: "FDB_NETWORK_OPTION_TRACE_ENABLE", Value: "/var/log/fdb-trace-logs"},
					}))
				})

				It("mounts the log config map", func() {
					Expect(spec.Volumes[2]).To(Equal(corev1.Volume{
						Name: "config-map",
						VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: fmt.Sprintf("%s-config", cluster.Name)},
							Items: []corev1.KeyToPath{
								{Key: "fdbmonitor-conf-log-json", Path: "config.json"},
								{Key: ClusterFileKey, Path: "fdb.cluster"},
							},
						}},
					}))
				})
			})

			When("running multiple log servers per disk", func() {
				BeforeEach(func() {
					cluster.Spec.LogServersPerPod = 2
					spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassLog, 1))
					Expect(err).NotTo(HaveOccurred())
				})

				It("should pass the process count to the main container", func() {
					mainContainer := spec.Containers[0]
					Expect(mainContainer.Name).To(Equal(fdbv1beta2.MainContainerName))
					Expect(mainContainer.Args).To(Equal([]string{
						"--input-dir", "/var/dynamic-conf",
						"--log-path", "/var/log/fdb-trace-logs/monitor.log",
						"--process-count", "2",
					}))

					Expect(mainContainer.Env).To(Equal([]corev1.EnvVar{
						{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
						{Name: "LOG_SERVERS_PER_POD", Value: "2"},
						{Name: fdbv1beta2.EnvNamePublicIP, ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
						}},
						{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
						}},
						{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
						}},
						{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
						}},
						{Name: "FDB_INSTANCE_ID", Value: "log-1"},
						{Name: fdbv1beta2.EnvNameImageType, Value: "unified"},
						{Name: "FDB_POD_NAME", ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
						}},
						{Name: "FDB_POD_NAMESPACE", ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
						}},
						{Name: "FDB_NETWORK_OPTION_TRACE_LOG_GROUP", Value: cluster.Name},
						{Name: "FDB_NETWORK_OPTION_TRACE_ENABLE", Value: "/var/log/fdb-trace-logs"},
					}))
				})

				It("mounts the multiple-storage config map", func() {
					Expect(spec.Volumes[2]).To(Equal(corev1.Volume{
						Name: "config-map",
						VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: fmt.Sprintf("%s-config", cluster.Name)},
							Items: []corev1.KeyToPath{
								{Key: "fdbmonitor-conf-log-json", Path: "config.json"},
								{Key: ClusterFileKey, Path: "fdb.cluster"},
							},
						}},
					}))
				})
			})

			When("enabling the node watch feature for the unified image", func() {
				BeforeEach(func() {
					cluster.Spec.EnableNodeWatch = pointer.Bool(true)
					spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassLog, 1))
					Expect(err).NotTo(HaveOccurred())
				})

				It("should set the flag to enable the node watch feature", func() {
					mainContainer := spec.Containers[0]
					Expect(mainContainer.Name).To(Equal(fdbv1beta2.MainContainerName))
					Expect(mainContainer.Args).To(Equal([]string{
						"--input-dir", "/var/dynamic-conf",
						"--log-path", "/var/log/fdb-trace-logs/monitor.log",
						"--enable-node-watch",
					}))
				})
			})

			Context("with an instance that is crash looping", func() {
				BeforeEach(func() {
					cluster.Spec.Buggify.CrashLoop = []fdbv1beta2.ProcessGroupID{"storage-1"}
					err := NormalizeClusterSpec(cluster, DeprecationOptions{})
					Expect(err).NotTo(HaveOccurred())
					spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
					Expect(err).NotTo(HaveOccurred())
				})

				It("should have a crash loop arg", func() {
					mainContainer := spec.Containers[0]
					Expect(mainContainer.Name).To(Equal(fdbv1beta2.MainContainerName))
					Expect(mainContainer.Command).To(Equal([]string{"crash-loop"}))
				})
			})

			Context("with an instance that is crash looping container", func() {
				BeforeEach(func() {
					cluster.Spec.Buggify.CrashLoopContainers = []fdbv1beta2.CrashLoopContainerObject{
						{
							ContainerName: fdbv1beta2.MainContainerName,
							Targets:       []fdbv1beta2.ProcessGroupID{"storage-1"},
						},
					}
					err := NormalizeClusterSpec(cluster, DeprecationOptions{})
					Expect(err).NotTo(HaveOccurred())
					spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
					Expect(err).NotTo(HaveOccurred())
				})

				It("should set the correct crash loop arg in main container", func() {
					mainContainer := spec.Containers[0]
					Expect(mainContainer.Name).To(Equal(fdbv1beta2.MainContainerName))
					Expect(mainContainer.Command).To(Equal([]string{"crash-loop"}))
				})

				It("should set the correct crash loop arg in sidecar container", func() {
					sidecarContainer := spec.Containers[1]
					Expect(sidecarContainer.Name).To(Equal(fdbv1beta2.SidecarContainerName))
					Expect(sidecarContainer.Command).ToNot(Equal([]string{"crash-loop"}))
				})
			})

			When("DNS names should be used", func() {
				var podName string

				BeforeEach(func() {
					cluster.Spec.Routing.UseDNSInClusterFile = pointer.Bool(true)
					cluster.Status.RunningVersion = fdbv1beta2.Versions.SupportsDNSInClusterFile.String()
					cluster.Spec.Version = fdbv1beta2.Versions.SupportsDNSInClusterFile.String()
					Expect(NormalizeClusterSpec(cluster, DeprecationOptions{})).NotTo(HaveOccurred())
					processGroup := GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1)
					podName = processGroup.GetPodName(cluster)
					spec, err = GetPodSpec(cluster, processGroup)
					Expect(err).NotTo(HaveOccurred())
				})

				It("should set the FDB_DNS_NAME env variable on the main container", func() {
					mainContainer := spec.Containers[0]

					Expect(mainContainer.Env).To(ContainElements(corev1.EnvVar{Name: "FDB_DNS_NAME", Value: GetPodDNSName(cluster, podName)}))
				})

				It("should set the correct crash loop arg in sidecar container", func() {
					sidecarContainer := spec.Containers[1]
					Expect(sidecarContainer.Name).To(Equal(fdbv1beta2.SidecarContainerName))
					Expect(sidecarContainer.Command).ToNot(Equal([]string{"crash-loop"}))
				})
			})
		})

		Context("with a pod IP family defined", func() {
			BeforeEach(func() {
				family := 6
				cluster.Spec.Routing.PodIPFamily = &family
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should have the built-in init container", func() {
				Expect(len(spec.InitContainers)).To(Equal(1))
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal(fdbv1beta2.InitContainerName))
				Expect(initContainer.Args).To(Equal([]string{
					"--copy-file",
					"fdb.cluster",
					"--input-monitor-conf",
					"fdbmonitor.conf",
					"--copy-binary",
					"fdbserver",
					"--copy-binary",
					"fdbcli",
					"--main-container-version",
					cluster.Spec.Version,
					"--public-ip-family",
					"6",
					"--bind-address",
					"[::]:8080",
					"--substitute-variable",
					"FDB_POD_IP",
					"--init-mode",
				}))
				Expect(initContainer.Env).To(Equal([]corev1.EnvVar{
					{Name: fdbv1beta2.EnvNamePublicIP, ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIPs"},
					}},
					{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIPs"},
					}},
					{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
				}))
			})

			It("should have the sidecar container", func() {
				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal(fdbv1beta2.SidecarContainerName))
				Expect(sidecarContainer.Args).To(Equal([]string{
					"--copy-file",
					"fdb.cluster",
					"--input-monitor-conf",
					"fdbmonitor.conf",
					"--copy-binary",
					"fdbserver",
					"--copy-binary",
					"fdbcli",
					"--main-container-version",
					cluster.Spec.Version,
					"--public-ip-family",
					"6",
					"--bind-address",
					"[::]:8080",
					"--substitute-variable",
					"FDB_POD_IP",
				}))
				Expect(sidecarContainer.Env).To(Equal([]corev1.EnvVar{
					{Name: fdbv1beta2.EnvNamePublicIP, ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIPs"},
					}},
					{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIPs"},
					}},
					{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
					{Name: "FDB_TLS_VERIFY_PEERS", Value: ""},
				}))
			})
		})

		When("enabling DNS in the cluster file", func() {
			BeforeEach(func() {
				cluster.Spec.Routing.UseDNSInClusterFile = pointer.Bool(true)
				cluster.Status.RunningVersion = fdbv1beta2.Versions.SupportsDNSInClusterFile.String()
				cluster.Spec.Version = fdbv1beta2.Versions.SupportsDNSInClusterFile.String()
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
			})

			It("should set an additional environment variable on the init container", func() {
				Expect(len(spec.InitContainers)).To(Equal(1))
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal(fdbv1beta2.InitContainerName))
				Expect(initContainer.Args).To(Equal([]string{
					"--copy-file",
					"fdb.cluster",
					"--input-monitor-conf",
					"fdbmonitor.conf",
					"--copy-binary",
					"fdbserver",
					"--copy-binary",
					"fdbcli",
					"--main-container-version",
					cluster.Spec.Version,
					"--substitute-variable",
					"FDB_POD_IP",
					"--substitute-variable",
					"FDB_DNS_NAME",
					"--init-mode",
				}))
				Expect(initContainer.Env).To(Equal([]corev1.EnvVar{
					{Name: fdbv1beta2.EnvNamePublicIP, ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
					{Name: "FDB_DNS_NAME", Value: "operator-test-1-storage-1.operator-test-1.my-ns.svc.cluster.local"},
				}))
			})

			It("should set an additional environment variable on the sidecar container", func() {
				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal(fdbv1beta2.SidecarContainerName))
				Expect(sidecarContainer.Args).To(Equal([]string{
					"--copy-file",
					"fdb.cluster",
					"--input-monitor-conf",
					"fdbmonitor.conf",
					"--copy-binary",
					"fdbserver",
					"--copy-binary",
					"fdbcli",
					"--main-container-version",
					cluster.Spec.Version,
					"--substitute-variable",
					"FDB_POD_IP",
					"--substitute-variable",
					"FDB_DNS_NAME",
				}))
				Expect(sidecarContainer.Env).To(Equal([]corev1.EnvVar{
					{Name: fdbv1beta2.EnvNamePublicIP, ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
					{Name: "FDB_DNS_NAME", Value: "operator-test-1-storage-1.operator-test-1.my-ns.svc.cluster.local"},
					{Name: "FDB_TLS_VERIFY_PEERS", Value: ""},
				}))
			})
		})

		When("enabling DNS in the locality fields", func() {
			BeforeEach(func() {
				cluster.Spec.Routing.DefineDNSLocalityFields = pointer.Bool(true)
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
			})

			It("should set an additional environment variable on the init container", func() {
				Expect(len(spec.InitContainers)).To(Equal(1))
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal("foundationdb-kubernetes-init"))
				Expect(initContainer.Args).To(Equal([]string{
					"--copy-file",
					"fdb.cluster",
					"--input-monitor-conf",
					"fdbmonitor.conf",
					"--copy-binary",
					"fdbserver",
					"--copy-binary",
					"fdbcli",
					"--main-container-version",
					"6.2.21",
					"--substitute-variable",
					"FDB_POD_IP",
					"--substitute-variable",
					"FDB_DNS_NAME",
					"--init-mode",
				}))
				Expect(initContainer.Env).To(Equal([]corev1.EnvVar{
					{Name: fdbv1beta2.EnvNamePublicIP, ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
					{Name: "FDB_DNS_NAME", Value: "operator-test-1-storage-1.operator-test-1.my-ns.svc.cluster.local"},
				}))
			})

			It("should set an additional environment variable on the sidecar container", func() {
				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal("foundationdb-kubernetes-sidecar"))
				Expect(sidecarContainer.Args).To(Equal([]string{
					"--copy-file",
					"fdb.cluster",
					"--input-monitor-conf",
					"fdbmonitor.conf",
					"--copy-binary",
					"fdbserver",
					"--copy-binary",
					"fdbcli",
					"--main-container-version",
					"6.2.21",
					"--substitute-variable",
					"FDB_POD_IP",
					"--substitute-variable",
					"FDB_DNS_NAME",
				}))
				Expect(sidecarContainer.Env).To(Equal([]corev1.EnvVar{
					{Name: fdbv1beta2.EnvNamePublicIP, ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
					{Name: "FDB_DNS_NAME", Value: "operator-test-1-storage-1.operator-test-1.my-ns.svc.cluster.local"},
					{Name: "FDB_TLS_VERIFY_PEERS", Value: ""},
				}))
			})
		})

		When("having a predefined node affinity rules", func() {
			BeforeEach(func() {
				affinity := &corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
							{
								Weight: 1,
								Preference: corev1.NodeSelectorTerm{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key: "test",
										},
									},
								},
							},
						},
					},
				}

				cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.Spec.Affinity = affinity
				cluster.Spec.FaultDomain = fdbv1beta2.FoundationDBClusterFaultDomain{
					Value: "",
					Key:   "kubernetes.io/hostname",
				}
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should have both affinity rules", func() {
				Expect(len(spec.Affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution)).To(BeNumerically("==", 1))
				Expect(len(spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution)).To(BeNumerically("==", 1))
			})
		})

		Context("with an process group that is crash looping", func() {
			BeforeEach(func() {
				cluster.Spec.Buggify.CrashLoop = []fdbv1beta2.ProcessGroupID{"storage-1"}
				err := NormalizeClusterSpec(cluster, DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should have a crash loop arg", func() {
				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal(fdbv1beta2.MainContainerName))
				Expect(mainContainer.Args).To(Equal([]string{"crash-loop"}))
			})
		})

		Context("with all process group crash looping", func() {
			BeforeEach(func() {
				cluster.Spec.Buggify.CrashLoop = []fdbv1beta2.ProcessGroupID{"*"}
				err := NormalizeClusterSpec(cluster, DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should have a crash loop arg", func() {
				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal(fdbv1beta2.MainContainerName))
				Expect(mainContainer.Args).To(Equal([]string{"crash-loop"}))
			})
		})

		Context("with a different process group crash looping", func() {
			BeforeEach(func() {
				cluster.Spec.Buggify.CrashLoop = []fdbv1beta2.ProcessGroupID{"storage-2"}
				err := NormalizeClusterSpec(cluster, DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should have the normal start command", func() {
				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal(fdbv1beta2.MainContainerName))
				Expect(mainContainer.Args).To(Equal([]string{
					"fdbmonitor --conffile /var/dynamic-conf/fdbmonitor.conf" +
						" --lockfile /var/dynamic-conf/fdbmonitor.lockfile" +
						" --loggroup operator-test-1" +
						" >> /var/log/fdb-trace-logs/fdbmonitor-$(date '+%Y-%m-%d').log 2>&1",
				}))
			})
		})

		Context("with a different process group crash looping in main container", func() {
			BeforeEach(func() {
				cluster.Spec.Buggify.CrashLoopContainers = []fdbv1beta2.CrashLoopContainerObject{
					{
						ContainerName: fdbv1beta2.MainContainerName,
						Targets:       []fdbv1beta2.ProcessGroupID{"storage-2"},
					},
				}
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should have the normal start command", func() {
				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal(fdbv1beta2.MainContainerName))
				Expect(mainContainer.Args).To(Equal([]string{
					"fdbmonitor --conffile /var/dynamic-conf/fdbmonitor.conf" +
						" --lockfile /var/dynamic-conf/fdbmonitor.lockfile" +
						" --loggroup operator-test-1" +
						" >> /var/log/fdb-trace-logs/fdbmonitor-$(date '+%Y-%m-%d').log 2>&1",
				}))
			})
		})

		Context("with a process group that defines crash looping for the main container", func() {
			BeforeEach(func() {
				cluster.Spec.Buggify.CrashLoopContainers = []fdbv1beta2.CrashLoopContainerObject{
					{
						ContainerName: fdbv1beta2.MainContainerName,
						Targets:       []fdbv1beta2.ProcessGroupID{"storage-1"},
					},
				}
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the correct start command in main container", func() {
				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal(fdbv1beta2.MainContainerName))
				Expect(mainContainer.Args).To(Equal([]string{"crash-loop"}))
			})

			It("should set the correct start command in sidecar container", func() {
				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal(fdbv1beta2.SidecarContainerName))
				Expect(sidecarContainer.Args).ToNot(Equal([]string{"crash-loop"}))
			})
		})

		Context("with a process group that defines crash looping for the sidecar container", func() {
			BeforeEach(func() {
				cluster.Spec.Buggify.CrashLoopContainers = []fdbv1beta2.CrashLoopContainerObject{
					{
						ContainerName: fdbv1beta2.SidecarContainerName,
						Targets:       []fdbv1beta2.ProcessGroupID{"storage-1"},
					},
				}
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the correct start command in main container", func() {
				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal(fdbv1beta2.MainContainerName))
				Expect(mainContainer.Args).NotTo(Equal([]string{"crash-loop"}))
			})

			It("should set the correct start command in sidecar container", func() {
				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal(fdbv1beta2.SidecarContainerName))
				Expect(sidecarContainer.Args).To(Equal([]string{"crash-loop"}))
			})
		})

		Context("with an process group with scheduling broken", func() {
			BeforeEach(func() {
				cluster.Spec.Buggify.NoSchedule = []fdbv1beta2.ProcessGroupID{"storage-1"}
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should have an affinity rule for a custom label on the node", func() {
				Expect(spec.Affinity).NotTo(BeNil())
				Expect(spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms).To(Equal([]corev1.NodeSelectorTerm{
					{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{Key: "foundationdb.org/no-schedule-allowed", Operator: corev1.NodeSelectorOpIn, Values: []string{"true"}},
						},
					},
				}))
			})
		})

		Context("with a basic storage process group with multiple storage servers per disk", func() {
			BeforeEach(func() {
				cluster.Spec.StorageServersPerPod = 2
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should have the built-in init container", func() {
				Expect(len(spec.InitContainers)).To(Equal(1))
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal(fdbv1beta2.InitContainerName))
				Expect(initContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes-sidecar:%s-1", cluster.Spec.Version)))
				Expect(initContainer.Args).To(Equal([]string{
					"--copy-file",
					"fdb.cluster",
					"--input-monitor-conf",
					"fdbmonitor.conf",
					"--copy-binary",
					"fdbserver",
					"--copy-binary",
					"fdbcli",
					"--main-container-version",
					cluster.Spec.Version,
					"--substitute-variable",
					"FDB_POD_IP",
					"--init-mode",
				}))
				Expect(initContainer.Env).To(Equal([]corev1.EnvVar{
					{Name: fdbv1beta2.EnvNamePublicIP, ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
				}))
				Expect(initContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
					{Name: "config-map", MountPath: "/var/input-files"},
					{Name: "dynamic-conf", MountPath: "/var/output-files"},
				}))
				Expect(initContainer.ReadinessProbe).To(BeNil())
			})

			It("should have two containers", func() {
				Expect(len(spec.Containers)).To(Equal(2))
			})

			It("should have the main foundationdb container", func() {
				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal(fdbv1beta2.MainContainerName))
				Expect(mainContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb:%s", cluster.Spec.Version)))
				Expect(mainContainer.Command).To(Equal([]string{"sh", "-c"}))
				Expect(mainContainer.Args).To(Equal([]string{
					"fdbmonitor --conffile /var/dynamic-conf/fdbmonitor.conf" +
						" --lockfile /var/dynamic-conf/fdbmonitor.lockfile" +
						" --loggroup operator-test-1" +
						" >> /var/log/fdb-trace-logs/fdbmonitor-$(date '+%Y-%m-%d').log 2>&1",
				}))

				Expect(mainContainer.Env).To(Equal([]corev1.EnvVar{
					{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
				}))

				Expect(*mainContainer.Resources.Limits.Cpu()).To(Equal(resource.MustParse("1")))
				Expect(*mainContainer.Resources.Limits.Memory()).To(Equal(resource.MustParse("4Gi")))
				Expect(*mainContainer.Resources.Requests.Cpu()).To(Equal(resource.MustParse("1")))
				Expect(*mainContainer.Resources.Requests.Memory()).To(Equal(resource.MustParse("4Gi")))

				Expect(len(mainContainer.VolumeMounts)).To(Equal(3))

				Expect(mainContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
					{Name: "data", MountPath: "/var/fdb/data"},
					{Name: "dynamic-conf", MountPath: "/var/dynamic-conf"},
					{Name: "fdb-trace-logs", MountPath: "/var/log/fdb-trace-logs"},
				}))

				Expect(*mainContainer.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
			})

			It("should have the sidecar container", func() {
				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal(fdbv1beta2.SidecarContainerName))
				Expect(sidecarContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes-sidecar:%s-1", cluster.Spec.Version)))
				Expect(sidecarContainer.Args).To(Equal([]string{
					"--copy-file",
					"fdb.cluster",
					"--input-monitor-conf",
					"fdbmonitor.conf",
					"--copy-binary",
					"fdbserver",
					"--copy-binary",
					"fdbcli",
					"--main-container-version",
					cluster.Spec.Version,
					"--substitute-variable",
					"FDB_POD_IP",
				}))
				Expect(sidecarContainer.Env).To(Equal([]corev1.EnvVar{
					{Name: fdbv1beta2.EnvNamePublicIP, ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
					{Name: "FDB_TLS_VERIFY_PEERS", Value: ""},
					{Name: "STORAGE_SERVERS_PER_POD", Value: "2"},
				}))
				Expect(sidecarContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
					{Name: "config-map", MountPath: "/var/input-files"},
					{Name: "dynamic-conf", MountPath: "/var/output-files"},
				}))
				Expect(sidecarContainer.LivenessProbe).To(Equal(&corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.IntOrString{IntVal: 8080},
						},
					},
					PeriodSeconds:    30,
					FailureThreshold: 5,
					TimeoutSeconds:   1,
				}))
				Expect(sidecarContainer.ReadinessProbe).To(BeNil())

				Expect(*sidecarContainer.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
			})

			It("should have the built-in volumes", func() {
				Expect(len(spec.Volumes)).To(Equal(4))
				Expect(spec.Volumes[0]).To(Equal(corev1.Volume{
					Name: "data",
					VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: fmt.Sprintf("%s-storage-1-data", cluster.Name),
					}},
				}))
				Expect(spec.Volumes[1]).To(Equal(corev1.Volume{
					Name:         "dynamic-conf",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				}))
				Expect(spec.Volumes[2]).To(Equal(corev1.Volume{
					Name: "config-map",
					VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: fmt.Sprintf("%s-config", cluster.Name)},
						Items: []corev1.KeyToPath{
							{Key: "fdbmonitor-conf-storage-density-2", Path: "fdbmonitor.conf"},
							{Key: ClusterFileKey, Path: "fdb.cluster"},
						},
					}},
				}))
				Expect(spec.Volumes[3]).To(Equal(corev1.Volume{
					Name:         "fdb-trace-logs",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				}))
			})

			It("should have no affinity rules", func() {
				Expect(spec.Affinity).To(BeNil())
			})
		})

		Context("with a the public IP from the pod", func() {
			BeforeEach(func() {
				var source = fdbv1beta2.PublicIPSourcePod
				cluster.Spec.Routing.PublicIPSource = &source
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should not have the pod IP in the init container args", func() {
				Expect(len(spec.InitContainers)).To(Equal(1))
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal(fdbv1beta2.InitContainerName))
				Expect(initContainer.Args).To(Equal([]string{
					"--copy-file",
					"fdb.cluster",
					"--input-monitor-conf",
					"fdbmonitor.conf",
					"--copy-binary",
					"fdbserver",
					"--copy-binary",
					"fdbcli",
					"--main-container-version",
					cluster.Spec.Version,
					"--substitute-variable",
					"FDB_POD_IP",
					"--init-mode",
				}))
			})

			It("should not have the pod IP in the sidecar container args", func() {
				Expect(len(spec.Containers)).To(Equal(2))
				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal(fdbv1beta2.SidecarContainerName))
				Expect(sidecarContainer.Args).To(Equal([]string{
					"--copy-file",
					"fdb.cluster",
					"--input-monitor-conf",
					"fdbmonitor.conf",
					"--copy-binary",
					"fdbserver",
					"--copy-binary",
					"fdbcli",
					"--main-container-version",
					cluster.Spec.Version,
					"--substitute-variable",
					"FDB_POD_IP",
				}))
			})

			It("should have the environment variables for the IPs in the sidecar container", func() {
				sidecarEnv := GetEnvVars(spec.Containers[1])
				Expect(sidecarEnv[fdbv1beta2.EnvNamePublicIP]).NotTo(BeNil())
				Expect(sidecarEnv[fdbv1beta2.EnvNamePublicIP].ValueFrom).NotTo(BeNil())
				Expect(sidecarEnv[fdbv1beta2.EnvNamePublicIP].ValueFrom.FieldRef.FieldPath).To(Equal("status.podIP"))
				Expect(sidecarEnv["FDB_POD_IP"]).NotTo(BeNil())
			})

			It("should have the environment variables for the IPs in the init container", func() {
				sidecarEnv := GetEnvVars(spec.InitContainers[0])
				Expect(sidecarEnv[fdbv1beta2.EnvNamePublicIP]).NotTo(BeNil())
				Expect(sidecarEnv[fdbv1beta2.EnvNamePublicIP].ValueFrom).NotTo(BeNil())
				Expect(sidecarEnv[fdbv1beta2.EnvNamePublicIP].ValueFrom.FieldRef.FieldPath).To(Equal("status.podIP"))
				Expect(sidecarEnv["FDB_POD_IP"]).NotTo(BeNil())
			})
		})

		Context("with a the public IP from the pod and an explicit listen address", func() {
			BeforeEach(func() {
				var source = fdbv1beta2.PublicIPSourcePod
				cluster.Spec.Routing.PublicIPSource = &source
				enabled := true
				cluster.Spec.UseExplicitListenAddress = &enabled
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should have the pod IP in the init container args", func() {
				Expect(len(spec.InitContainers)).To(Equal(1))
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal(fdbv1beta2.InitContainerName))
				Expect(initContainer.Args).To(Equal([]string{
					"--copy-file",
					"fdb.cluster",
					"--input-monitor-conf",
					"fdbmonitor.conf",
					"--copy-binary",
					"fdbserver",
					"--copy-binary",
					"fdbcli",
					"--main-container-version",
					cluster.Spec.Version,
					"--substitute-variable",
					"FDB_POD_IP",
					"--init-mode",
				}))
			})

			It("should have the pod IP in the sidecar container args", func() {
				Expect(len(spec.Containers)).To(Equal(2))
				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal(fdbv1beta2.SidecarContainerName))
				Expect(sidecarContainer.Args).To(Equal([]string{
					"--copy-file",
					"fdb.cluster",
					"--input-monitor-conf",
					"fdbmonitor.conf",
					"--copy-binary",
					"fdbserver",
					"--copy-binary",
					"fdbcli",
					"--main-container-version",
					cluster.Spec.Version,
					"--substitute-variable",
					"FDB_POD_IP",
				}))
			})

			It("should have the environment variables for the IPs in the sidecar container", func() {
				sidecarEnv := GetEnvVars(spec.Containers[1])
				Expect(sidecarEnv[fdbv1beta2.EnvNamePublicIP]).NotTo(BeNil())
				Expect(sidecarEnv[fdbv1beta2.EnvNamePublicIP].ValueFrom).NotTo(BeNil())
				Expect(sidecarEnv[fdbv1beta2.EnvNamePublicIP].ValueFrom.FieldRef.FieldPath).To(Equal("status.podIP"))
				Expect(sidecarEnv["FDB_POD_IP"]).NotTo(BeNil())
				Expect(sidecarEnv["FDB_POD_IP"].ValueFrom).NotTo(BeNil())
				Expect(sidecarEnv["FDB_POD_IP"].ValueFrom.FieldRef.FieldPath).To(Equal("status.podIP"))
			})

			It("should have the environment variables for the IPs in the init container", func() {
				sidecarEnv := GetEnvVars(spec.InitContainers[0])
				Expect(sidecarEnv[fdbv1beta2.EnvNamePublicIP]).NotTo(BeNil())
				Expect(sidecarEnv[fdbv1beta2.EnvNamePublicIP].ValueFrom).NotTo(BeNil())
				Expect(sidecarEnv[fdbv1beta2.EnvNamePublicIP].ValueFrom.FieldRef.FieldPath).To(Equal("status.podIP"))
				Expect(sidecarEnv["FDB_POD_IP"]).NotTo(BeNil())
				Expect(sidecarEnv["FDB_POD_IP"].ValueFrom).NotTo(BeNil())
				Expect(sidecarEnv["FDB_POD_IP"].ValueFrom.FieldRef.FieldPath).To(Equal("status.podIP"))
			})
		})

		Context("with a the public IP from the service", func() {
			BeforeEach(func() {
				var source = fdbv1beta2.PublicIPSourceService
				cluster.Spec.Routing.PublicIPSource = &source
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should have the environment variables for the IPs in the sidecar container", func() {
				sidecarEnv := GetEnvVars(spec.Containers[1])
				Expect(sidecarEnv[fdbv1beta2.EnvNamePublicIP]).NotTo(BeNil())
				Expect(sidecarEnv[fdbv1beta2.EnvNamePublicIP].ValueFrom).NotTo(BeNil())
				Expect(sidecarEnv[fdbv1beta2.EnvNamePublicIP].ValueFrom.FieldRef.FieldPath).To(Equal("metadata.annotations['foundationdb.org/public-ip']"))
				Expect(sidecarEnv["FDB_POD_IP"]).NotTo(BeNil())
				Expect(sidecarEnv["FDB_POD_IP"].ValueFrom).NotTo(BeNil())
				Expect(sidecarEnv["FDB_POD_IP"].ValueFrom.FieldRef.FieldPath).To(Equal("status.podIP"))
			})

			It("should have the environment variables for the IPs in the init container", func() {
				sidecarEnv := GetEnvVars(spec.InitContainers[0])
				Expect(sidecarEnv[fdbv1beta2.EnvNamePublicIP]).NotTo(BeNil())
				Expect(sidecarEnv[fdbv1beta2.EnvNamePublicIP].ValueFrom).NotTo(BeNil())
				Expect(sidecarEnv[fdbv1beta2.EnvNamePublicIP].ValueFrom.FieldRef.FieldPath).To(Equal("metadata.annotations['foundationdb.org/public-ip']"))
				Expect(sidecarEnv["FDB_POD_IP"]).NotTo(BeNil())
				Expect(sidecarEnv["FDB_POD_IP"].ValueFrom).NotTo(BeNil())
				Expect(sidecarEnv["FDB_POD_IP"].ValueFrom.FieldRef.FieldPath).To(Equal("status.podIP"))
			})

			It("should have the pod IP in the init container args", func() {
				Expect(len(spec.InitContainers)).To(Equal(1))
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal(fdbv1beta2.InitContainerName))
				Expect(initContainer.Args).To(Equal([]string{
					"--copy-file",
					"fdb.cluster",
					"--input-monitor-conf",
					"fdbmonitor.conf",
					"--copy-binary",
					"fdbserver",
					"--copy-binary",
					"fdbcli",
					"--main-container-version",
					cluster.Spec.Version,
					"--substitute-variable",
					"FDB_POD_IP",
					"--init-mode",
				}))
			})

			It("should have the pod IP in the sidecar container args", func() {
				Expect(len(spec.Containers)).To(Equal(2))
				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal(fdbv1beta2.SidecarContainerName))
				Expect(sidecarContainer.Args).To(Equal([]string{
					"--copy-file",
					"fdb.cluster",
					"--input-monitor-conf",
					"fdbmonitor.conf",
					"--copy-binary",
					"fdbserver",
					"--copy-binary",
					"fdbcli",
					"--main-container-version",
					cluster.Spec.Version,
					"--substitute-variable",
					"FDB_POD_IP",
				}))
			})
		})

		Context("with a headless service", func() {
			BeforeEach(func() {
				var enabled = true
				cluster.Spec.Routing.HeadlessService = &enabled
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should have the hostname and subdomain set", func() {
				Expect(spec.Hostname).To(Equal("operator-test-1-storage-1"))
				Expect(spec.Subdomain).To(Equal("operator-test-1"))
			})
		})

		Context("with no headless service", func() {
			BeforeEach(func() {
				var enabled = false
				cluster.Spec.Routing.HeadlessService = &enabled
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should not have the hostname and subdomain set", func() {
				Expect(spec.Hostname).To(Equal(""))
				Expect(spec.Subdomain).To(Equal(""))
			})
		})

		Context("with custom resources", func() {
			BeforeEach(func() {
				cluster = CreateDefaultCluster()
				cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {PodTemplate: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: fdbv1beta2.MainContainerName,
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										"cpu":    resource.MustParse("2"),
										"memory": resource.MustParse("8Gi"),
									},
									Limits: corev1.ResourceList{
										"cpu":    resource.MustParse("4"),
										"memory": resource.MustParse("16Gi"),
									},
								},
							},
						},
					},
				}}}
				err := NormalizeClusterSpec(cluster, DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())

				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the resources on the main container", func() {
				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal(fdbv1beta2.MainContainerName))
				Expect(*mainContainer.Resources.Limits.Cpu()).To(Equal(resource.MustParse("4")))
				Expect(*mainContainer.Resources.Limits.Memory()).To(Equal(resource.MustParse("16Gi")))
				Expect(*mainContainer.Resources.Requests.Cpu()).To(Equal(resource.MustParse("2")))
				Expect(*mainContainer.Resources.Requests.Memory()).To(Equal(resource.MustParse("8Gi")))
			})
		})

		Context("with a host-based fault domain", func() {
			BeforeEach(func() {
				cluster.Spec.FaultDomain = fdbv1beta2.FoundationDBClusterFaultDomain{}
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
			})

			It("should set the fault domain information in the sidecar environment", func() {
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal(fdbv1beta2.InitContainerName))
				Expect(initContainer.Env).To(Equal([]corev1.EnvVar{
					{Name: fdbv1beta2.EnvNamePublicIP, ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
					}},
					{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
					}},
					{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
				}))

			})

			It("should set the pod affinity", func() {
				Expect(spec.Affinity).To(Equal(&corev1.Affinity{
					PodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
							{
								Weight: 1,
								PodAffinityTerm: corev1.PodAffinityTerm{
									TopologyKey: "kubernetes.io/hostname",
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											fdbv1beta2.FDBClusterLabel:      cluster.Name,
											fdbv1beta2.FDBProcessClassLabel: string(fdbv1beta2.ProcessClassStorage),
										},
									},
								},
							},
						},
					},
				}))
			})
		})

		Context("with custom resource labels", func() {
			BeforeEach(func() {
				cluster.Spec.LabelConfig = fdbv1beta2.LabelConfig{
					MatchLabels:    map[string]string{"fdb-custom-name": cluster.Name, "fdb-managed-by-operator": "true"},
					ResourceLabels: map[string]string{"fdb-new-custom-name": cluster.Name},
				}
				cluster.Spec.FaultDomain = fdbv1beta2.FoundationDBClusterFaultDomain{}
				err = NormalizeClusterSpec(cluster, DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())

				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the pod affinity", func() {
				Expect(spec.Affinity).To(Equal(&corev1.Affinity{
					PodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
							{
								Weight: 1,
								PodAffinityTerm: corev1.PodAffinityTerm{
									TopologyKey: "kubernetes.io/hostname",
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"fdb-custom-name":               cluster.Name,
											fdbv1beta2.FDBProcessClassLabel: string(fdbv1beta2.ProcessClassStorage),
											"fdb-managed-by-operator":       "true",
										},
									},
								},
							},
						},
					},
				}))
			})
		})

		Context("with a custom fault domain", func() {
			BeforeEach(func() {

				cluster.Spec.FaultDomain = fdbv1beta2.FoundationDBClusterFaultDomain{
					Key:       "rack",
					ValueFrom: "$RACK",
				}
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the fault domain information in the sidecar environment", func() {
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal(fdbv1beta2.InitContainerName))
				Expect(initContainer.Env).To(Equal([]corev1.EnvVar{
					{Name: fdbv1beta2.EnvNamePublicIP, ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
					}},
					{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
				}))

			})

			It("should set the pod affinity", func() {
				Expect(spec.Affinity).To(Equal(&corev1.Affinity{
					PodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
							{
								Weight: 1,
								PodAffinityTerm: corev1.PodAffinityTerm{
									TopologyKey: "rack",
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											fdbv1beta2.FDBClusterLabel:      cluster.Name,
											fdbv1beta2.FDBProcessClassLabel: string(fdbv1beta2.ProcessClassStorage),
										},
									},
								},
							},
						},
					},
				}))
			})
		})

		Context("with cross-Kubernetes replication", func() {
			BeforeEach(func() {
				cluster.Spec.FaultDomain = fdbv1beta2.FoundationDBClusterFaultDomain{
					Key:   "foundationdb.org/kubernetes-cluster",
					Value: "kc2",
				}
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the fault domain information in the sidecar environment", func() {
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal(fdbv1beta2.InitContainerName))
				Expect(initContainer.Env).To(Equal([]corev1.EnvVar{
					{Name: fdbv1beta2.EnvNamePublicIP, ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
					}},
					{Name: "FDB_ZONE_ID", Value: "kc2"},
					{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
				}))
			})

			It("should leave the pod affinity empty", func() {
				Expect(spec.Affinity).To(BeNil())
			})
		})

		Context("with custom containers", func() {
			BeforeEach(func() {
				cluster = CreateDefaultCluster()
				cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {PodTemplate: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						InitContainers: []corev1.Container{{
							Name:    "test-container",
							Image:   "foundationdb/" + cluster.Name,
							Command: []string{"echo", "test1"},
						}},
						Containers: []corev1.Container{{
							Name:    "test-container",
							Image:   "foundationdb/" + cluster.Name,
							Command: []string{"echo", "test2"},
						}},
					},
				}}}
				err = NormalizeClusterSpec(cluster, DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())

				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should have both init containers", func() {
				Expect(len(spec.InitContainers)).To(Equal(2))

				testInitContainer := spec.InitContainers[0]
				Expect(testInitContainer.Name).To(Equal("test-container"))
				Expect(testInitContainer.Image).To(Equal("foundationdb/" + cluster.Name))
				Expect(testInitContainer.Command).To(Equal([]string{"echo", "test1"}))

				initContainer := spec.InitContainers[1]
				Expect(initContainer.Name).To(Equal(fdbv1beta2.InitContainerName))
				Expect(initContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes-sidecar:%s-1", cluster.Spec.Version)))
			})

			It("should have all three containers", func() {
				Expect(len(spec.Containers)).To(Equal(3))

				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal(fdbv1beta2.MainContainerName))
				Expect(mainContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb:%s", cluster.Spec.Version)))

				testContainer := spec.Containers[1]
				Expect(testContainer.Name).To(Equal("test-container"))
				Expect(testContainer.Image).To(Equal("foundationdb/" + cluster.Name))
				Expect(testContainer.Command).To(Equal([]string{"echo", "test2"}))

				sidecarContainer := spec.Containers[2]
				Expect(sidecarContainer.Name).To(Equal(fdbv1beta2.SidecarContainerName))
				Expect(sidecarContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes-sidecar:%s-1", cluster.Spec.Version)))
			})
		})

		Context("with custom container images with tag", func() {
			BeforeEach(func() {
				cluster = CreateDefaultCluster()
				cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {PodTemplate: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						InitContainers: []corev1.Container{{
							Name:  fdbv1beta2.InitContainerName,
							Image: "test/foundationdb-kubernetes-sidecar:dummy",
						}},
						Containers: []corev1.Container{{
							Name:  fdbv1beta2.MainContainerName,
							Image: "test/foundationdb:dummy",
						}, {
							Name:  fdbv1beta2.SidecarContainerName,
							Image: "test/foundationdb-kubernetes-sidecar:dummy",
						}},
					},
				}}}
				err = NormalizeClusterSpec(cluster, DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())
			})

			It("should return an error since a tag is specified", func() {
				_, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).To(HaveOccurred())
			})
		})

		Context("with custom environment", func() {
			BeforeEach(func() {
				cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {PodTemplate: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: fdbv1beta2.MainContainerName,
								Env: []corev1.EnvVar{
									{Name: "FDB_TLS_CERTIFICATE_FILE", Value: "/var/secrets/cert.pem"},
									{Name: "FDB_TLS_CA_FILE", Value: "/var/secrets/cert.pem"},
									{Name: "FDB_TLS_KEY_FILE", Value: "/var/secrets/cert.pem"},
								},
							},
							{
								Name: fdbv1beta2.SidecarContainerName,
								Env: []corev1.EnvVar{
									{Name: "ADDITIONAL_ENV_FILE", Value: "/var/custom-env"},
								},
							},
						},
						InitContainers: []corev1.Container{
							{
								Name: fdbv1beta2.InitContainerName,
								Env: []corev1.EnvVar{
									{Name: "ADDITIONAL_ENV_FILE", Value: "/var/custom-env-init"},
								},
							},
						},
					},
				}}}

				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the environment variables on the containers", func() {
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal(fdbv1beta2.InitContainerName))
				Expect(initContainer.Env).To(Equal([]corev1.EnvVar{
					{Name: "ADDITIONAL_ENV_FILE", Value: "/var/custom-env-init"},
					{Name: fdbv1beta2.EnvNamePublicIP, ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
				}))

				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal(fdbv1beta2.MainContainerName))
				Expect(mainContainer.Env).To(Equal([]corev1.EnvVar{
					{Name: "FDB_TLS_CERTIFICATE_FILE", Value: "/var/secrets/cert.pem"},
					{Name: "FDB_TLS_CA_FILE", Value: "/var/secrets/cert.pem"},
					{Name: "FDB_TLS_KEY_FILE", Value: "/var/secrets/cert.pem"},
					{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
				}))

				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal(fdbv1beta2.SidecarContainerName))
				Expect(sidecarContainer.Env).To(Equal([]corev1.EnvVar{
					{Name: "ADDITIONAL_ENV_FILE", Value: "/var/custom-env"},
					{Name: fdbv1beta2.EnvNamePublicIP, ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
					{Name: "FDB_TLS_VERIFY_PEERS", Value: ""},
				}))
			})
		})

		Context("with TLS for the sidecar", func() {
			BeforeEach(func() {
				cluster.Spec.SidecarContainer.EnableTLS = true
				cluster.Spec.SidecarContainer.PeerVerificationRules = "S.CN=foundationdb.org"

				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("does not pass the TLS flags to the init container", func() {
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal(fdbv1beta2.InitContainerName))
				Expect(initContainer.Args).To(Equal([]string{
					"--copy-file",
					"fdb.cluster",
					"--input-monitor-conf",
					"fdbmonitor.conf",
					"--copy-binary",
					"fdbserver",
					"--copy-binary",
					"fdbcli",
					"--main-container-version",
					cluster.Spec.Version,
					"--substitute-variable",
					"FDB_POD_IP",
					"--init-mode",
				}))
				Expect(initContainer.Env).To(Equal([]corev1.EnvVar{
					{Name: fdbv1beta2.EnvNamePublicIP, ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
				}))
			})

			It("passes the TLS flags to the sidecar", func() {
				Expect(len(spec.Containers)).To(Equal(2))

				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal(fdbv1beta2.SidecarContainerName))
				Expect(sidecarContainer.Args).To(Equal([]string{
					"--copy-file",
					"fdb.cluster",
					"--input-monitor-conf",
					"fdbmonitor.conf",
					"--copy-binary",
					"fdbserver",
					"--copy-binary",
					"fdbcli",
					"--main-container-version",
					cluster.Spec.Version,
					"--substitute-variable",
					"FDB_POD_IP",
					"--tls",
				}))
				Expect(sidecarContainer.Env).To(Equal([]corev1.EnvVar{
					{Name: fdbv1beta2.EnvNamePublicIP, ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
					{Name: "FDB_TLS_VERIFY_PEERS", Value: "S.CN=foundationdb.org"},
				}))
			})
		})

		Context("with custom volumes", func() {
			BeforeEach(func() {
				cluster = CreateDefaultCluster()
				cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {PodTemplate: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Volumes: []corev1.Volume{{
							Name: "test-secrets",
							VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
								SecretName: "test-secrets",
							}},
						}},
						Containers: []corev1.Container{
							{
								Name: fdbv1beta2.MainContainerName,
								VolumeMounts: []corev1.VolumeMount{{
									Name:      "test-secrets",
									MountPath: "/var/secrets",
								}},
							},
						},
					},
				}}}
				err = NormalizeClusterSpec(cluster, DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())

				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("adds volumes to the container", func() {
				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal(fdbv1beta2.MainContainerName))
				Expect(mainContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
					{Name: "test-secrets", MountPath: "/var/secrets"},
					{Name: "data", MountPath: "/var/fdb/data"},
					{Name: "dynamic-conf", MountPath: "/var/dynamic-conf"},
					{Name: "fdb-trace-logs", MountPath: "/var/log/fdb-trace-logs"},
				}))
			})

			It("does not add volumes to the sidecar container", func() {
				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal(fdbv1beta2.SidecarContainerName))

				Expect(sidecarContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
					{Name: "config-map", MountPath: "/var/input-files"},
					{Name: "dynamic-conf", MountPath: "/var/output-files"},
				}))

			})

			It("adds volumes to the pod spec", func() {
				Expect(len(spec.Volumes)).To(Equal(5))
				Expect(spec.Volumes[0]).To(Equal(corev1.Volume{
					Name: "test-secrets",
					VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
						SecretName: "test-secrets",
					}},
				}))
				Expect(spec.Volumes[1]).To(Equal(corev1.Volume{
					VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: fmt.Sprintf("%s-storage-1-data", cluster.Name),
					}},
					Name: "data",
				}))
				Expect(spec.Volumes[2]).To(Equal(corev1.Volume{
					Name:         "dynamic-conf",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				}))
				Expect(spec.Volumes[3]).To(Equal(corev1.Volume{
					Name: "config-map",
					VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: fmt.Sprintf("%s-config", cluster.Name)},
						Items: []corev1.KeyToPath{
							{Key: "fdbmonitor-conf-storage", Path: "fdbmonitor.conf"},
							{Key: ClusterFileKey, Path: "fdb.cluster"},
						},
					}},
				}))
				Expect(spec.Volumes[4]).To(Equal(corev1.Volume{
					Name:         "fdb-trace-logs",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				}))
			})
		})

		Context("with custom sidecar version", func() {
			BeforeEach(func() {
				cluster.Spec.SidecarContainer.ImageConfigs = []fdbv1beta2.ImageConfig{
					{Version: cluster.Spec.Version, TagSuffix: "-2"},
					{Version: "6.1.0", TagSuffix: "-3"},
				}
				err = NormalizeClusterSpec(cluster, DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("sets the images on the containers", func() {
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal(fdbv1beta2.InitContainerName))
				Expect(initContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes-sidecar:%s-2", cluster.Spec.Version)))

				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal(fdbv1beta2.MainContainerName))
				Expect(mainContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb:%s", cluster.Spec.Version)))

				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal(fdbv1beta2.SidecarContainerName))
				Expect(sidecarContainer.Image).To(Equal(initContainer.Image))
			})
		})

		Context("with a custom security context", func() {
			BeforeEach(func() {

				podSecurityContext := &corev1.PodSecurityContext{FSGroup: new(int64)}
				*podSecurityContext.FSGroup = 5000
				mainSecurityContext := &corev1.SecurityContext{RunAsGroup: new(int64), RunAsUser: new(int64)}
				*mainSecurityContext.RunAsGroup = 3000
				*mainSecurityContext.RunAsUser = 4000
				sidecarSecurityContext := &corev1.SecurityContext{RunAsGroup: new(int64), RunAsUser: new(int64)}
				*sidecarSecurityContext.RunAsGroup = 1000
				*sidecarSecurityContext.RunAsUser = 2000

				cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {PodTemplate: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						SecurityContext: podSecurityContext,
						Containers: []corev1.Container{
							{
								Name:            fdbv1beta2.MainContainerName,
								SecurityContext: mainSecurityContext,
							},
							{
								Name:            fdbv1beta2.SidecarContainerName,
								SecurityContext: sidecarSecurityContext,
							},
						},
						InitContainers: []corev1.Container{
							{
								Name:            fdbv1beta2.InitContainerName,
								SecurityContext: sidecarSecurityContext,
							},
						},
					},
				}}}

				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the security contexts", func() {

				Expect(*spec.SecurityContext.FSGroup).To(Equal(int64(5000)))

				Expect(len(spec.InitContainers)).To(Equal(1))
				initContainer := spec.InitContainers[0]
				Expect(*initContainer.SecurityContext.RunAsGroup).To(Equal(int64(1000)))
				Expect(*initContainer.SecurityContext.RunAsUser).To(Equal(int64(2000)))

				mainContainer := spec.Containers[0]
				Expect(*mainContainer.SecurityContext.RunAsGroup).To(Equal(int64(3000)))
				Expect(*mainContainer.SecurityContext.RunAsUser).To(Equal(int64(4000)))

				sidecarContainer := spec.Containers[1]
				Expect(*sidecarContainer.SecurityContext.RunAsGroup).To(Equal(int64(1000)))
				Expect(*sidecarContainer.SecurityContext.RunAsUser).To(Equal(int64(2000)))

			})
		})

		Context("with an process group ID prefix", func() {
			BeforeEach(func() {
				cluster.Spec.ProcessGroupIDPrefix = "dc1"
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should put the prefix in the instance ID", func() {
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Env).To(Equal([]corev1.EnvVar{
					{Name: fdbv1beta2.EnvNamePublicIP, ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_INSTANCE_ID", Value: "dc1-storage-1"},
				}))
			})
		})

		Context("with custom map", func() {
			BeforeEach(func() {
				cluster.Spec.ConfigMap = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "config1"}}
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("adds config-map volume that refers to custom map", func() {
				Expect(spec.Volumes[2].VolumeSource.ConfigMap.LocalObjectReference.Name).To(Equal(fmt.Sprintf("%s-%s", cluster.Name, "config1")))
			})
		})

		Context("with no custom map", func() {
			BeforeEach(func() {
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})
			It("adds config-map volume that refers to custom map", func() {
				Expect(spec.Volumes[2].VolumeSource.ConfigMap.LocalObjectReference.Name).To(Equal(fmt.Sprintf("%s-%s", cluster.Name, "config")))
			})
		})

		Context("with custom pvc", func() {
			BeforeEach(func() {
				cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {VolumeClaimTemplate: &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "claim1"}}}}
				err = NormalizeClusterSpec(cluster, DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())

				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("adds data volume that refers to custom pvc", func() {
				Expect(spec.Volumes[0].VolumeSource.PersistentVolumeClaim.ClaimName).To(Equal(fmt.Sprintf("%s-storage-1-%s", cluster.Name, "claim1")))
			})
		})

		Context("with no custom pvc", func() {
			BeforeEach(func() {
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})
			It("adds data volume that refers to default pvc", func() {
				Expect(spec.Volumes[0].VolumeSource.PersistentVolumeClaim.ClaimName).To(Equal(fmt.Sprintf("%s-storage-1-%s", cluster.Name, "data")))
			})
		})

		Context("with a custom CA", func() {
			BeforeEach(func() {
				cluster.Spec.TrustedCAs = []string{"Test"}
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
			})

			It("should pass the CA file to the main container", func() {
				mainContainer := spec.Containers[0]
				Expect(mainContainer.Env).To(Equal([]corev1.EnvVar{
					{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
					{Name: "FDB_TLS_CA_FILE", Value: "/var/dynamic-conf/ca.pem"},
				}))
			})

			It("should pass the CA file to the init container", func() {
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Args).To(Equal([]string{
					"--copy-file",
					"fdb.cluster",
					"--copy-file",
					"ca.pem",
					"--input-monitor-conf",
					"fdbmonitor.conf",
					"--copy-binary",
					"fdbserver",
					"--copy-binary",
					"fdbcli",
					"--main-container-version",
					cluster.Spec.Version,
					"--substitute-variable",
					"FDB_POD_IP",
					"--init-mode",
				}))
			})

			It("should pass the CA to the sidecar container", func() {
				sidecarContainer := spec.Containers[1]

				Expect(sidecarContainer.Args).To(Equal([]string{
					"--copy-file",
					"fdb.cluster",
					"--copy-file",
					"ca.pem",
					"--input-monitor-conf",
					"fdbmonitor.conf",
					"--copy-binary",
					"fdbserver",
					"--copy-binary",
					"fdbcli",
					"--main-container-version",
					cluster.Spec.Version,
					"--substitute-variable",
					"FDB_POD_IP",
				}))

				Expect(sidecarContainer.Env).To(Equal([]corev1.EnvVar{
					{Name: fdbv1beta2.EnvNamePublicIP, ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_POD_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
					{Name: "FDB_TLS_VERIFY_PEERS", Value: ""},
					{Name: "FDB_TLS_CA_FILE", Value: "/var/input-files/ca.pem"},
				}))
			})

			It("should have the CA file in the config map volume", func() {
				Expect(len(spec.Volumes)).To(Equal(4))
				Expect(spec.Volumes[2]).To(Equal(corev1.Volume{
					Name: "config-map",
					VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: fmt.Sprintf("%s-config", cluster.Name)},
						Items: []corev1.KeyToPath{
							{Key: "fdbmonitor-conf-storage", Path: "fdbmonitor.conf"},
							{Key: ClusterFileKey, Path: "fdb.cluster"},
							{Key: "ca-file", Path: "ca.pem"},
						},
					}},
				}))
			})
		})

		Context("with ReadOnlyRootFilesystem disabled", func() {
			BeforeEach(func() {
				var enabled = false
				cluster.Spec.Processes["general"] = fdbv1beta2.ProcessSettings{
					PodTemplate: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: fdbv1beta2.MainContainerName,
									SecurityContext: &corev1.SecurityContext{
										ReadOnlyRootFilesystem: &enabled,
									},
								},
							},
						},
					},
				}
				err = NormalizeClusterSpec(cluster, DeprecationOptions{})
				Expect(err).ToNot(HaveOccurred())
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).ToNot(HaveOccurred())
			})

			It("should have ReadOnlyRootFilesystem set to false", func() {
				checked := false

				for _, container := range spec.Containers {
					if container.Name != fdbv1beta2.MainContainerName {
						continue
					}
					Expect(container.SecurityContext).ToNot(BeNil())
					Expect(*container.SecurityContext.ReadOnlyRootFilesystem).To(BeFalse())
					checked = true
				}

				Expect(checked).To(BeTrue())
			})
		})

		Context("with an empty SecurityContext", func() {
			BeforeEach(func() {
				cluster.Spec.Processes["general"] = fdbv1beta2.ProcessSettings{
					PodTemplate: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: fdbv1beta2.MainContainerName,
								},
							},
						},
					},
				}
				err = NormalizeClusterSpec(cluster, DeprecationOptions{})
				Expect(err).ToNot(HaveOccurred())
				spec, err = GetPodSpec(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).ToNot(HaveOccurred())
			})

			It("should have ReadOnlyRootFilesystem set to true", func() {
				checked := false

				for _, container := range spec.Containers {
					if container.Name != fdbv1beta2.MainContainerName {
						continue
					}
					Expect(container.SecurityContext).ToNot(BeNil())
					Expect(*container.SecurityContext.ReadOnlyRootFilesystem).To(BeTrue())
					checked = true
				}

				Expect(checked).To(BeTrue())
			})
		})
	})

	Describe("GetService", func() {
		var service *corev1.Service

		Context("with a basic storage process group", func() {
			BeforeEach(func() {
				service, err = GetService(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the metadata on the service", func() {
				Expect(service.Namespace).To(Equal("my-ns"))
				Expect(service.Name).To(Equal(fmt.Sprintf("%s-storage-1", cluster.Name)))
				Expect(service.ObjectMeta.Labels).To(Equal(map[string]string{
					fdbv1beta2.FDBClusterLabel:        cluster.Name,
					fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
					fdbv1beta2.FDBProcessGroupIDLabel: "storage-1",
				}))
			})

			It("should set the spec on the service", func() {
				Expect(service.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))

				Expect(len(service.Spec.Ports)).To(Equal(2))
				Expect(service.Spec.Ports[0].Name).To(Equal("tls"))
				Expect(service.Spec.Ports[0].Port).To(Equal(int32(4500)))
				Expect(service.Spec.Ports[1].Name).To(Equal("non-tls"))
				Expect(service.Spec.Ports[1].Port).To(Equal(int32(4501)))

				Expect(service.Spec.Selector).To(Equal(map[string]string{
					fdbv1beta2.FDBClusterLabel:        cluster.Name,
					fdbv1beta2.FDBProcessGroupIDLabel: "storage-1",
				}))

				Expect(service.Spec.IPFamilies).To(BeNil())
			})
		})

		Context("with podIPFamily 6", func() {
			BeforeEach(func() {
				cluster.Spec.Routing.PodIPFamily = pointer.Int(6)
				service, err = GetService(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the spec on the service", func() {
				Expect(service.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))

				Expect(len(service.Spec.Ports)).To(Equal(2))
				Expect(service.Spec.Ports[0].Name).To(Equal("tls"))
				Expect(service.Spec.Ports[0].Port).To(Equal(int32(4500)))
				Expect(service.Spec.Ports[1].Name).To(Equal("non-tls"))
				Expect(service.Spec.Ports[1].Port).To(Equal(int32(4501)))

				Expect(service.Spec.Selector).To(Equal(map[string]string{
					fdbv1beta2.FDBClusterLabel:        cluster.Name,
					fdbv1beta2.FDBProcessGroupIDLabel: "storage-1",
				}))

				Expect(service.Spec.IPFamilies).To(HaveLen(1))
				Expect(service.Spec.IPFamilies[0]).To(Equal(corev1.IPv6Protocol))
			})

			AfterEach(func() {
				cluster.Spec.Routing.PodIPFamily = nil
			})
		})

		Context("with custom resource labels", func() {
			BeforeEach(func() {
				cluster.Spec.LabelConfig = fdbv1beta2.LabelConfig{
					MatchLabels:    map[string]string{"fdb-custom-name": cluster.Name, "fdb-managed-by-operator": "true"},
					ResourceLabels: map[string]string{"fdb-new-custom-name": cluster.Name},
				}
				err = NormalizeClusterSpec(cluster, DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())

				service, err = GetService(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the metadata on the service", func() {
				Expect(service.Namespace).To(Equal("my-ns"))
				Expect(service.Name).To(Equal(fmt.Sprintf("%s-storage-1", cluster.Name)))
				Expect(service.ObjectMeta.Labels).To(Equal(map[string]string{
					"fdb-custom-name":                 cluster.Name,
					"fdb-managed-by-operator":         "true",
					"fdb-new-custom-name":             cluster.Name,
					fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
					fdbv1beta2.FDBProcessGroupIDLabel: "storage-1",
				}))
			})

			It("should set the spec on the service", func() {
				Expect(service.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))

				Expect(len(service.Spec.Ports)).To(Equal(2))
				Expect(service.Spec.Ports[0].Name).To(Equal("tls"))
				Expect(service.Spec.Ports[0].Port).To(Equal(int32(4500)))
				Expect(service.Spec.Ports[1].Name).To(Equal("non-tls"))
				Expect(service.Spec.Ports[1].Port).To(Equal(int32(4501)))

				Expect(service.Spec.Selector).To(Equal(map[string]string{
					"fdb-custom-name":                 cluster.Name,
					"fdb-managed-by-operator":         "true",
					fdbv1beta2.FDBProcessGroupIDLabel: "storage-1",
				}))
			})
		})
	})

	Describe("GetPvc", func() {
		var pvc *corev1.PersistentVolumeClaim

		Context("with a basic storage process group", func() {
			BeforeEach(func() {
				pvc, err = GetPvc(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the metadata on the PVC", func() {
				Expect(pvc.Namespace).To(Equal("my-ns"))
				Expect(pvc.Name).To(Equal(fmt.Sprintf("%s-storage-1-data", cluster.Name)))
				Expect(pvc.ObjectMeta.Labels).To(Equal(map[string]string{
					fdbv1beta2.FDBClusterLabel:        cluster.Name,
					fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
					fdbv1beta2.FDBProcessGroupIDLabel: "storage-1",
				}))
			})

			It("should set the spec on the PVC", func() {
				Expect(pvc.Spec).To(Equal(corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("128G"),
						},
					},
				}))
			})
		})

		Context("with custom resource labels", func() {
			BeforeEach(func() {
				cluster.Spec.LabelConfig = fdbv1beta2.LabelConfig{
					MatchLabels:    map[string]string{"fdb-custom-name": cluster.Name, "fdb-managed-by-operator": "true"},
					ResourceLabels: map[string]string{"fdb-new-custom-name": cluster.Name},
				}
				err = NormalizeClusterSpec(cluster, DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())

				pvc, err = GetPvc(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the metadata on the PVC", func() {
				Expect(pvc.Namespace).To(Equal("my-ns"))
				Expect(pvc.Name).To(Equal(fmt.Sprintf("%s-storage-1-data", cluster.Name)))
				Expect(pvc.ObjectMeta.Labels).To(Equal(map[string]string{
					"fdb-custom-name":                 cluster.Name,
					"fdb-new-custom-name":             cluster.Name,
					"fdb-managed-by-operator":         "true",
					fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
					fdbv1beta2.FDBProcessGroupIDLabel: "storage-1",
				}))
			})
		})

		Context("with a custom storage size", func() {
			BeforeEach(func() {
				cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {VolumeClaimTemplate: &corev1.PersistentVolumeClaim{
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceStorage: resource.MustParse("64G"),
							},
						},
					},
				}}}
				pvc, err = GetPvc(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the storage size on the resources", func() {
				Expect(pvc.Spec.Resources).To(Equal(corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("64G"),
					},
				}))
			})
		})

		Context("with custom metadata", func() {
			BeforeEach(func() {
				cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {VolumeClaimTemplate: &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"fdb-annotation": "value1",
						},
						Labels: map[string]string{
							"fdb-label": "value2",
						},
					},
				}}}
				pvc, err = GetPvc(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the metadata on the PVC", func() {
				Expect(pvc.Namespace).To(Equal("my-ns"))
				Expect(pvc.Name).To(Equal(fmt.Sprintf("%s-storage-1-data", cluster.Name)))
				Expect(pvc.ObjectMeta.Annotations).To(Equal(map[string]string{
					"fdb-annotation":                     "value1",
					"foundationdb.org/last-applied-spec": "f0c8a45ea6c3dd26c2dc2b5f3c699f38d613dab273d0f8a6eae6abd9a9569063",
				}))
				Expect(pvc.ObjectMeta.Labels).To(Equal(map[string]string{
					fdbv1beta2.FDBClusterLabel:        cluster.Name,
					fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
					fdbv1beta2.FDBProcessGroupIDLabel: "storage-1",
					"fdb-label":                       "value2",
				}))
			})
		})

		Context("for a stateless process group", func() {
			BeforeEach(func() {
				pvc, err = GetPvc(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStateless, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("returns a nil PVC", func() {
				Expect(pvc).To(BeNil())
			})
		})

		Context("with an process group ID prefix", func() {
			BeforeEach(func() {
				cluster.Spec.ProcessGroupIDPrefix = "dc1"
				pvc, err = GetPvc(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include the prefix in the process group IDs", func() {
				Expect(pvc.Name).To(Equal(fmt.Sprintf("%s-storage-1-data", cluster.Name)))
				Expect(pvc.ObjectMeta.Labels).To(Equal(map[string]string{
					fdbv1beta2.FDBClusterLabel:        cluster.Name,
					fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
					fdbv1beta2.FDBProcessGroupIDLabel: "dc1-storage-1",
				}))
			})
		})

		Context("with a custom storage class", func() {
			var class string
			BeforeEach(func() {
				class = "local"
				cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {VolumeClaimTemplate: &corev1.PersistentVolumeClaim{
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &class,
					},
				}}}
				pvc, err = GetPvc(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the storage class on the PVC", func() {
				Expect(pvc.Spec.StorageClassName).To(Equal(&class))
			})
		})

		Context("with an process group ID prefix", func() {
			BeforeEach(func() {
				cluster.Spec.ProcessGroupIDPrefix = "dc1"
				pvc, err = GetPvc(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include the prefix in the process group IDs", func() {
				Expect(pvc.Name).To(Equal(fmt.Sprintf("%s-storage-1-data", cluster.Name)))
				Expect(pvc.ObjectMeta.Labels).To(Equal(map[string]string{
					fdbv1beta2.FDBClusterLabel:        cluster.Name,
					fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
					fdbv1beta2.FDBProcessGroupIDLabel: "dc1-storage-1",
				}))
			})
		})

		Context("with custom name in the suffix", func() {
			BeforeEach(func() {
				cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {VolumeClaimTemplate: &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Name: "pvc1"},
				}}}
				pvc, err = GetPvc(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include claim name with custom suffix", func() {
				Expect(pvc.Name).To(Equal(fmt.Sprintf("%s-storage-1-pvc1", cluster.Name)))
			})
		})

		Context("with default name in the suffix", func() {
			BeforeEach(func() {
				pvc, err = GetPvc(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include claim name with default suffix", func() {
				Expect(pvc.Name).To(Equal(fmt.Sprintf("%s-storage-1-data", cluster.Name)))
			})
		})
	})

	Describe("GetHeadlessService", func() {
		var service *corev1.Service
		var enabled = true

		BeforeEach(func() {
			cluster.Spec.Routing.HeadlessService = &enabled
		})

		JustBeforeEach(func() {
			service = GetHeadlessService(cluster)
		})

		Context("with the default config", func() {
			It("should set the metadata on the service", func() {
				Expect(service.ObjectMeta.Namespace).To(Equal("my-ns"))
				Expect(service.ObjectMeta.Name).To(Equal("operator-test-1"))
				Expect(service.ObjectMeta.Labels).To(Equal(map[string]string{
					fdbv1beta2.FDBClusterLabel: "operator-test-1",
				}))
			})

			It("should use the default service spec", func() {
				Expect(service.Spec).To(Equal(corev1.ServiceSpec{
					ClusterIP: "None",
					Selector: map[string]string{
						fdbv1beta2.FDBClusterLabel: "operator-test-1",
					},
				}))
			})
		})

		Context("with IPFamily", func() {
			BeforeEach(func() {
				cluster.Spec.Routing.PodIPFamily = pointer.Int(6)
				cluster.Spec.Routing.UseDNSInClusterFile = pointer.Bool(true)
				err = NormalizeClusterSpec(cluster, DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())
			})

			It("should update the IPFamilies in the service spec", func() {
				Expect(service.ObjectMeta.Namespace).To(Equal("my-ns"))
				Expect(service.ObjectMeta.Name).To(Equal("operator-test-1"))
				Expect(len(service.Spec.IPFamilies)).To(Equal(1))
				Expect(service.Spec.IPFamilies).To(Equal([]corev1.IPFamily{corev1.IPv6Protocol}))
			})
		})

		Context("with custom resource labels", func() {
			BeforeEach(func() {
				cluster.Spec.LabelConfig = fdbv1beta2.LabelConfig{
					MatchLabels:    map[string]string{"fdb-custom-name": cluster.Name, "fdb-managed-by-operator": "true"},
					ResourceLabels: map[string]string{"fdb-new-custom-name": cluster.Name},
				}
				err = NormalizeClusterSpec(cluster, DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the metadata on the service", func() {
				Expect(service.ObjectMeta.Namespace).To(Equal("my-ns"))
				Expect(service.ObjectMeta.Name).To(Equal("operator-test-1"))
				Expect(service.ObjectMeta.Labels).To(Equal(map[string]string{
					"fdb-custom-name":         "operator-test-1",
					"fdb-new-custom-name":     "operator-test-1",
					"fdb-managed-by-operator": "true",
				}))
			})

			It("should use the default service spec", func() {
				Expect(service.Spec).To(Equal(corev1.ServiceSpec{
					ClusterIP: "None",
					Selector: map[string]string{
						"fdb-custom-name":         "operator-test-1",
						"fdb-managed-by-operator": "true",
					},
				}))
			})
		})

		Context("with the headless service disabled", func() {
			BeforeEach(func() {
				enabled = false
			})

			It("should return nil", func() {
				Expect(service).To(BeNil())
			})
		})

		Context("with a nil headless flag", func() {
			BeforeEach(func() {
				cluster.Spec.Routing.HeadlessService = nil
			})

			It("should return nil", func() {
				Expect(service).To(BeNil())
			})
		})
	})

	When("getting the backup deployment", func() {
		var backup *fdbv1beta2.FoundationDBBackup
		var deployment *appsv1.Deployment

		BeforeEach(func() {
			backup = CreateDefaultBackup(cluster)
			Expect(backup.Name).To(Equal("operator-test-1"))
		})

		Context("with a basic deployment", func() {
			BeforeEach(func() {
				deployment, err = GetBackupDeployment(backup)
				Expect(err).NotTo(HaveOccurred())
				Expect(deployment).NotTo(BeNil())
			})

			It("should set the metadata for the deployment", func() {
				Expect(deployment.ObjectMeta.Name).To(Equal("operator-test-1-backup-agents"))
				Expect(len(deployment.ObjectMeta.OwnerReferences)).To(Equal(1))
				Expect(deployment.ObjectMeta.OwnerReferences[0].UID).To(Equal(cluster.ObjectMeta.UID))
				Expect(deployment.ObjectMeta.Labels).To(Equal(map[string]string{
					fdbv1beta2.BackupDeploymentLabel: string(cluster.ObjectMeta.UID),
				}))
				Expect(deployment.ObjectMeta.Annotations).To(Equal(map[string]string{
					"foundationdb.org/last-applied-spec": "a1d6ee086624f097243eb14aefbedf84a9016ba0f85640f11020bf50ec9e6d9b",
				}))
			})

			It("should set the replication factor to the specified agent count", func() {
				Expect(deployment.Spec.Replicas).NotTo(BeNil())
				Expect(*deployment.Spec.Replicas).To(Equal(int32(3)))
			})

			It("should set the labels for the pod selector", func() {
				Expect(*deployment.Spec.Selector).To(Equal(metav1.LabelSelector{MatchLabels: map[string]string{
					fdbv1beta2.BackupDeploymentPodLabel: "operator-test-1-backup-agents",
				}}))
				Expect(deployment.Spec.Template.ObjectMeta.Labels).To(Equal(map[string]string{
					fdbv1beta2.BackupDeploymentPodLabel: "operator-test-1-backup-agents",
				}))
			})

			It("should have one container and one init container", func() {
				Expect(len(deployment.Spec.Template.Spec.Containers)).To(Equal(1))
				Expect(len(deployment.Spec.Template.Spec.InitContainers)).To(Equal(1))
			})

			It("should have the default volumes", func() {
				Expect(deployment.Spec.Template.Spec.Volumes).To(Equal([]corev1.Volume{
					{Name: "logs", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					{Name: "dynamic-conf", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					{
						Name: "config-map",
						VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: fmt.Sprintf("%s-config", cluster.Name)},
							Items: []corev1.KeyToPath{
								{Key: ClusterFileKey, Path: "fdb.cluster"},
							},
						}},
					},
				}))
			})

			Describe("the main container", func() {
				var container corev1.Container

				BeforeEach(func() {
					container = deployment.Spec.Template.Spec.Containers[0]
				})

				It("should set the container name", func() {
					Expect(container.Name).To(Equal(fdbv1beta2.MainContainerName))
				})

				It("should set the image and command for the backup agent", func() {
					Expect(container.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb:%s", cluster.Spec.Version)))
					Expect(container.Command).To(Equal([]string{"backup_agent"}))
					Expect(container.Args).To(Equal([]string{
						"--log",
						"--logdir",
						"/var/log/fdb-trace-logs",
					}))
				})

				It("should set the basic environment", func() {
					Expect(container.Env).To(Equal([]corev1.EnvVar{
						{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
					}))
				})

				It("should set the default volume mounts", func() {
					Expect(container.VolumeMounts).To(Equal([]corev1.VolumeMount{
						{Name: "logs", MountPath: "/var/log/fdb-trace-logs"},
						{Name: "dynamic-conf", MountPath: "/var/dynamic-conf"},
					}))
				})

				It("should set default resource limits", func() {
					Expect(*container.Resources.Limits.Cpu()).To(Equal(resource.MustParse("1")))
					Expect(*container.Resources.Limits.Memory()).To(Equal(resource.MustParse("1Gi")))
					Expect(*container.Resources.Requests.Cpu()).To(Equal(resource.MustParse("1")))
					Expect(*container.Resources.Requests.Memory()).To(Equal(resource.MustParse("1Gi")))
				})
			})

			Describe("the init container", func() {
				var container corev1.Container

				BeforeEach(func() {
					container = deployment.Spec.Template.Spec.InitContainers[0]
				})

				It("should set the container name", func() {
					Expect(container.Name).To(Equal(fdbv1beta2.InitContainerName))
				})

				It("should set the image and command for the container", func() {
					Expect(container.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes-sidecar:%s-1", cluster.Spec.Version)))
					Expect(container.Args).To(Equal([]string{
						"--copy-file",
						"fdb.cluster",
						"--require-not-empty",
						"fdb.cluster",
						"--init-mode",
					}))
				})

				It("should set the default volume mounts", func() {
					Expect(container.VolumeMounts).To(Equal([]corev1.VolumeMount{
						{Name: "config-map", MountPath: "/var/input-files"},
						{Name: "dynamic-conf", MountPath: "/var/output-files"},
					}))
				})
			})
		})

		Context("with a custom secret for the backup credentials", func() {
			BeforeEach(func() {
				backup.Spec.PodTemplateSpec = &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Volumes: []corev1.Volume{
							{Name: "secrets", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "backup-secrets"}}},
						},
						Containers: []corev1.Container{
							{
								Name: fdbv1beta2.MainContainerName,
								Env: []corev1.EnvVar{
									{Name: "FDB_BLOB_CREDENTIALS", Value: "/var/secrets/blob_credentials.json"},
								},
								VolumeMounts: []corev1.VolumeMount{
									{Name: "secrets", MountPath: "/var/secrets"},
								},
							},
						},
					},
				}
				deployment, err = GetBackupDeployment(backup)
				Expect(err).NotTo(HaveOccurred())
				Expect(deployment).NotTo(BeNil())
			})

			It("should customize the volumes", func() {
				Expect(deployment.Spec.Template.Spec.Volumes).To(Equal([]corev1.Volume{
					{Name: "secrets", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "backup-secrets"}}},
					{Name: "logs", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					{Name: "dynamic-conf", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					{
						Name: "config-map",
						VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
							LocalObjectReference: corev1.LocalObjectReference{Name: fmt.Sprintf("%s-config", cluster.Name)},
							Items: []corev1.KeyToPath{
								{Key: ClusterFileKey, Path: "fdb.cluster"},
							},
						}},
					},
				}))
			})

			Describe("the main container", func() {
				var container corev1.Container

				BeforeEach(func() {
					container = deployment.Spec.Template.Spec.Containers[0]
				})

				It("should set the container name", func() {
					Expect(container.Name).To(Equal(fdbv1beta2.MainContainerName))
				})

				It("should customize the environment", func() {
					Expect(container.Env).To(Equal([]corev1.EnvVar{
						{Name: "FDB_BLOB_CREDENTIALS", Value: "/var/secrets/blob_credentials.json"},
						{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
					}))
				})

				It("should customize the volume mounts", func() {
					Expect(container.VolumeMounts).To(Equal([]corev1.VolumeMount{
						{Name: "secrets", MountPath: "/var/secrets"},
						{Name: "logs", MountPath: "/var/log/fdb-trace-logs"},
						{Name: "dynamic-conf", MountPath: "/var/dynamic-conf"},
					}))
				})
			})
		})

		Context("with a custom label", func() {
			BeforeEach(func() {
				backup.Spec.BackupDeploymentMetadata = &metav1.ObjectMeta{
					Labels: map[string]string{
						"fdb-test": "test-value",
					},
				}
				deployment, err = GetBackupDeployment(backup)
				Expect(err).NotTo(HaveOccurred())
				Expect(deployment).NotTo(BeNil())
			})

			It("should add the labels", func() {
				Expect(deployment.ObjectMeta.Labels).To(Equal(map[string]string{
					"foundationdb.org/backup-for": "",
					"fdb-test":                    "test-value",
				}))
			})
		})

		Context("with a nil agent count", func() {
			BeforeEach(func() {
				backup.Spec.AgentCount = nil
				deployment, err = GetBackupDeployment(backup)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should have 2 replicas", func() {
				Expect(deployment).NotTo(BeNil())
				Expect(*deployment.Spec.Replicas).To(Equal(int32(2)))
			})
		})

		Context("with an agent count of 0", func() {
			BeforeEach(func() {
				agentCount := 0
				backup.Spec.AgentCount = &agentCount
				deployment, err = GetBackupDeployment(backup)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should be nil", func() {
				Expect(deployment).To(BeNil())
			})
		})

		Context("with a custom TLS CA file", func() {
			BeforeEach(func() {
				backup.Spec.PodTemplateSpec = &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Name: fdbv1beta2.MainContainerName,
							Env: []corev1.EnvVar{{
								Name:  "FDB_TLS_CA_FILE",
								Value: "/tmp/ca.pem",
							}},
						}},
					},
				}
				deployment, err = GetBackupDeployment(backup)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should respect the custom CA", func() {
				container := deployment.Spec.Template.Spec.Containers[0]
				Expect(container.Env).To(Equal([]corev1.EnvVar{
					{Name: "FDB_TLS_CA_FILE", Value: "/tmp/ca.pem"},
					{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
				}))
			})
		})

		Context("with customParameters", func() {
			BeforeEach(func() {
				backup.Spec.CustomParameters = []fdbv1beta2.FoundationDBCustomParameter{"customParameter=1337"}
				deployment, err = GetBackupDeployment(backup)
				Expect(err).NotTo(HaveOccurred())
				Expect(deployment).NotTo(BeNil())
			})

			It("should set custom parameter in the args", func() {
				Expect(deployment.ObjectMeta.Name).To(Equal("operator-test-1-backup-agents"))
				Expect(len(deployment.ObjectMeta.OwnerReferences)).To(Equal(1))
				Expect(deployment.ObjectMeta.OwnerReferences[0].UID).To(Equal(cluster.ObjectMeta.UID))
				Expect(deployment.ObjectMeta.Labels).To(Equal(map[string]string{
					"foundationdb.org/backup-for": string(cluster.ObjectMeta.UID),
				}))
				Expect(deployment.ObjectMeta.Annotations).To(Equal(map[string]string{
					"foundationdb.org/last-applied-spec": "ab69c51bd609ab9bb94605b25fbfe49e25090040d66bb5af2b3c52e6a6ed4330",
				}))

				Expect(deployment.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--customParameter=1337"))
			})
		})

		When("defining an image config", func() {
			BeforeEach(func() {
				backup.Spec.MainContainer.ImageConfigs = []fdbv1beta2.ImageConfig{
					{BaseImage: "foundationdb/foundationdb", Tag: "dev"},
				}
				backup.Spec.SidecarContainer.ImageConfigs = []fdbv1beta2.ImageConfig{
					{BaseImage: "foundationdb/foundationdb-kubernetes-sidecar", Tag: "dev-1"},
				}
				deployment, err = GetBackupDeployment(backup)
				Expect(err).NotTo(HaveOccurred())
				Expect(deployment).NotTo(BeNil())
			})

			It("should set the image based on the image configs", func() {
				Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("foundationdb/foundationdb:dev"))
				Expect(deployment.Spec.Template.Spec.InitContainers[0].Image).To(Equal("foundationdb/foundationdb-kubernetes-sidecar:dev-1"))
			})
		})

		When("AllowTagOverride is set to true", func() {
			BeforeEach(func() {
				backup.Spec.AllowTagOverride = pointer.Bool(true)
				templateSpec := corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:  fdbv1beta2.MainContainerName,
								Image: "test:main",
							},
						},
						InitContainers: []corev1.Container{
							{
								Name:  fdbv1beta2.InitContainerName,
								Image: "test:sidecar",
							},
						},
					},
				}

				backup.Spec.PodTemplateSpec = &templateSpec
				deployment, err = GetBackupDeployment(backup)
				Expect(err).NotTo(HaveOccurred())
				Expect(deployment).NotTo(BeNil())
			})

			It("should set the image without validating the tag", func() {
				Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal("test:main"))
				Expect(deployment.Spec.Template.Spec.InitContainers[0].Image).To(Equal("test:sidecar"))
			})
		})

		When("using the unified image", func() {
			BeforeEach(func() {
				imageType := fdbv1beta2.ImageTypeUnified
				backup.Spec.ImageType = &imageType
				deployment, err = GetBackupDeployment(backup)
				Expect(err).NotTo(HaveOccurred())
				Expect(deployment).NotTo(BeNil())
			})

			It("should use the unified image as main and init container", func() {
				Expect(deployment.Spec.Template.Spec.InitContainers).To(HaveLen(1))
				Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))

				Expect(deployment.Spec.Template.Spec.InitContainers[0].Image).To(HavePrefix("foundationdb/foundationdb-kubernetes"))
				Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(HavePrefix("foundationdb/foundationdb-kubernetes"))
			})
		})
	})

	Context("Get image for container", func() {
		type testCase struct {
			imageName     string
			imageConfigs  []fdbv1beta2.ImageConfig
			versionString string
		}

		DescribeTable("should return the correct image",
			func(input testCase, allowTagOverride bool, expected string) {
				image, _ := GetImage(input.imageName, input.imageConfigs, input.versionString, allowTagOverride)
				Expect(image).To(Equal(expected))
			},
			Entry("only defaults used",
				testCase{
					imageName: "",
					imageConfigs: []fdbv1beta2.ImageConfig{
						{BaseImage: "test/test"},
					},
					versionString: "6.3.10",
				}, false, "test/test:6.3.10"),
			Entry("imageName is set",
				testCase{
					imageName: "test/curImage",
					imageConfigs: []fdbv1beta2.ImageConfig{
						{BaseImage: "test/test"},
					},
					versionString: "6.3.10",
				}, false, "test/curImage:6.3.10"),
			Entry("image tag is set but not allowOverride",
				testCase{
					imageName: "test/curImage:dev",
					imageConfigs: []fdbv1beta2.ImageConfig{
						{BaseImage: "test/test"},
					},
					versionString: "6.3.10",
				}, false, ""),
			Entry("image tag is set and allowOverride",
				testCase{
					imageName: "test/curImage:dev",
					imageConfigs: []fdbv1beta2.ImageConfig{
						{BaseImage: "test/test"},
					},
					versionString: "6.3.10",
				}, true, "test/curImage:dev"),
		)

		Context("Configure the sidecar image", func() {
			type testCase struct {
				container      *corev1.Container
				initMode       bool
				processGroupID fdbv1beta2.ProcessGroupID
				hasError       bool
			}

			DescribeTable("should return the correct image",
				func(input testCase, expected string) {
					err = configureSidecarContainerForCluster(cluster, "operator-test-storage-1", input.container, input.initMode, input.processGroupID, "6.2.21")
					if input.hasError {
						Expect(err).To(HaveOccurred())
					} else {
						Expect(err).NotTo(HaveOccurred())
					}

					Expect(input.container.Image).To(Equal(expected))
				},
				Entry("only defaults used",
					testCase{
						container:      &corev1.Container{},
						initMode:       false,
						processGroupID: "123",
						hasError:       false,
					}, "foundationdb/foundationdb-kubernetes-sidecar:6.2.21-1"),
				Entry("set a tag in the image without override",
					testCase{
						container: &corev1.Container{
							Image: "myimage:mytag",
						},
						initMode:       false,
						processGroupID: "123",
						hasError:       true,
					}, "myimage:mytag"),
			)
		})
	})

	Describe("GetStorageServersPerPodForPod", func() {
		Context("when env var is set with 1", func() {
			It("should return 1", func() {
				pod := &corev1.Pod{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Env: []corev1.EnvVar{
								{
									Name:  "STORAGE_SERVERS_PER_POD",
									Value: "1",
								},
							},
						}},
					},
				}

				storageServersPerPod, err := GetServersPerPodForPod(pod, fdbv1beta2.ProcessClassStorage)
				Expect(err).NotTo(HaveOccurred())
				Expect(storageServersPerPod).To(Equal(1))
			})
		})

		Context("when env var is set with 2", func() {
			It("should return 2", func() {
				pod := &corev1.Pod{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Env: []corev1.EnvVar{
								{
									Name:  "STORAGE_SERVERS_PER_POD",
									Value: "2",
								},
							},
						}},
					},
				}

				storageServersPerPod, err := GetServersPerPodForPod(pod, fdbv1beta2.ProcessClassStorage)
				Expect(err).NotTo(HaveOccurred())
				Expect(storageServersPerPod).To(Equal(2))
			})
		})

		Context("when env var is unset", func() {
			It("should return 1", func() {
				pod := &corev1.Pod{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{
							Env: []corev1.EnvVar{},
						}},
					},
				}

				storageServersPerPod, err := GetServersPerPodForPod(pod, fdbv1beta2.ProcessClassStorage)
				Expect(err).NotTo(HaveOccurred())
				Expect(storageServersPerPod).To(Equal(1))
			})
		})

		Context("when pod is nil", func() {
			It("should return 1", func() {
				storageServersPerPod, err := GetServersPerPodForPod(nil, fdbv1beta2.ProcessClassStorage)
				Expect(err).NotTo(HaveOccurred())
				Expect(storageServersPerPod).To(Equal(1))
			})
		})

		Context("when Pod doesn't contain a Spec", func() {
			It("should return 1", func() {
				pod := &corev1.Pod{}

				storageServersPerPod, err := GetServersPerPodForPod(pod, fdbv1beta2.ProcessClassStorage)
				Expect(err).NotTo(HaveOccurred())
				Expect(storageServersPerPod).To(Equal(1))
			})
		})

		Context("when Pod doesn't contain a containers", func() {
			It("should return 1", func() {
				pod := &corev1.Pod{
					Spec: corev1.PodSpec{},
				}

				storageServersPerPod, err := GetServersPerPodForPod(pod, fdbv1beta2.ProcessClassStorage)
				Expect(err).NotTo(HaveOccurred())
				Expect(storageServersPerPod).To(Equal(1))
			})
		})
	})

	Describe("GetPodDNSName", func() {
		It("builds the DNS name based on the cluster spec", func() {
			cluster.Spec.Routing.DNSDomain = pointer.String("cluster.example")
			Expect(GetPodDNSName(cluster, "operator-test-storage-1")).To(Equal("operator-test-storage-1.operator-test-1.my-ns.svc.cluster.example"))
		})
	})

	DescribeTable("getting the process group ID from the Pod name", func(cluster *fdbv1beta2.FoundationDBCluster, podName string, expected fdbv1beta2.ProcessGroupID) {
		Expect(GetProcessGroupIDFromPodName(cluster, podName)).To(Equal(expected))
	},
		Entry("cluster without prefix", &fdbv1beta2.FoundationDBCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
		}, "test-storage-1", fdbv1beta2.ProcessGroupID("storage-1")),
		Entry("cluster with prefix", &fdbv1beta2.FoundationDBCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: fdbv1beta2.FoundationDBClusterSpec{
				ProcessGroupIDPrefix: "prefix",
			},
		}, "test-storage-1", fdbv1beta2.ProcessGroupID("prefix-storage-1")))

	Describe("ContainsPod", func() {
		var pod1, pod2 *corev1.Pod

		BeforeEach(func() {
			pod1, err = GetPod(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
			Expect(err).NotTo(HaveOccurred())
			pod2, err = GetPod(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 2))
			Expect(err).NotTo(HaveOccurred())
			pod2.Labels[fdbv1beta2.FDBClusterLabel] = "incorrect-cluster-name"
		})

		It("should check whether the Pod belongs to the cluster", func() {
			Expect(ContainsPod(cluster, *pod1)).To(BeTrue())
			Expect(ContainsPod(cluster, *pod2)).To(BeFalse())
		})
	})
})
