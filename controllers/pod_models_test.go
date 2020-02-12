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

package controllers

import (
	"context"
	"fmt"
	appsv1beta1 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var _ = Describe("pod_models", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var err error

	BeforeEach(func() {
		cluster = createDefaultCluster()
	})

	Describe("GetPod", func() {
		var pod *corev1.Pod
		Context("with a basic storage instance", func() {
			BeforeEach(func() {
				pod, err = GetPod(context.TODO(), cluster, "storage", 1, k8sClient)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should contain the instance's metadata", func() {
				Expect(pod.Namespace).To(Equal("my-ns"))
				Expect(pod.Name).To(Equal(fmt.Sprintf("%s-storage-1", cluster.Name)))
				Expect(pod.ObjectMeta.Labels).To(Equal(map[string]string{
					"fdb-cluster-name":     cluster.Name,
					"fdb-process-class":    "storage",
					"fdb-full-instance-id": "storage-1",
					"fdb-instance-id":      "storage-1",
				}))
			})

			It("should contain the instance's pod spec", func() {
				spec, err := GetPodSpec(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
				Expect(pod.Spec).To(Equal(*spec))
			})
		})

		Context("with a cluster controller instance", func() {
			BeforeEach(func() {
				pod, err = GetPod(context.TODO(), cluster, "cluster_controller", 1, k8sClient)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should contain the instance's metadata", func() {
				Expect(pod.Name).To(Equal(fmt.Sprintf("%s-cluster-controller-1", cluster.Name)))
				Expect(pod.ObjectMeta.Labels).To(Equal(map[string]string{
					"fdb-cluster-name":     cluster.Name,
					"fdb-process-class":    "cluster_controller",
					"fdb-full-instance-id": "cluster_controller-1",
					"fdb-instance-id":      "cluster_controller-1",
				}))
			})

			It("should contain the instance's pod spec", func() {
				spec, err := GetPodSpec(cluster, "cluster_controller", 1)
				Expect(err).NotTo(HaveOccurred())
				Expect(pod.Spec).To(Equal(*spec))
			})
		})

		Context("with an instance ID prefix", func() {
			BeforeEach(func() {
				cluster.Spec.InstanceIDPrefix = "dc1"
				pod, err = GetPod(context.TODO(), cluster, "storage", 1, k8sClient)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should not include the prefix in the instance name", func() {
				Expect(pod.Name).To(Equal(fmt.Sprintf("%s-storage-1", cluster.Name)))
			})

			It("should contain the prefix in the instance labels labels", func() {
				Expect(pod.ObjectMeta.Labels).To(Equal(map[string]string{
					"fdb-cluster-name":     cluster.Name,
					"fdb-process-class":    "storage",
					"fdb-full-instance-id": "dc1-storage-1",
					"fdb-instance-id":      "dc1-storage-1",
				}))
			})
		})

		Context("with custom annotations", func() {
			BeforeEach(func() {
				cluster.Spec.PodTemplate = &corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"fdb-annotation": "value1",
						},
					},
				}
				pod, err = GetPod(context.TODO(), cluster, "storage", 1, k8sClient)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should add the annotations to the metadata", func() {
				hash, err := GetPodSpecHash(cluster, pod.Labels["fdb-process-class"], 1, &pod.Spec)
				Expect(err).NotTo(HaveOccurred())
				Expect(pod.ObjectMeta.Annotations).To(Equal(map[string]string{
					"fdb-annotation": "value1",
					"org.foundationdb/last-applied-pod-spec-hash": hash,
				}))
			})
		})

		Context("with custom labels", func() {
			BeforeEach(func() {
				cluster.Spec.PodTemplate = &corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"fdb-label": "value2",
						},
					},
				}
				pod, err = GetPod(context.TODO(), cluster, "storage", 1, k8sClient)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should add the labels to the metadata", func() {
				Expect(pod.ObjectMeta.Labels).To(Equal(map[string]string{
					"fdb-cluster-name":     cluster.Name,
					"fdb-process-class":    "storage",
					"fdb-instance-id":      "storage-1",
					"fdb-full-instance-id": "storage-1",
					"fdb-label":            "value2",
				}))
			})
		})

		Context("with custom labels with a deprecated field", func() {
			BeforeEach(func() {
				cluster.Spec.PodLabels = map[string]string{
					"fdb-label": "value3",
				}

				pod, err = GetPod(context.TODO(), cluster, "storage", 1, k8sClient)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should put the labels on the pod", func() {
				Expect(pod.ObjectMeta.Labels).To(Equal(map[string]string{
					"fdb-cluster-name":     cluster.Name,
					"fdb-process-class":    "storage",
					"fdb-instance-id":      "storage-1",
					"fdb-full-instance-id": "storage-1",
					"fdb-label":            "value3",
				}))

			})
		})
	})

	Describe("GetPodSpec", func() {
		var spec *corev1.PodSpec
		Context("with a basic storage instance", func() {
			BeforeEach(func() {
				spec, err = GetPodSpec(cluster, "storage", 1)
			})

			It("should have the built-in init container", func() {
				Expect(len(spec.InitContainers)).To(Equal(1))
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal("foundationdb-kubernetes-init"))
				Expect(initContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes-sidecar:%s-1", cluster.Spec.Version)))
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
					"6.2.15",
					"--init-mode",
				}))
				Expect(initContainer.Env).To(Equal([]corev1.EnvVar{
					corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
				}))
				Expect(initContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
					corev1.VolumeMount{Name: "config-map", MountPath: "/var/input-files"},
					corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/output-files"},
				}))
				Expect(initContainer.ReadinessProbe).To(BeNil())
			})

			It("should have two containers", func() {
				Expect(len(spec.Containers)).To(Equal(2))
			})

			It("should have the main foundationdb container", func() {
				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal("foundationdb"))
				Expect(mainContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb:%s", cluster.Spec.Version)))
				Expect(mainContainer.Command).To(Equal([]string{"sh", "-c"}))
				Expect(mainContainer.Args).To(Equal([]string{
					"fdbmonitor --conffile /var/dynamic-conf/fdbmonitor.conf" +
						" --lockfile /var/fdb/fdbmonitor.lockfile",
				}))

				Expect(mainContainer.Env).To(Equal([]corev1.EnvVar{
					corev1.EnvVar{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
					corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/dynamic-conf/ca.pem"},
				}))

				Expect(*mainContainer.Resources.Limits.Cpu()).To(Equal(resource.MustParse("1")))
				Expect(*mainContainer.Resources.Limits.Memory()).To(Equal(resource.MustParse("1Gi")))
				Expect(*mainContainer.Resources.Requests.Cpu()).To(Equal(resource.MustParse("1")))
				Expect(*mainContainer.Resources.Requests.Memory()).To(Equal(resource.MustParse("1Gi")))

				Expect(len(mainContainer.VolumeMounts)).To(Equal(3))

				Expect(mainContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
					corev1.VolumeMount{Name: "data", MountPath: "/var/fdb/data"},
					corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/dynamic-conf"},
					corev1.VolumeMount{Name: "fdb-trace-logs", MountPath: "/var/log/fdb-trace-logs"},
				}))
			})

			It("should have the sidecar container", func() {
				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal("foundationdb-kubernetes-sidecar"))
				Expect(sidecarContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes-sidecar:%s-1", cluster.Spec.Version)))
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
					"6.2.15",
				}))
				Expect(sidecarContainer.Env).To(Equal([]corev1.EnvVar{
					corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
					corev1.EnvVar{Name: "FDB_TLS_VERIFY_PEERS", Value: ""},
					corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/input-files/ca.pem"},
				}))
				Expect(sidecarContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
					corev1.VolumeMount{Name: "config-map", MountPath: "/var/input-files"},
					corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/output-files"},
				}))
				Expect(sidecarContainer.ReadinessProbe).To(Equal(&corev1.Probe{
					Handler: corev1.Handler{
						TCPSocket: &corev1.TCPSocketAction{
							Port: intstr.IntOrString{IntVal: 8080},
						},
					},
				}))
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
							corev1.KeyToPath{Key: "fdbmonitor-conf-storage", Path: "fdbmonitor.conf"},
							corev1.KeyToPath{Key: "cluster-file", Path: "fdb.cluster"},
							corev1.KeyToPath{Key: "ca-file", Path: "ca.pem"},
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

		Context("with custom resources", func() {
			BeforeEach(func() {
				cluster.Spec.PodTemplate = &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							corev1.Container{
								Name: "foundationdb",
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
				}
				spec, err = GetPodSpec(cluster, "storage", 1)
			})

			It("should set the resources on the main container", func() {
				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal("foundationdb"))
				Expect(*mainContainer.Resources.Limits.Cpu()).To(Equal(resource.MustParse("4")))
				Expect(*mainContainer.Resources.Limits.Memory()).To(Equal(resource.MustParse("16Gi")))
				Expect(*mainContainer.Resources.Requests.Cpu()).To(Equal(resource.MustParse("2")))
				Expect(*mainContainer.Resources.Requests.Memory()).To(Equal(resource.MustParse("8Gi")))
			})
		})

		Context("with custom resources with deprecated field", func() {
			BeforeEach(func() {
				cluster.Spec.Resources = &corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						"cpu":    resource.MustParse("2"),
						"memory": resource.MustParse("8Gi"),
					},
					Limits: corev1.ResourceList{
						"cpu":    resource.MustParse("4"),
						"memory": resource.MustParse("16Gi"),
					},
				}
				spec, err = GetPodSpec(cluster, "storage", 1)
			})

			It("should set the resources on the main container", func() {
				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal("foundationdb"))
				Expect(*mainContainer.Resources.Limits.Cpu()).To(Equal(resource.MustParse("4")))
				Expect(*mainContainer.Resources.Limits.Memory()).To(Equal(resource.MustParse("16Gi")))
				Expect(*mainContainer.Resources.Requests.Cpu()).To(Equal(resource.MustParse("2")))
				Expect(*mainContainer.Resources.Requests.Memory()).To(Equal(resource.MustParse("8Gi")))
			})
		})

		Context("with no volume", func() {
			BeforeEach(func() {
				cluster.Spec.VolumeSize = "0"
				spec, err = GetPodSpec(cluster, "storage", 1)
			})

			It("should use an EmptyDir volume", func() {
				Expect(spec.Volumes[0]).To(Equal(corev1.Volume{
					Name:         "data",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				}))
			})
		})

		Context("with a host-based fault domain", func() {
			BeforeEach(func() {
				cluster.Spec.FaultDomain = appsv1beta1.FoundationDBClusterFaultDomain{}
				spec, err = GetPodSpec(cluster, "storage", 1)
			})

			It("should set the fault domain information in the sidecar environment", func() {
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal("foundationdb-kubernetes-init"))
				Expect(initContainer.Env).To(Equal([]corev1.EnvVar{
					corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
					}},
					corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
					}},
					corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
				}))

			})

			It("should set the pod affinity", func() {
				Expect(spec.Affinity).To(Equal(&corev1.Affinity{
					PodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
							corev1.WeightedPodAffinityTerm{
								Weight: 1,
								PodAffinityTerm: corev1.PodAffinityTerm{
									TopologyKey: "kubernetes.io/hostname",
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"fdb-cluster-name":  cluster.Name,
											"fdb-process-class": "storage",
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

				cluster.Spec.FaultDomain = appsv1beta1.FoundationDBClusterFaultDomain{
					Key:       "rack",
					ValueFrom: "$RACK",
				}
				spec, err = GetPodSpec(cluster, "storage", 1)
			})

			It("should set the fault domain information in the sidecar environment", func() {
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal("foundationdb-kubernetes-init"))
				Expect(initContainer.Env).To(Equal([]corev1.EnvVar{
					corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
					}},
					corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
				}))

			})

			It("should set the pod affinity", func() {
				Expect(spec.Affinity).To(Equal(&corev1.Affinity{
					PodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
							corev1.WeightedPodAffinityTerm{
								Weight: 1,
								PodAffinityTerm: corev1.PodAffinityTerm{
									TopologyKey: "rack",
									LabelSelector: &metav1.LabelSelector{
										MatchLabels: map[string]string{
											"fdb-cluster-name":  cluster.Name,
											"fdb-process-class": "storage",
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
				cluster.Spec.FaultDomain = appsv1beta1.FoundationDBClusterFaultDomain{
					Key:   "foundationdb.org/kubernetes-cluster",
					Value: "kc2",
				}
				spec, err = GetPodSpec(cluster, "storage", 1)
			})

			It("should set the fault domain information in the sidecar environment", func() {
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal("foundationdb-kubernetes-init"))
				Expect(initContainer.Env).To(Equal([]corev1.EnvVar{
					corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
					}},
					corev1.EnvVar{Name: "FDB_ZONE_ID", Value: "kc2"},
					corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
				}))
			})

			It("should leave the pod affinity empty", func() {
				Expect(spec.Affinity).To(BeNil())
			})
		})

		Context("with custom containers", func() {
			BeforeEach(func() {
				cluster.Spec.PodTemplate = &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						InitContainers: []corev1.Container{corev1.Container{
							Name:    "test-container",
							Image:   "foundationdb/" + cluster.Name,
							Command: []string{"echo", "test1"},
						}},
						Containers: []corev1.Container{corev1.Container{
							Name:    "test-container",
							Image:   "foundationdb/" + cluster.Name,
							Command: []string{"echo", "test2"},
						}},
					},
				}
				spec, err = GetPodSpec(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should have both init containers", func() {
				Expect(len(spec.InitContainers)).To(Equal(2))

				testInitContainer := spec.InitContainers[0]
				Expect(testInitContainer.Name).To(Equal("test-container"))
				Expect(testInitContainer.Image).To(Equal("foundationdb/" + cluster.Name))
				Expect(testInitContainer.Command).To(Equal([]string{"echo", "test1"}))

				initContainer := spec.InitContainers[1]
				Expect(initContainer.Name).To(Equal("foundationdb-kubernetes-init"))
				Expect(initContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes-sidecar:%s-1", cluster.Spec.Version)))
			})

			It("should have all three containers", func() {
				Expect(len(spec.Containers)).To(Equal(3))

				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal("foundationdb"))
				Expect(mainContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb:%s", cluster.Spec.Version)))

				testContainer := spec.Containers[1]
				Expect(testContainer.Name).To(Equal("test-container"))
				Expect(testContainer.Image).To(Equal("foundationdb/" + cluster.Name))
				Expect(testContainer.Command).To(Equal([]string{"echo", "test2"}))

				sidecarContainer := spec.Containers[2]
				Expect(sidecarContainer.Name).To(Equal("foundationdb-kubernetes-sidecar"))
				Expect(sidecarContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes-sidecar:%s-1", cluster.Spec.Version)))
			})
		})

		Context("with custom containers with deprecated field", func() {
			BeforeEach(func() {
				cluster.Spec.InitContainers = []corev1.Container{corev1.Container{
					Name:    "test-container",
					Image:   "foundationdb/" + cluster.Name,
					Command: []string{"echo", "test1"},
				}}
				cluster.Spec.Containers = []corev1.Container{corev1.Container{
					Name:    "test-container",
					Image:   "foundationdb/" + cluster.Name,
					Command: []string{"echo", "test2"},
				}}
				spec, err = GetPodSpec(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should have both init containers", func() {
				Expect(len(spec.InitContainers)).To(Equal(2))
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal("foundationdb-kubernetes-init"))
				Expect(initContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes-sidecar:%s-1", cluster.Spec.Version)))

				testInitContainer := spec.InitContainers[1]
				Expect(testInitContainer.Name).To(Equal("test-container"))
				Expect(testInitContainer.Image).To(Equal("foundationdb/" + cluster.Name))
				Expect(testInitContainer.Command).To(Equal([]string{"echo", "test1"}))
			})

			It("should have all three containers", func() {
				Expect(len(spec.Containers)).To(Equal(3))

				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal("foundationdb"))
				Expect(mainContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb:%s", cluster.Spec.Version)))

				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal("foundationdb-kubernetes-sidecar"))
				Expect(sidecarContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes-sidecar:%s-1", cluster.Spec.Version)))

				testContainer := spec.Containers[2]
				Expect(testContainer.Name).To(Equal("test-container"))
				Expect(testContainer.Image).To(Equal("foundationdb/" + cluster.Name))
				Expect(testContainer.Command).To(Equal([]string{"echo", "test2"}))
			})
		})

		Context("with custom environment", func() {
			BeforeEach(func() {
				cluster.Spec.PodTemplate = &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							corev1.Container{
								Name: "foundationdb",
								Env: []corev1.EnvVar{
									corev1.EnvVar{Name: "FDB_TLS_CERTIFICATE_FILE", Value: "/var/secrets/cert.pem"},
									corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/secrets/cert.pem"},
									corev1.EnvVar{Name: "FDB_TLS_KEY_FILE", Value: "/var/secrets/cert.pem"},
								},
							},
							corev1.Container{
								Name: "foundationdb-kubernetes-sidecar",
								Env: []corev1.EnvVar{
									corev1.EnvVar{Name: "ADDITIONAL_ENV_FILE", Value: "/var/custom-env"},
								},
							},
						},
						InitContainers: []corev1.Container{
							corev1.Container{
								Name: "foundationdb-kubernetes-init",
								Env: []corev1.EnvVar{
									corev1.EnvVar{Name: "ADDITIONAL_ENV_FILE", Value: "/var/custom-env-init"},
								},
							},
						},
					},
				}

				spec, err = GetPodSpec(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the environment variables on the containers", func() {
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal("foundationdb-kubernetes-init"))
				Expect(initContainer.Env).To(Equal([]corev1.EnvVar{
					corev1.EnvVar{Name: "ADDITIONAL_ENV_FILE", Value: "/var/custom-env-init"},
					corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
				}))

				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal("foundationdb"))
				Expect(mainContainer.Env).To(Equal([]corev1.EnvVar{
					corev1.EnvVar{Name: "FDB_TLS_CERTIFICATE_FILE", Value: "/var/secrets/cert.pem"},
					corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/secrets/cert.pem"},
					corev1.EnvVar{Name: "FDB_TLS_KEY_FILE", Value: "/var/secrets/cert.pem"},
					corev1.EnvVar{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
				}))

				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal("foundationdb-kubernetes-sidecar"))
				Expect(sidecarContainer.Env).To(Equal([]corev1.EnvVar{
					corev1.EnvVar{Name: "ADDITIONAL_ENV_FILE", Value: "/var/custom-env"},
					corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
					corev1.EnvVar{Name: "FDB_TLS_VERIFY_PEERS", Value: ""},
					corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/input-files/ca.pem"},
				}))
			})
		})

		Context("with custom environment with deprecated fields", func() {
			BeforeEach(func() {
				cluster.Spec.MainContainer.Env = []corev1.EnvVar{
					corev1.EnvVar{Name: "FDB_TLS_CERTIFICATE_FILE", Value: "/var/secrets/cert.pem"},
					corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/secrets/cert.pem"},
					corev1.EnvVar{Name: "FDB_TLS_KEY_FILE", Value: "/var/secrets/cert.pem"},
				}
				cluster.Spec.SidecarContainer.Env = []corev1.EnvVar{
					corev1.EnvVar{Name: "ADDITIONAL_ENV_FILE", Value: "/var/custom-env"},
				}

				spec, err = GetPodSpec(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the environment on the containers", func() {
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal("foundationdb-kubernetes-init"))
				Expect(initContainer.Env).To(Equal([]corev1.EnvVar{
					corev1.EnvVar{Name: "ADDITIONAL_ENV_FILE", Value: "/var/custom-env"},
					corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
				}))

				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal("foundationdb"))
				Expect(mainContainer.Env).To(Equal([]corev1.EnvVar{
					corev1.EnvVar{Name: "FDB_TLS_CERTIFICATE_FILE", Value: "/var/secrets/cert.pem"},
					corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/secrets/cert.pem"},
					corev1.EnvVar{Name: "FDB_TLS_KEY_FILE", Value: "/var/secrets/cert.pem"},
					corev1.EnvVar{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
				}))

				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal("foundationdb-kubernetes-sidecar"))
				Expect(sidecarContainer.Env).To(Equal([]corev1.EnvVar{
					corev1.EnvVar{Name: "ADDITIONAL_ENV_FILE", Value: "/var/custom-env"},
					corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
					corev1.EnvVar{Name: "FDB_TLS_VERIFY_PEERS", Value: ""},
					corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/input-files/ca.pem"},
				}))
			})
		})

		Context("with TLS for the sidecar", func() {
			BeforeEach(func() {
				cluster.Spec.SidecarContainer.EnableTLS = true
				cluster.Spec.SidecarContainer.Env = []corev1.EnvVar{
					corev1.EnvVar{Name: "FDB_TLS_CERTIFICATE_FILE", Value: "/var/secrets/cert.pem"},
					corev1.EnvVar{Name: "FDB_TLS_KEY_FILE", Value: "/var/secrets/cert.pem"},
				}

				cluster.Spec.SidecarContainer.PeerVerificationRules = "S.CN=foundationdb.org"

				spec, err = GetPodSpec(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("passes the TLS environment to the init container", func() {
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal("foundationdb-kubernetes-init"))
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
					"6.2.15",
					"--init-mode",
				}))
				Expect(initContainer.Env).To(Equal([]corev1.EnvVar{
					corev1.EnvVar{Name: "FDB_TLS_CERTIFICATE_FILE", Value: "/var/secrets/cert.pem"},
					corev1.EnvVar{Name: "FDB_TLS_KEY_FILE", Value: "/var/secrets/cert.pem"},
					corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
				}))
			})

			It("passes the TLS environment to the sidecar", func() {
				Expect(len(spec.Containers)).To(Equal(2))

				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal("foundationdb"))
				Expect(mainContainer.Env).To(Equal([]corev1.EnvVar{
					corev1.EnvVar{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
					corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/dynamic-conf/ca.pem"},
				}))

				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal("foundationdb-kubernetes-sidecar"))
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
					"--tls",
				}))
				Expect(sidecarContainer.Env).To(Equal([]corev1.EnvVar{
					corev1.EnvVar{Name: "FDB_TLS_CERTIFICATE_FILE", Value: "/var/secrets/cert.pem"},
					corev1.EnvVar{Name: "FDB_TLS_KEY_FILE", Value: "/var/secrets/cert.pem"},
					corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
					corev1.EnvVar{Name: "FDB_TLS_VERIFY_PEERS", Value: "S.CN=foundationdb.org"},
					corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/input-files/ca.pem"},
				}))
			})
		})

		Context("with custom volumes", func() {
			BeforeEach(func() {
				cluster.Spec.PodTemplate = &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Volumes: []corev1.Volume{corev1.Volume{
							Name: "test-secrets",
							VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
								SecretName: "test-secrets",
							}},
						}},
						Containers: []corev1.Container{
							corev1.Container{
								Name: "foundationdb",
								VolumeMounts: []corev1.VolumeMount{corev1.VolumeMount{
									Name:      "test-secrets",
									MountPath: "/var/secrets",
								}},
							},
						},
					},
				}
				spec, err = GetPodSpec(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("adds volumes to the container", func() {
				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal("foundationdb"))
				Expect(mainContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
					corev1.VolumeMount{Name: "test-secrets", MountPath: "/var/secrets"},
					corev1.VolumeMount{Name: "data", MountPath: "/var/fdb/data"},
					corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/dynamic-conf"},
					corev1.VolumeMount{Name: "fdb-trace-logs", MountPath: "/var/log/fdb-trace-logs"},
				}))
			})

			It("does not add volumes to the sidecar container", func() {
				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal("foundationdb-kubernetes-sidecar"))

				Expect(sidecarContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
					corev1.VolumeMount{Name: "config-map", MountPath: "/var/input-files"},
					corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/output-files"},
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
							corev1.KeyToPath{Key: "fdbmonitor-conf-storage", Path: "fdbmonitor.conf"},
							corev1.KeyToPath{Key: "cluster-file", Path: "fdb.cluster"},
							corev1.KeyToPath{Key: "ca-file", Path: "ca.pem"},
						},
					}},
				}))
				Expect(spec.Volumes[4]).To(Equal(corev1.Volume{
					Name:         "fdb-trace-logs",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				}))
			})
		})

		Context("with custom volumes with deprecated fields", func() {
			BeforeEach(func() {
				cluster.Spec.Volumes = []corev1.Volume{corev1.Volume{
					Name: "test-secrets",
					VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
						SecretName: "test-secrets",
					}},
				}}
				cluster.Spec.MainContainer.VolumeMounts = []corev1.VolumeMount{corev1.VolumeMount{
					Name:      "test-secrets",
					MountPath: "/var/secrets",
				}}
				spec, err = GetPodSpec(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("adds volumes to the container", func() {
				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal("foundationdb"))
				Expect(mainContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
					corev1.VolumeMount{Name: "data", MountPath: "/var/fdb/data"},
					corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/dynamic-conf"},
					corev1.VolumeMount{Name: "fdb-trace-logs", MountPath: "/var/log/fdb-trace-logs"},
					corev1.VolumeMount{Name: "test-secrets", MountPath: "/var/secrets"},
				}))
			})

			It("does not add volumes to the sidecar container", func() {
				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal("foundationdb-kubernetes-sidecar"))

				Expect(sidecarContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
					corev1.VolumeMount{Name: "config-map", MountPath: "/var/input-files"},
					corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/output-files"},
				}))

			})

			It("adds volumes to the pod spec", func() {
				Expect(len(spec.Volumes)).To(Equal(5))
				Expect(spec.Volumes[0]).To(Equal(corev1.Volume{
					VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: fmt.Sprintf("%s-storage-1-data", cluster.Name),
					}},
					Name: "data",
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
							corev1.KeyToPath{Key: "fdbmonitor-conf-storage", Path: "fdbmonitor.conf"},
							corev1.KeyToPath{Key: "cluster-file", Path: "fdb.cluster"},
							corev1.KeyToPath{Key: "ca-file", Path: "ca.pem"},
						},
					}},
				}))
				Expect(spec.Volumes[3]).To(Equal(corev1.Volume{
					Name:         "fdb-trace-logs",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
				}))
				Expect(spec.Volumes[4]).To(Equal(corev1.Volume{
					Name: "test-secrets",
					VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
						SecretName: "test-secrets",
					}},
				}))
			})
		})

		Context("with custom sidecar version", func() {
			BeforeEach(func() {
				cluster.Spec.SidecarVersions = map[string]int{
					cluster.Spec.Version: 2,
					"6.1.0":              3,
				}
				spec, err = GetPodSpec(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("sets the images on the containers", func() {
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal("foundationdb-kubernetes-init"))
				Expect(initContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes-sidecar:%s-2", cluster.Spec.Version)))

				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal("foundationdb"))
				Expect(mainContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb:%s", cluster.Spec.Version)))

				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal("foundationdb-kubernetes-sidecar"))
				Expect(sidecarContainer.Image).To(Equal(initContainer.Image))
			})
		})

		Context("with custom sidecar version with deprecated field", func() {
			BeforeEach(func() {
				cluster.Spec.SidecarVersion = 2
				spec, err = GetPodSpec(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("sets the images on the containers", func() {
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal("foundationdb-kubernetes-init"))
				Expect(initContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes-sidecar:%s-2", cluster.Spec.Version)))

				mainContainer := spec.Containers[0]
				Expect(mainContainer.Name).To(Equal("foundationdb"))
				Expect(mainContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb:%s", cluster.Spec.Version)))

				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal("foundationdb-kubernetes-sidecar"))
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

				cluster.Spec.PodTemplate = &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						SecurityContext: podSecurityContext,
						Containers: []corev1.Container{
							corev1.Container{
								Name:            "foundationdb",
								SecurityContext: mainSecurityContext,
							},
							corev1.Container{
								Name:            "foundationdb-kubernetes-sidecar",
								SecurityContext: sidecarSecurityContext,
							},
						},
						InitContainers: []corev1.Container{
							corev1.Container{
								Name:            "foundationdb-kubernetes-init",
								SecurityContext: sidecarSecurityContext,
							},
						},
					},
				}

				spec, err = GetPodSpec(cluster, "storage", 1)
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

		Context("with a custom security context with deprecated fields", func() {
			BeforeEach(func() {
				cluster.Spec.PodSecurityContext = &corev1.PodSecurityContext{FSGroup: new(int64)}
				*cluster.Spec.PodSecurityContext.FSGroup = 5000

				cluster.Spec.MainContainer.SecurityContext = &corev1.SecurityContext{RunAsGroup: new(int64), RunAsUser: new(int64)}
				*cluster.Spec.MainContainer.SecurityContext.RunAsGroup = 3000
				*cluster.Spec.MainContainer.SecurityContext.RunAsUser = 4000
				cluster.Spec.SidecarContainer.SecurityContext = &corev1.SecurityContext{RunAsGroup: new(int64), RunAsUser: new(int64)}
				*cluster.Spec.SidecarContainer.SecurityContext.RunAsGroup = 1000
				*cluster.Spec.SidecarContainer.SecurityContext.RunAsUser = 2000

				spec, err = GetPodSpec(cluster, "storage", 1)
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

		Context("with an instance ID prefix", func() {
			BeforeEach(func() {
				cluster.Spec.InstanceIDPrefix = "dc1"
				spec, err = GetPodSpec(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should put the prefix in the instance ID", func() {
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Env).To(Equal([]corev1.EnvVar{
					corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: "dc1-storage-1"},
				}))
			})
		})

		Context("with serevice account token disabled", func() {
			BeforeEach(func() {
				var automount = false
				cluster.Spec.AutomountServiceAccountToken = &automount
				spec, err = GetPodSpec(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("disables the token in the spec", func() {
				Expect(*spec.AutomountServiceAccountToken).To(BeFalse())
			})
		})

		Context("with command line arguments for the sidecar", func() {
			BeforeEach(func() {
				cluster.Spec.Version = Versions.WithCommandLineVariablesForSidecar.String()
				cluster.Spec.SidecarVariables = []string{"FAULT_DOMAIN", "ZONE"}
				spec, err = GetPodSpec(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should add the arguments to the init container", func() {
				Expect(len(spec.InitContainers)).To(Equal(1))
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal("foundationdb-kubernetes-init"))
				Expect(initContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes-sidecar:%s-1", cluster.Spec.Version)))
				Expect(initContainer.Args).To(Equal([]string{
					"--copy-file", "fdb.cluster", "--copy-file", "ca.pem",
					"--input-monitor-conf", "fdbmonitor.conf",
					"--copy-binary", "fdbserver", "--copy-binary", "fdbcli",
					"--main-container-version", cluster.Spec.Version,
					"--substitute-variable", "FAULT_DOMAIN", "--substitute-variable", "ZONE",
					"--init-mode",
				}))
				Expect(initContainer.Env).To(Equal([]corev1.EnvVar{
					corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
				}))
			})

			It("should add the arguments to the sidecar", func() {
				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal("foundationdb-kubernetes-sidecar"))
				Expect(sidecarContainer.Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb-kubernetes-sidecar:%s-1", cluster.Spec.Version)))

				Expect(sidecarContainer.Args).To(Equal([]string{
					"--copy-file", "fdb.cluster", "--copy-file", "ca.pem",
					"--input-monitor-conf", "fdbmonitor.conf",
					"--copy-binary", "fdbserver", "--copy-binary", "fdbcli",
					"--main-container-version", cluster.Spec.Version,
					"--substitute-variable", "FAULT_DOMAIN", "--substitute-variable", "ZONE",
				}))

				Expect(sidecarContainer.Env).To(Equal([]corev1.EnvVar{
					corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
					corev1.EnvVar{Name: "FDB_TLS_VERIFY_PEERS", Value: ""},
					corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/input-files/ca.pem"},
				}))
			})

			It("should not include the sidecar conf in the config map", func() {
				Expect(spec.Volumes[2]).To(Equal(corev1.Volume{
					Name: "config-map",
					VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: fmt.Sprintf("%s-config", cluster.Name)},
						Items: []corev1.KeyToPath{
							corev1.KeyToPath{Key: "fdbmonitor-conf-storage", Path: "fdbmonitor.conf"},
							corev1.KeyToPath{Key: "cluster-file", Path: "fdb.cluster"},
							corev1.KeyToPath{Key: "ca-file", Path: "ca.pem"},
						},
					}},
				}))
			})
		})

		Context("with environment variables for the sidecar", func() {
			BeforeEach(func() {
				cluster.Spec.Version = Versions.WithEnvironmentVariablesForSidecar.String()
				cluster.Spec.SidecarVariables = []string{"FAULT_DOMAIN", "ZONE"}
				spec, err = GetPodSpec(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("adds the environment variables to the init container", func() {
				initContainer := spec.InitContainers[0]
				Expect(initContainer.Name).To(Equal("foundationdb-kubernetes-init"))
				Expect(initContainer.Args).To(Equal([]string{}))
				Expect(initContainer.Env).To(Equal([]corev1.EnvVar{
					corev1.EnvVar{Name: "COPY_ONCE", Value: "1"},
					corev1.EnvVar{Name: "SIDECAR_CONF_DIR", Value: "/var/input-files"},
					corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
				}))
			})

			It("adds the environment variables to the sidecar container", func() {
				sidecarContainer := spec.Containers[1]
				Expect(sidecarContainer.Name).To(Equal("foundationdb-kubernetes-sidecar"))
				Expect(sidecarContainer.Args).To(BeNil())
				Expect(sidecarContainer.Env).To(Equal([]corev1.EnvVar{
					corev1.EnvVar{Name: "SIDECAR_CONF_DIR", Value: "/var/input-files"},
					corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
					}},
					corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
					}},
					corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
					corev1.EnvVar{Name: "FDB_TLS_VERIFY_PEERS", Value: ""},
					corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/input-files/ca.pem"},
				}))

				Expect(len(spec.Volumes)).To(Equal(4))

				Expect(spec.Volumes[2]).To(Equal(corev1.Volume{
					Name: "config-map",
					VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: fmt.Sprintf("%s-config", cluster.Name)},
						Items: []corev1.KeyToPath{
							corev1.KeyToPath{Key: "fdbmonitor-conf-storage", Path: "fdbmonitor.conf"},
							corev1.KeyToPath{Key: "cluster-file", Path: "fdb.cluster"},
							corev1.KeyToPath{Key: "ca-file", Path: "ca.pem"},
							corev1.KeyToPath{Key: "sidecar-conf", Path: "config.json"},
						},
					}},
				}))
			})
		})
	})

	Describe("GetPvc", func() {
		var pvc *corev1.PersistentVolumeClaim

		Context("with a basic storage instance", func() {
			BeforeEach(func() {
				pvc, err = GetPvc(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the metadata on the PVC", func() {
				Expect(pvc.Namespace).To(Equal("my-ns"))
				Expect(pvc.Name).To(Equal(fmt.Sprintf("%s-storage-1-data", cluster.Name)))
				Expect(pvc.ObjectMeta.Labels).To(Equal(map[string]string{
					"fdb-cluster-name":     cluster.Name,
					"fdb-process-class":    "storage",
					"fdb-instance-id":      "storage-1",
					"fdb-full-instance-id": "storage-1",
				}))
			})

			It("should set the spec on the PVC", func() {
				Expect(pvc.Spec).To(Equal(corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"storage": resource.MustParse("128G"),
						},
					},
				}))
			})
		})

		Context("with a custom storage size", func() {
			BeforeEach(func() {
				cluster.Spec.VolumeClaim = &corev1.PersistentVolumeClaim{
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								"storage": resource.MustParse("64G"),
							},
						},
					},
				}
				pvc, err = GetPvc(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the storage size on the resources", func() {
				Expect(pvc.Spec.Resources).To(Equal(corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						"storage": resource.MustParse("64G"),
					},
				}))
			})
		})

		Context("with a custom storage size with a deprecated field", func() {
			BeforeEach(func() {
				cluster.Spec.VolumeSize = "64G"
				pvc, err = GetPvc(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the storage size on the resources", func() {
				Expect(pvc.Spec.Resources).To(Equal(corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						"storage": resource.MustParse("64G"),
					},
				}))
			})
		})

		Context("with custom metadata", func() {
			BeforeEach(func() {
				cluster.Spec.VolumeClaim = &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"fdb-annotation": "value1",
						},
						Labels: map[string]string{
							"fdb-label": "value2",
						},
					},
				}
				pvc, err = GetPvc(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the metadata on the PVC", func() {
				Expect(pvc.Namespace).To(Equal("my-ns"))
				Expect(pvc.Name).To(Equal(fmt.Sprintf("%s-storage-1-data", cluster.Name)))
				Expect(pvc.ObjectMeta.Annotations).To(Equal(map[string]string{
					"fdb-annotation": "value1",
				}))
				Expect(pvc.ObjectMeta.Labels).To(Equal(map[string]string{
					"fdb-cluster-name":     cluster.Name,
					"fdb-process-class":    "storage",
					"fdb-instance-id":      "storage-1",
					"fdb-full-instance-id": "storage-1",
					"fdb-label":            "value2",
				}))
			})
		})

		Context("with a value size of 0", func() {
			BeforeEach(func() {
				cluster.Spec.VolumeClaim = &corev1.PersistentVolumeClaim{
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								"storage": resource.MustParse("0"),
							},
						},
					},
				}
				pvc, err = GetPvc(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should return a nil PVC", func() {
				Expect(pvc).To(BeNil())
			})
		})

		Context("with a value size of 0 with deprecated field", func() {
			BeforeEach(func() {
				cluster.Spec.VolumeSize = "0"
				pvc, err = GetPvc(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should return a nil PVC", func() {
				Expect(pvc).To(BeNil())
			})
		})

		Context("with a custom storage class", func() {
			var class string
			BeforeEach(func() {
				class = "local"
				cluster.Spec.VolumeClaim = &corev1.PersistentVolumeClaim{
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: &class,
					},
				}
				pvc, err = GetPvc(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the storage class on the PVC", func() {
				Expect(pvc.Spec.StorageClassName).To(Equal(&class))
			})
		})

		Context("with a custom storage class with a deprecated field", func() {
			var class string
			BeforeEach(func() {
				class = "local"
				cluster.Spec.StorageClass = &class
				pvc, err = GetPvc(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the storage class on the PVC", func() {
				Expect(pvc.Spec.StorageClassName).To(Equal(&class))
			})
		})

		Context("for a stateless instance", func() {
			BeforeEach(func() {
				pvc, err = GetPvc(cluster, "stateless", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("returns a nil PVC", func() {
				Expect(pvc).To(BeNil())
			})
		})

		Context("with an instance ID prefix", func() {
			BeforeEach(func() {
				cluster.Spec.InstanceIDPrefix = "dc1"
				pvc, err = GetPvc(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include the prefix in the instance IDs", func() {
				Expect(pvc.Name).To(Equal(fmt.Sprintf("%s-storage-1-data", cluster.Name)))
				Expect(pvc.ObjectMeta.Labels).To(Equal(map[string]string{
					"fdb-cluster-name":     cluster.Name,
					"fdb-process-class":    "storage",
					"fdb-instance-id":      "dc1-storage-1",
					"fdb-full-instance-id": "dc1-storage-1",
				}))
			})
		})

		Context("with custom name in the suffix", func() {
			BeforeEach(func() {
				cluster.Spec.VolumeClaim = &corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{Name: "pvc1"},
				}
				pvc, err = GetPvc(cluster, "storage", 1)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should include the prefix in the instance IDs with custom suffix", func() {
				Expect(pvc.Name).To(Equal(fmt.Sprintf("%s-storage-1-pvc1", cluster.Name)))
			})
		})

	})
})
