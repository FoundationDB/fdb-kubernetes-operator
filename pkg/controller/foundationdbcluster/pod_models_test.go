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

package foundationdbcluster

import (
	"context"
	"fmt"
	"testing"

	appsv1beta1 "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func TestGetPodWithStorageInstance(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	pod, err := GetPod(context.TODO(), cluster, "storage", 1, c)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(pod.Namespace).To(gomega.Equal("default"))
	g.Expect(pod.Name).To(gomega.Equal(fmt.Sprintf("%s-storage-1", cluster.Name)))
	g.Expect(pod.ObjectMeta.Labels).To(gomega.Equal(map[string]string{
		"fdb-cluster-name":     cluster.Name,
		"fdb-process-class":    "storage",
		"fdb-full-instance-id": "storage-1",
		"fdb-instance-id":      "storage-1",
		"fdb-label":            "value2",
	}))

	spec, err := GetPodSpec(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(pod.Spec).To(gomega.Equal(*spec))
}

func TestGetPodWithClusterControllerInstance(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	pod, err := GetPod(context.TODO(), cluster, "cluster_controller", 1, c)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(pod.Namespace).To(gomega.Equal("default"))
	g.Expect(pod.Name).To(gomega.Equal(fmt.Sprintf("%s-cluster-controller-1", cluster.Name)))
	g.Expect(pod.ObjectMeta.Labels).To(gomega.Equal(map[string]string{
		"fdb-cluster-name":     cluster.Name,
		"fdb-process-class":    "cluster_controller",
		"fdb-full-instance-id": "cluster_controller-1",
		"fdb-instance-id":      "cluster_controller-1",
		"fdb-label":            "value2",
	}))

	spec, err := GetPodSpec(cluster, "cluster_controller", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(pod.Spec).To(gomega.Equal(*spec))
}

func TestGetPodWithInstanceIDPrefix(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	cluster.Spec.InstanceIDPrefix = "dc1"
	pod, err := GetPod(context.TODO(), cluster, "storage", 1, c)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(pod.Name).To(gomega.Equal(fmt.Sprintf("%s-storage-1", cluster.Name)))
	g.Expect(pod.ObjectMeta.Labels).To(gomega.Equal(map[string]string{
		"fdb-cluster-name":     cluster.Name,
		"fdb-process-class":    "storage",
		"fdb-instance-id":      "dc1-storage-1",
		"fdb-full-instance-id": "dc1-storage-1",
		"fdb-label":            "value2",
	}))

	volumes := pod.Spec.Volumes
	g.Expect(len(volumes)).To(gomega.Equal(4))
	g.Expect(volumes[0].PersistentVolumeClaim.ClaimName).To(gomega.Equal(fmt.Sprintf("%s-data", pod.Name)))
}

func TestGetPodSpecForStorageInstance(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.FaultDomain = appsv1beta1.FoundationDBClusterFaultDomain{}
	spec, err := GetPodSpec(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(len(spec.InitContainers)).To(gomega.Equal(1))
	initContainer := spec.InitContainers[0]
	g.Expect(initContainer.Name).To(gomega.Equal("foundationdb-kubernetes-init"))
	g.Expect(initContainer.Image).To(gomega.Equal("foundationdb/foundationdb-kubernetes-sidecar:6.1.8-1"))
	g.Expect(initContainer.Args).To(gomega.Equal([]string{}))
	g.Expect(initContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "COPY_ONCE", Value: "1"},
		corev1.EnvVar{Name: "SIDECAR_CONF_DIR", Value: "/var/input-files"},
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
	g.Expect(initContainer.VolumeMounts).To(gomega.Equal([]corev1.VolumeMount{
		corev1.VolumeMount{Name: "config-map", MountPath: "/var/input-files"},
		corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/output-files"},
	}))
	g.Expect(initContainer.ReadinessProbe).To(gomega.BeNil())

	g.Expect(len(spec.Containers)).To(gomega.Equal(2))

	mainContainer := spec.Containers[0]
	g.Expect(mainContainer.Name).To(gomega.Equal("foundationdb"))
	g.Expect(mainContainer.Image).To(gomega.Equal("foundationdb/foundationdb:6.1.8"))
	g.Expect(mainContainer.Command).To(gomega.Equal([]string{"sh", "-c"}))
	g.Expect(mainContainer.Args).To(gomega.Equal([]string{
		"fdbmonitor --conffile /var/dynamic-conf/fdbmonitor.conf" +
			" --lockfile /var/fdb/fdbmonitor.lockfile",
	}))

	g.Expect(mainContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
		corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/dynamic-conf/ca.pem"},
	}))

	g.Expect(*mainContainer.Resources.Limits.Cpu()).To(gomega.Equal(resource.MustParse("1")))
	g.Expect(*mainContainer.Resources.Limits.Memory()).To(gomega.Equal(resource.MustParse("1Gi")))
	g.Expect(*mainContainer.Resources.Requests.Cpu()).To(gomega.Equal(resource.MustParse("1")))
	g.Expect(*mainContainer.Resources.Requests.Memory()).To(gomega.Equal(resource.MustParse("1Gi")))

	g.Expect(len(mainContainer.VolumeMounts)).To(gomega.Equal(3))

	g.Expect(mainContainer.VolumeMounts).To(gomega.Equal([]corev1.VolumeMount{
		corev1.VolumeMount{Name: "data", MountPath: "/var/fdb/data"},
		corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/dynamic-conf"},
		corev1.VolumeMount{Name: "fdb-trace-logs", MountPath: "/var/log/fdb-trace-logs"},
	}))

	sidecarContainer := spec.Containers[1]
	g.Expect(sidecarContainer.Name).To(gomega.Equal("foundationdb-kubernetes-sidecar"))
	g.Expect(sidecarContainer.Image).To(gomega.Equal(initContainer.Image))
	g.Expect(sidecarContainer.Args).To(gomega.Equal([]string{}))
	g.Expect(sidecarContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "SIDECAR_CONF_DIR", Value: "/var/input-files"},
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
		corev1.EnvVar{Name: "FDB_TLS_VERIFY_PEERS", Value: ""},
		corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/input-files/ca.pem"},
	}))
	g.Expect(sidecarContainer.VolumeMounts).To(gomega.Equal(initContainer.VolumeMounts))
	g.Expect(sidecarContainer.ReadinessProbe).To(gomega.Equal(&corev1.Probe{
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.IntOrString{IntVal: 8080},
			},
		},
	}))

	g.Expect(len(spec.Volumes)).To(gomega.Equal(4))
	g.Expect(spec.Volumes[0]).To(gomega.Equal(corev1.Volume{
		Name: "data",
		VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: fmt.Sprintf("%s-storage-1-data", cluster.Name),
		}},
	}))
	g.Expect(spec.Volumes[1]).To(gomega.Equal(corev1.Volume{
		Name:         "dynamic-conf",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}))
	g.Expect(spec.Volumes[2]).To(gomega.Equal(corev1.Volume{
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
	g.Expect(spec.Volumes[3]).To(gomega.Equal(corev1.Volume{
		Name:         "fdb-trace-logs",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}))

	g.Expect(spec.Affinity).To(gomega.Equal(&corev1.Affinity{
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

	g.Expect(spec.AutomountServiceAccountToken).To(gomega.BeNil())
}

func TestGetPodSpecWithCustomResources(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
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
	spec := GetPodSpec(cluster, "storage", "1")
	g.Expect(len(spec.Containers)).To(gomega.Equal(2))

	mainContainer := spec.Containers[0]
	g.Expect(mainContainer.Name).To(gomega.Equal("foundationdb"))
	g.Expect(*mainContainer.Resources.Limits.Cpu()).To(gomega.Equal(resource.MustParse("4")))
	g.Expect(*mainContainer.Resources.Limits.Memory()).To(gomega.Equal(resource.MustParse("16Gi")))
	g.Expect(*mainContainer.Resources.Requests.Cpu()).To(gomega.Equal(resource.MustParse("2")))
	g.Expect(*mainContainer.Resources.Requests.Memory()).To(gomega.Equal(resource.MustParse("8Gi")))
}

func TestGetPodSpecWithCustomResourcesWithDeprecatedField(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
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
	spec := GetPodSpec(cluster, "storage", "1")
	g.Expect(len(spec.Containers)).To(gomega.Equal(2))

	mainContainer := spec.Containers[0]
	g.Expect(mainContainer.Name).To(gomega.Equal("foundationdb"))
	g.Expect(*mainContainer.Resources.Limits.Cpu()).To(gomega.Equal(resource.MustParse("4")))
	g.Expect(*mainContainer.Resources.Limits.Memory()).To(gomega.Equal(resource.MustParse("16Gi")))
	g.Expect(*mainContainer.Resources.Requests.Cpu()).To(gomega.Equal(resource.MustParse("2")))
	g.Expect(*mainContainer.Resources.Requests.Memory()).To(gomega.Equal(resource.MustParse("8Gi")))
}
func TestGetPodSpecForStorageInstanceWithNoVolume(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.VolumeSize = "0"
	spec, err := GetPodSpec(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(spec.Volumes[0]).To(gomega.Equal(corev1.Volume{
		Name:         "data",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}))
}

func TestGetPodSpecWithFaultDomainDisabled(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.FaultDomain = appsv1beta1.FoundationDBClusterFaultDomain{
		Key: "foundationdb.org/none",
	}
	spec, err := GetPodSpec(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	initContainer := spec.InitContainers[0]
	g.Expect(initContainer.Name).To(gomega.Equal("foundationdb-kubernetes-init"))
	g.Expect(initContainer.Env).To(gomega.Equal([]corev1.EnvVar{
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

	g.Expect(spec.Affinity).To(gomega.BeNil())
}

func TestGetPodSpecWithAlternativeFaultDomainVariable(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.FaultDomain = appsv1beta1.FoundationDBClusterFaultDomain{
		Key:       "rack",
		ValueFrom: "$RACK",
	}
	spec, err := GetPodSpec(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	initContainer := spec.InitContainers[0]
	g.Expect(initContainer.Name).To(gomega.Equal("foundationdb-kubernetes-init"))
	g.Expect(initContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "COPY_ONCE", Value: "1"},
		corev1.EnvVar{Name: "SIDECAR_CONF_DIR", Value: "/var/input-files"},
		corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
		}},
		corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
		}},
		corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
	}))

	g.Expect(spec.Affinity).To(gomega.Equal(&corev1.Affinity{
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
}

func TestGetPodSpecWithCrossKubernetesReplication(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.FaultDomain = appsv1beta1.FoundationDBClusterFaultDomain{
		Key:   "foundationdb.org/kubernetes-cluster",
		Value: "kc2",
	}
	spec, err := GetPodSpec(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	initContainer := spec.InitContainers[0]
	g.Expect(initContainer.Name).To(gomega.Equal("foundationdb-kubernetes-init"))
	g.Expect(initContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "COPY_ONCE", Value: "1"},
		corev1.EnvVar{Name: "SIDECAR_CONF_DIR", Value: "/var/input-files"},
		corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
		}},
		corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
		}},
		corev1.EnvVar{Name: "FDB_ZONE_ID", Value: "kc2"},
		corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: "storage-1"},
	}))

	g.Expect(spec.Affinity).To(gomega.BeNil())
}

func TestGetPodSpecWithCustomContainers(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
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
	spec := GetPodSpec(cluster, "storage", "1")

	g.Expect(len(spec.InitContainers)).To(gomega.Equal(2))

	testInitContainer := spec.InitContainers[0]
	g.Expect(testInitContainer.Name).To(gomega.Equal("test-container"))
	g.Expect(testInitContainer.Image).To(gomega.Equal("foundationdb/" + cluster.Name))
	g.Expect(testInitContainer.Command).To(gomega.Equal([]string{"echo", "test1"}))

	initContainer := spec.InitContainers[1]
	g.Expect(initContainer.Name).To(gomega.Equal("foundationdb-kubernetes-init"))
	g.Expect(initContainer.Image).To(gomega.Equal("foundationdb/foundationdb-kubernetes-sidecar:6.1.8-1"))

	g.Expect(len(spec.Containers)).To(gomega.Equal(3))

	mainContainer := spec.Containers[0]
	g.Expect(mainContainer.Name).To(gomega.Equal("foundationdb"))
	g.Expect(mainContainer.Image).To(gomega.Equal("foundationdb/foundationdb:6.1.8"))

	testContainer := spec.Containers[1]
	g.Expect(testContainer.Name).To(gomega.Equal("test-container"))
	g.Expect(testContainer.Image).To(gomega.Equal("foundationdb/" + cluster.Name))
	g.Expect(testContainer.Command).To(gomega.Equal([]string{"echo", "test2"}))

	sidecarContainer := spec.Containers[2]
	g.Expect(sidecarContainer.Name).To(gomega.Equal("foundationdb-kubernetes-sidecar"))
	g.Expect(sidecarContainer.Image).To(gomega.Equal(initContainer.Image))
}

func TestGetPodSpecWithCustomContainersWithDeprecatedField(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
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
	spec, err := GetPodSpec(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(len(spec.InitContainers)).To(gomega.Equal(2))
	initContainer := spec.InitContainers[0]
	g.Expect(initContainer.Name).To(gomega.Equal("foundationdb-kubernetes-init"))
	g.Expect(initContainer.Image).To(gomega.Equal("foundationdb/foundationdb-kubernetes-sidecar:6.1.8-1"))

	testInitContainer := spec.InitContainers[1]
	g.Expect(testInitContainer.Name).To(gomega.Equal("test-container"))
	g.Expect(testInitContainer.Image).To(gomega.Equal("foundationdb/" + cluster.Name))
	g.Expect(testInitContainer.Command).To(gomega.Equal([]string{"echo", "test1"}))

	g.Expect(len(spec.Containers)).To(gomega.Equal(3))

	mainContainer := spec.Containers[0]
	g.Expect(mainContainer.Name).To(gomega.Equal("foundationdb"))
	g.Expect(mainContainer.Image).To(gomega.Equal("foundationdb/foundationdb:6.1.8"))

	sidecarContainer := spec.Containers[1]
	g.Expect(sidecarContainer.Name).To(gomega.Equal("foundationdb-kubernetes-sidecar"))
	g.Expect(sidecarContainer.Image).To(gomega.Equal(initContainer.Image))

	testContainer := spec.Containers[2]
	g.Expect(testContainer.Name).To(gomega.Equal("test-container"))
	g.Expect(testContainer.Image).To(gomega.Equal("foundationdb/" + cluster.Name))
	g.Expect(testContainer.Command).To(gomega.Equal([]string{"echo", "test2"}))
}

func TestGetPodSpecWithCustomEnvironment(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
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

	spec := GetPodSpec(cluster, "storage", "1")

	g.Expect(len(spec.InitContainers)).To(gomega.Equal(1))
	initContainer := spec.InitContainers[0]
	g.Expect(initContainer.Name).To(gomega.Equal("foundationdb-kubernetes-init"))
	g.Expect(initContainer.Image).To(gomega.Equal("foundationdb/foundationdb-kubernetes-sidecar:6.1.8-1"))
	g.Expect(initContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "ADDITIONAL_ENV_FILE", Value: "/var/custom-env-init"},
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
		corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: "1"},
	}))

	g.Expect(len(spec.Containers)).To(gomega.Equal(2))

	mainContainer := spec.Containers[0]
	g.Expect(mainContainer.Name).To(gomega.Equal("foundationdb"))
	g.Expect(mainContainer.Image).To(gomega.Equal("foundationdb/foundationdb:6.1.8"))
	g.Expect(mainContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "FDB_TLS_CERTIFICATE_FILE", Value: "/var/secrets/cert.pem"},
		corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/secrets/cert.pem"},
		corev1.EnvVar{Name: "FDB_TLS_KEY_FILE", Value: "/var/secrets/cert.pem"},
		corev1.EnvVar{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
	}))

	sidecarContainer := spec.Containers[1]
	g.Expect(sidecarContainer.Name).To(gomega.Equal("foundationdb-kubernetes-sidecar"))
	g.Expect(sidecarContainer.Image).To(gomega.Equal(initContainer.Image))
	g.Expect(sidecarContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "ADDITIONAL_ENV_FILE", Value: "/var/custom-env"},
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
		corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: "1"},
		corev1.EnvVar{Name: "FDB_TLS_VERIFY_PEERS", Value: ""},
		corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/input-files/ca.pem"},
	}))
}

func TestGetPodSpecWithCustomEnvironmentWithDeprecatedFields(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.MainContainer.Env = []corev1.EnvVar{
		corev1.EnvVar{Name: "FDB_TLS_CERTIFICATE_FILE", Value: "/var/secrets/cert.pem"},
		corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/secrets/cert.pem"},
		corev1.EnvVar{Name: "FDB_TLS_KEY_FILE", Value: "/var/secrets/cert.pem"},
	}
	cluster.Spec.SidecarContainer.Env = []corev1.EnvVar{
		corev1.EnvVar{Name: "ADDITIONAL_ENV_FILE", Value: "/var/custom-env"},
	}

	spec, err := GetPodSpec(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(len(spec.InitContainers)).To(gomega.Equal(1))
	initContainer := spec.InitContainers[0]
	g.Expect(initContainer.Name).To(gomega.Equal("foundationdb-kubernetes-init"))
	g.Expect(initContainer.Image).To(gomega.Equal("foundationdb/foundationdb-kubernetes-sidecar:6.1.8-1"))
	g.Expect(initContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "ADDITIONAL_ENV_FILE", Value: "/var/custom-env"},
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

	g.Expect(len(spec.Containers)).To(gomega.Equal(2))

	mainContainer := spec.Containers[0]
	g.Expect(mainContainer.Name).To(gomega.Equal("foundationdb"))
	g.Expect(mainContainer.Image).To(gomega.Equal("foundationdb/foundationdb:6.1.8"))
	g.Expect(mainContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "FDB_TLS_CERTIFICATE_FILE", Value: "/var/secrets/cert.pem"},
		corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/secrets/cert.pem"},
		corev1.EnvVar{Name: "FDB_TLS_KEY_FILE", Value: "/var/secrets/cert.pem"},
		corev1.EnvVar{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
	}))

	sidecarContainer := spec.Containers[1]
	g.Expect(sidecarContainer.Name).To(gomega.Equal("foundationdb-kubernetes-sidecar"))
	g.Expect(sidecarContainer.Image).To(gomega.Equal(initContainer.Image))
	g.Expect(sidecarContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "ADDITIONAL_ENV_FILE", Value: "/var/custom-env"},
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
}

func TestGetPodSpecWithSidecarTls(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.SidecarContainer.EnableTLS = true
	cluster.Spec.SidecarContainer.Env = []corev1.EnvVar{
		corev1.EnvVar{Name: "FDB_TLS_CERTIFICATE_FILE", Value: "/var/secrets/cert.pem"},
		corev1.EnvVar{Name: "FDB_TLS_KEY_FILE", Value: "/var/secrets/cert.pem"},
	}

	cluster.Spec.SidecarContainer.PeerVerificationRules = "S.CN=foundationdb.org"

	spec, err := GetPodSpec(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(len(spec.InitContainers)).To(gomega.Equal(1))
	initContainer := spec.InitContainers[0]
	g.Expect(initContainer.Name).To(gomega.Equal("foundationdb-kubernetes-init"))
	g.Expect(initContainer.Image).To(gomega.Equal("foundationdb/foundationdb-kubernetes-sidecar:6.1.8-1"))
	g.Expect(initContainer.Args).To(gomega.Equal([]string{}))
	g.Expect(initContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "FDB_TLS_CERTIFICATE_FILE", Value: "/var/secrets/cert.pem"},
		corev1.EnvVar{Name: "FDB_TLS_KEY_FILE", Value: "/var/secrets/cert.pem"},
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

	g.Expect(len(spec.Containers)).To(gomega.Equal(2))

	mainContainer := spec.Containers[0]
	g.Expect(mainContainer.Name).To(gomega.Equal("foundationdb"))
	g.Expect(mainContainer.Image).To(gomega.Equal("foundationdb/foundationdb:6.1.8"))
	g.Expect(mainContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
		corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/dynamic-conf/ca.pem"},
	}))

	sidecarContainer := spec.Containers[1]
	g.Expect(sidecarContainer.Name).To(gomega.Equal("foundationdb-kubernetes-sidecar"))
	g.Expect(sidecarContainer.Image).To(gomega.Equal(initContainer.Image))
	g.Expect(sidecarContainer.Args).To(gomega.Equal([]string{
		"--tls",
	}))
	g.Expect(sidecarContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "FDB_TLS_CERTIFICATE_FILE", Value: "/var/secrets/cert.pem"},
		corev1.EnvVar{Name: "FDB_TLS_KEY_FILE", Value: "/var/secrets/cert.pem"},
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
		corev1.EnvVar{Name: "FDB_TLS_VERIFY_PEERS", Value: "S.CN=foundationdb.org"},
		corev1.EnvVar{Name: "FDB_TLS_CA_FILE", Value: "/var/input-files/ca.pem"},
	}))
}

func TestGetPodSpecWithCustomVolumes(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
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
	spec := GetPodSpec(cluster, "storage", "1")

	g.Expect(len(spec.InitContainers)).To(gomega.Equal(1))
	initContainer := spec.InitContainers[0]
	g.Expect(initContainer.Name).To(gomega.Equal("foundationdb-kubernetes-init"))
	g.Expect(initContainer.Image).To(gomega.Equal("foundationdb/foundationdb-kubernetes-sidecar:6.1.8-1"))
	g.Expect(initContainer.VolumeMounts).To(gomega.Equal([]corev1.VolumeMount{
		corev1.VolumeMount{Name: "config-map", MountPath: "/var/input-files"},
		corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/output-files"},
	}))

	g.Expect(len(spec.Containers)).To(gomega.Equal(2))

	mainContainer := spec.Containers[0]
	g.Expect(mainContainer.Name).To(gomega.Equal("foundationdb"))
	g.Expect(mainContainer.Image).To(gomega.Equal("foundationdb/foundationdb:6.1.8"))
	g.Expect(mainContainer.VolumeMounts).To(gomega.Equal([]corev1.VolumeMount{
		corev1.VolumeMount{Name: "test-secrets", MountPath: "/var/secrets"},
		corev1.VolumeMount{Name: "data", MountPath: "/var/fdb/data"},
		corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/dynamic-conf"},
		corev1.VolumeMount{Name: "fdb-trace-logs", MountPath: "/var/log/fdb-trace-logs"},
	}))

	sidecarContainer := spec.Containers[1]
	g.Expect(sidecarContainer.Name).To(gomega.Equal("foundationdb-kubernetes-sidecar"))
	g.Expect(sidecarContainer.Image).To(gomega.Equal(initContainer.Image))
	g.Expect(sidecarContainer.VolumeMounts).To(gomega.Equal(initContainer.VolumeMounts))
	g.Expect(len(spec.Volumes)).To(gomega.Equal(5))
	g.Expect(spec.Volumes[0]).To(gomega.Equal(corev1.Volume{
		Name: "test-secrets",
		VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
			SecretName: "test-secrets",
		}},
	}))
	g.Expect(spec.Volumes[1]).To(gomega.Equal(corev1.Volume{
		VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: fmt.Sprintf("%s-1-data", cluster.Name),
		}},
		Name: "data",
	}))
	g.Expect(spec.Volumes[2]).To(gomega.Equal(corev1.Volume{
		Name:         "dynamic-conf",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}))
	g.Expect(spec.Volumes[3]).To(gomega.Equal(corev1.Volume{
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
	g.Expect(spec.Volumes[4]).To(gomega.Equal(corev1.Volume{
		Name:         "fdb-trace-logs",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}))
}

func TestGetPodSpecWithCustomVolumesWithDeprecatedField(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
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
	spec, err := GetPodSpec(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(len(spec.InitContainers)).To(gomega.Equal(1))
	initContainer := spec.InitContainers[0]
	g.Expect(initContainer.Name).To(gomega.Equal("foundationdb-kubernetes-init"))
	g.Expect(initContainer.Image).To(gomega.Equal("foundationdb/foundationdb-kubernetes-sidecar:6.1.8-1"))
	g.Expect(initContainer.VolumeMounts).To(gomega.Equal([]corev1.VolumeMount{
		corev1.VolumeMount{Name: "config-map", MountPath: "/var/input-files"},
		corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/output-files"},
	}))

	g.Expect(len(spec.Containers)).To(gomega.Equal(2))

	mainContainer := spec.Containers[0]
	g.Expect(mainContainer.Name).To(gomega.Equal("foundationdb"))
	g.Expect(mainContainer.Image).To(gomega.Equal("foundationdb/foundationdb:6.1.8"))
	g.Expect(mainContainer.VolumeMounts).To(gomega.Equal([]corev1.VolumeMount{
		corev1.VolumeMount{Name: "data", MountPath: "/var/fdb/data"},
		corev1.VolumeMount{Name: "dynamic-conf", MountPath: "/var/dynamic-conf"},
		corev1.VolumeMount{Name: "fdb-trace-logs", MountPath: "/var/log/fdb-trace-logs"},
		corev1.VolumeMount{Name: "test-secrets", MountPath: "/var/secrets"},
	}))

	sidecarContainer := spec.Containers[1]
	g.Expect(sidecarContainer.Name).To(gomega.Equal("foundationdb-kubernetes-sidecar"))
	g.Expect(sidecarContainer.Image).To(gomega.Equal(initContainer.Image))
	g.Expect(sidecarContainer.VolumeMounts).To(gomega.Equal(initContainer.VolumeMounts))
	g.Expect(len(spec.Volumes)).To(gomega.Equal(5))
	g.Expect(spec.Volumes[0]).To(gomega.Equal(corev1.Volume{
		Name: "data",
		VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: fmt.Sprintf("%s-storage-1-data", cluster.Name),
		}},
	}))
	g.Expect(spec.Volumes[1]).To(gomega.Equal(corev1.Volume{
		Name:         "dynamic-conf",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}))
	g.Expect(spec.Volumes[2]).To(gomega.Equal(corev1.Volume{
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
	g.Expect(spec.Volumes[3]).To(gomega.Equal(corev1.Volume{
		Name:         "fdb-trace-logs",
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}))
	g.Expect(spec.Volumes[4]).To(gomega.Equal(corev1.Volume{
		Name: "test-secrets",
		VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
			SecretName: "test-secrets",
		}},
	}))
}

func TestGetPodSpecWithCustomSidecarVersion(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.SidecarVersions = map[string]int{
		cluster.Spec.Version: 2,
		"6.1.0":              3,
	}
	spec, err := GetPodSpec(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(len(spec.InitContainers)).To(gomega.Equal(1))
	initContainer := spec.InitContainers[0]
	g.Expect(initContainer.Name).To(gomega.Equal("foundationdb-kubernetes-init"))
	g.Expect(initContainer.Image).To(gomega.Equal("foundationdb/foundationdb-kubernetes-sidecar:6.1.8-2"))

	g.Expect(len(spec.Containers)).To(gomega.Equal(2))

	mainContainer := spec.Containers[0]
	g.Expect(mainContainer.Name).To(gomega.Equal("foundationdb"))
	g.Expect(mainContainer.Image).To(gomega.Equal("foundationdb/foundationdb:6.1.8"))

	sidecarContainer := spec.Containers[1]
	g.Expect(sidecarContainer.Name).To(gomega.Equal("foundationdb-kubernetes-sidecar"))
	g.Expect(sidecarContainer.Image).To(gomega.Equal(initContainer.Image))
}

func TestGetPodSpecWithCustomSidecarVersionWithDeprecatedField(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.SidecarVersion = 2
	spec, err := GetPodSpec(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(len(spec.InitContainers)).To(gomega.Equal(1))
	initContainer := spec.InitContainers[0]
	g.Expect(initContainer.Name).To(gomega.Equal("foundationdb-kubernetes-init"))
	g.Expect(initContainer.Image).To(gomega.Equal("foundationdb/foundationdb-kubernetes-sidecar:6.1.8-2"))

	g.Expect(len(spec.Containers)).To(gomega.Equal(2))

	mainContainer := spec.Containers[0]
	g.Expect(mainContainer.Name).To(gomega.Equal("foundationdb"))
	g.Expect(mainContainer.Image).To(gomega.Equal("foundationdb/foundationdb:6.1.8"))

	sidecarContainer := spec.Containers[1]
	g.Expect(sidecarContainer.Name).To(gomega.Equal("foundationdb-kubernetes-sidecar"))
	g.Expect(sidecarContainer.Image).To(gomega.Equal(initContainer.Image))
}

func TestGetPodSpecWithSecurityContext(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()

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

	spec := GetPodSpec(cluster, "storage", "1")

	g.Expect(*spec.SecurityContext.FSGroup).To(gomega.Equal(int64(5000)))

	g.Expect(len(spec.InitContainers)).To(gomega.Equal(1))
	initContainer := spec.InitContainers[0]
	g.Expect(*initContainer.SecurityContext.RunAsGroup).To(gomega.Equal(int64(1000)))
	g.Expect(*initContainer.SecurityContext.RunAsUser).To(gomega.Equal(int64(2000)))

	mainContainer := spec.Containers[0]
	g.Expect(*mainContainer.SecurityContext.RunAsGroup).To(gomega.Equal(int64(3000)))
	g.Expect(*mainContainer.SecurityContext.RunAsUser).To(gomega.Equal(int64(4000)))

	sidecarContainer := spec.Containers[1]
	g.Expect(*sidecarContainer.SecurityContext.RunAsGroup).To(gomega.Equal(int64(1000)))
	g.Expect(*sidecarContainer.SecurityContext.RunAsUser).To(gomega.Equal(int64(2000)))
}

func TestGetPodSpecWithSecurityContextWithDeprecatedField(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.PodSecurityContext = &corev1.PodSecurityContext{FSGroup: new(int64)}
	*cluster.Spec.PodSecurityContext.FSGroup = 5000

	cluster.Spec.MainContainer.SecurityContext = &corev1.SecurityContext{RunAsGroup: new(int64), RunAsUser: new(int64)}
	*cluster.Spec.MainContainer.SecurityContext.RunAsGroup = 3000
	*cluster.Spec.MainContainer.SecurityContext.RunAsUser = 4000
	cluster.Spec.SidecarContainer.SecurityContext = &corev1.SecurityContext{RunAsGroup: new(int64), RunAsUser: new(int64)}
	*cluster.Spec.SidecarContainer.SecurityContext.RunAsGroup = 1000
	*cluster.Spec.SidecarContainer.SecurityContext.RunAsUser = 2000

	spec, err := GetPodSpec(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(*spec.SecurityContext.FSGroup).To(gomega.Equal(int64(5000)))

	g.Expect(len(spec.InitContainers)).To(gomega.Equal(1))
	initContainer := spec.InitContainers[0]
	g.Expect(*initContainer.SecurityContext.RunAsGroup).To(gomega.Equal(int64(1000)))
	g.Expect(*initContainer.SecurityContext.RunAsUser).To(gomega.Equal(int64(2000)))

	mainContainer := spec.Containers[0]
	g.Expect(*mainContainer.SecurityContext.RunAsGroup).To(gomega.Equal(int64(3000)))
	g.Expect(*mainContainer.SecurityContext.RunAsUser).To(gomega.Equal(int64(4000)))

	sidecarContainer := spec.Containers[1]
	g.Expect(*sidecarContainer.SecurityContext.RunAsGroup).To(gomega.Equal(int64(1000)))
	g.Expect(*sidecarContainer.SecurityContext.RunAsUser).To(gomega.Equal(int64(2000)))
}

func TestGetPodSpecWithInstanceIDPrefix(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.FaultDomain = appsv1beta1.FoundationDBClusterFaultDomain{}
	cluster.Spec.InstanceIDPrefix = "dc1"
	spec, err := GetPodSpec(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(len(spec.InitContainers)).To(gomega.Equal(1))
	initContainer := spec.InitContainers[0]
	g.Expect(initContainer.Env).To(gomega.Equal([]corev1.EnvVar{
		corev1.EnvVar{Name: "COPY_ONCE", Value: "1"},
		corev1.EnvVar{Name: "SIDECAR_CONF_DIR", Value: "/var/input-files"},
		corev1.EnvVar{Name: "FDB_PUBLIC_IP", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
		}},
		corev1.EnvVar{Name: "FDB_MACHINE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
		}},
		corev1.EnvVar{Name: "FDB_ZONE_ID", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
		}},
		corev1.EnvVar{Name: "FDB_INSTANCE_ID", Value: "dc1-storage-1"},
	}))
}

func TestGetPodSpecWithServiceAccountDisabled(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	var automount = false
	cluster.Spec.AutomountServiceAccountToken = &automount
	spec, err := GetPodSpec(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(*spec.AutomountServiceAccountToken).To(gomega.BeFalse())
}

func TestGetPodSpecWithCommandLinesVariablesForSidecar(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.Version = Versions.WithCommandLineVariablesForSidecar.String()
	cluster.Spec.SidecarVariables = []string{"FAULT_DOMAIN", "ZONE"}
	spec, err := GetPodSpec(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(len(spec.InitContainers)).To(gomega.Equal(1))
	initContainer := spec.InitContainers[0]
	g.Expect(initContainer.Name).To(gomega.Equal("foundationdb-kubernetes-init"))
	g.Expect(initContainer.Image).To(gomega.Equal("foundationdb/foundationdb-kubernetes-sidecar:7.0.0-1"))
	g.Expect(initContainer.Args).To(gomega.Equal([]string{
		"--copy-file", "fdb.cluster", "--copy-file", "ca.pem",
		"--input-monitor-conf", "fdbmonitor.conf",
		"--copy-binary", "fdbserver", "--copy-binary", "fdbcli",
		"--main-container-version", "7.0.0",
		"--substitute-variable", "FAULT_DOMAIN", "--substitute-variable", "ZONE",
		"--init-mode",
	}))
	g.Expect(initContainer.Env).To(gomega.Equal([]corev1.EnvVar{
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

	sidecarContainer := spec.Containers[1]
	g.Expect(sidecarContainer.Name).To(gomega.Equal("foundationdb-kubernetes-sidecar"))
	g.Expect(sidecarContainer.Image).To(gomega.Equal(initContainer.Image))

	g.Expect(sidecarContainer.Args).To(gomega.Equal([]string{
		"--copy-file", "fdb.cluster", "--copy-file", "ca.pem",
		"--input-monitor-conf", "fdbmonitor.conf",
		"--copy-binary", "fdbserver", "--copy-binary", "fdbcli",
		"--main-container-version", "7.0.0",
		"--substitute-variable", "FAULT_DOMAIN", "--substitute-variable", "ZONE",
	}))

	g.Expect(sidecarContainer.Env).To(gomega.Equal([]corev1.EnvVar{
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
	g.Expect(sidecarContainer.VolumeMounts).To(gomega.Equal(initContainer.VolumeMounts))
	g.Expect(sidecarContainer.ReadinessProbe).To(gomega.Equal(&corev1.Probe{
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.IntOrString{IntVal: 8080},
			},
		},
	}))

	g.Expect(len(spec.Volumes)).To(gomega.Equal(4))
	g.Expect(spec.Volumes[2]).To(gomega.Equal(corev1.Volume{
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
}

func TestGetPodSpecWithCommandLinesVariablesForSidecarWithPendingUpgrade(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.RunningVersion = Versions.WithBinariesFromMainContainer.String()
	cluster.Spec.Version = Versions.WithBinariesFromMainContainerNext.String()
	spec, err := GetPodSpec(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(len(spec.InitContainers)).To(gomega.Equal(1))
	initContainer := spec.InitContainers[0]
	g.Expect(initContainer.Name).To(gomega.Equal("foundationdb-kubernetes-init"))
	g.Expect(initContainer.Image).To(gomega.Equal("foundationdb/foundationdb-kubernetes-sidecar:7.0.0-1"))
	g.Expect(initContainer.Args).To(gomega.Equal([]string{
		"--copy-file", "fdb.cluster", "--copy-file", "ca.pem",
		"--input-monitor-conf", "fdbmonitor.conf",
		"--copy-binary", "fdbserver", "--copy-binary", "fdbcli",
		"--main-container-version", "7.0.0",
		"--init-mode",
	}))
	g.Expect(initContainer.Env).To(gomega.Equal([]corev1.EnvVar{
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

	sidecarContainer := spec.Containers[1]
	g.Expect(sidecarContainer.Name).To(gomega.Equal("foundationdb-kubernetes-sidecar"))
	g.Expect(sidecarContainer.Image).To(gomega.Equal(initContainer.Image))

	g.Expect(sidecarContainer.Args).To(gomega.Equal([]string{
		"--copy-file", "fdb.cluster", "--copy-file", "ca.pem",
		"--input-monitor-conf", "fdbmonitor.conf",
		"--copy-binary", "fdbserver", "--copy-binary", "fdbcli",
		"--main-container-version", "7.0.0",
	}))

	g.Expect(sidecarContainer.Env).To(gomega.Equal([]corev1.EnvVar{
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
	g.Expect(sidecarContainer.VolumeMounts).To(gomega.Equal(initContainer.VolumeMounts))
	g.Expect(sidecarContainer.ReadinessProbe).To(gomega.Equal(&corev1.Probe{
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.IntOrString{IntVal: 8080},
			},
		},
	}))

	g.Expect(len(spec.Volumes)).To(gomega.Equal(4))
	g.Expect(spec.Volumes[2]).To(gomega.Equal(corev1.Volume{
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
}

func TestGetPodSpecWithEnvironmentVariablesForSidecar(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := createDefaultCluster()
	cluster.Spec.Version = Versions.WithEnvironmentVariablesForSidecar.String()
	cluster.Spec.SidecarVariables = []string{"FAULT_DOMAIN", "ZONE"}
	spec, err := GetPodSpec(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(len(spec.InitContainers)).To(gomega.Equal(1))
	initContainer := spec.InitContainers[0]
	g.Expect(initContainer.Name).To(gomega.Equal("foundationdb-kubernetes-init"))
	g.Expect(initContainer.Image).To(gomega.Equal("foundationdb/foundationdb-kubernetes-sidecar:6.2.11-1"))
	g.Expect(initContainer.Args).To(gomega.Equal([]string{}))
	g.Expect(initContainer.Env).To(gomega.Equal([]corev1.EnvVar{
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

	g.Expect(len(spec.Containers)).To(gomega.Equal(2))

	sidecarContainer := spec.Containers[1]
	g.Expect(sidecarContainer.Name).To(gomega.Equal("foundationdb-kubernetes-sidecar"))
	g.Expect(sidecarContainer.Image).To(gomega.Equal(initContainer.Image))
	g.Expect(sidecarContainer.Args).To(gomega.Equal([]string{}))
	g.Expect(sidecarContainer.Env).To(gomega.Equal([]corev1.EnvVar{
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

	g.Expect(len(spec.Volumes)).To(gomega.Equal(4))

	g.Expect(spec.Volumes[2]).To(gomega.Equal(corev1.Volume{
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
}

func TestGetPodWithInstanceIDPrefix(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(pod.ObjectMeta.Labels).To(gomega.Equal(map[string]string{
		"fdb-cluster-name":     cluster.Name,
		"fdb-process-class":    "storage",
		"fdb-instance-id":      "1",
		"fdb-full-instance-id": "dc1-1",
	}))

	hash, err := GetPodSpecHash(cluster, pod.Labels["fdb-process-class"], pod.Labels["fdb-instance-id"], &pod.Spec)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(pod.ObjectMeta.Annotations).To(gomega.Equal(map[string]string{
		"org.foundationdb/last-applied-pod-spec-hash": hash,
	}))
}

func TestGetPodWithCustomAnnotations(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	cluster.Spec.PodTemplate = &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"fdb-annotation": "value1",
			},
		},
	}
	pod, err := GetPod(context.TODO(), cluster, "storage", 1, c)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	hash, err := GetPodSpecHash(cluster, pod.Labels["fdb-process-class"], pod.Labels["fdb-instance-id"], &pod.Spec)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(pod.ObjectMeta.Annotations).To(gomega.Equal(map[string]string{
		"fdb-annotation": "value1",
		"org.foundationdb/last-applied-pod-spec-hash": hash,
	}))
}

func TestGetPodWithCustomLabels(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	cluster.Spec.PodTemplate = &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"fdb-label": "value2",
			},
		},
	}
	pod, err := GetPod(context.TODO(), cluster, "storage", 1, c)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(pod.Namespace).To(gomega.Equal("default"))
	g.Expect(pod.Name).To(gomega.Equal(fmt.Sprintf("%s-1", cluster.Name)))
	g.Expect(pod.ObjectMeta.Labels).To(gomega.Equal(map[string]string{
		"fdb-cluster-name":     cluster.Name,
		"fdb-process-class":    "storage",
		"fdb-instance-id":      "1",
		"fdb-full-instance-id": "1",
		"fdb-label":            "value2",
	}))
}

func TestGetPodForWithCustomLabelsWithDeprecatedField(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	cluster.Spec.PodLabels = map[string]string{
		"fdb-label": "value3",
	}

	pod, err := GetPod(context.TODO(), cluster, "storage", 1, c)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(pod.ObjectMeta.Labels).To(gomega.Equal(map[string]string{
		"fdb-cluster-name":     cluster.Name,
		"fdb-process-class":    "storage",
		"fdb-instance-id":      "1",
		"fdb-full-instance-id": "1",
		"fdb-label":            "value3",
	}))
}

func TestGetPvcForStorageInstance(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	pvc, err := GetPvc(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(pvc.Namespace).To(gomega.Equal("default"))
	g.Expect(pvc.Name).To(gomega.Equal(fmt.Sprintf("%s-storage-1-data", cluster.Name)))
	g.Expect(pvc.ObjectMeta.Labels).To(gomega.Equal(map[string]string{
		"fdb-cluster-name":     cluster.Name,
		"fdb-process-class":    "storage",
		"fdb-instance-id":      "storage-1",
		"fdb-full-instance-id": "storage-1",
	}))
	g.Expect(pvc.Spec).To(gomega.Equal(corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				"storage": resource.MustParse("16G"),
			},
		},
	}))
}

func TestGetPvcWithCustomMetadata(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	cluster.Spec.PodTemplate = &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"fdb-annotation": "value1",
			},
			Labels: map[string]string{
				"fdb-label": "value2",
			},
		},
	}
	pvc, err := GetPvc(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(pvc.Namespace).To(gomega.Equal("default"))
	g.Expect(pvc.Name).To(gomega.Equal(fmt.Sprintf("%s-1-data", cluster.Name)))
	g.Expect(pvc.ObjectMeta.Annotations).To(gomega.Equal(map[string]string{
		"fdb-annotation": "value1",
	}))
	g.Expect(pvc.ObjectMeta.Labels).To(gomega.Equal(map[string]string{
		"fdb-cluster-name":     cluster.Name,
		"fdb-process-class":    "storage",
		"fdb-instance-id":      "1",
		"fdb-full-instance-id": "1",
		"fdb-label":            "value2",
	}))

}

func TestGetPvcWithNoVolume(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	cluster.Spec.VolumeSize = "0"
	pvc, err := GetPvc(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(pvc).To(gomega.BeNil())
}

func TestGetPvcWithStorageClass(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	class := "local"
	cluster.Spec.StorageClass = &class
	pvc, err := GetPvc(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(pvc.Spec.StorageClassName).To(gomega.Equal(&class))
}

func TestGetPvcForStatelessInstance(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	pvc, err := GetPvc(cluster, "stateless", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	g.Expect(pvc).To(gomega.BeNil())
}

func TestGetPvcWithInstanceIDPrefix(t *testing.T) {
	g := gomega.NewGomegaWithT(t)

	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	cluster := createDefaultCluster()
	cluster.Spec.InstanceIDPrefix = "dc1"
	pvc, err := GetPvc(cluster, "storage", 1)
	g.Expect(err).NotTo(gomega.HaveOccurred())

	g.Expect(pvc.Name).To(gomega.Equal(fmt.Sprintf("%s-storage-1-data", cluster.Name)))
	g.Expect(pvc.ObjectMeta.Labels).To(gomega.Equal(map[string]string{
		"fdb-cluster-name":     cluster.Name,
		"fdb-process-class":    "storage",
		"fdb-instance-id":      "dc1-storage-1",
		"fdb-full-instance-id": "dc1-storage-1",
		"fdb-label":            "value2",
	}))

}
