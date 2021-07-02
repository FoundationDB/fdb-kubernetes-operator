/*
 * replace_misconfigured_pods.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021 Apple Inc. and the FoundationDB project authors
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

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("replace_misconfigured_pods", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var err error
	instanceName := fmt.Sprintf("%s-%d", fdbtypes.ProcessClassStorage, 1337)

	BeforeEach(func() {
		cluster = createDefaultCluster()
		cluster.Spec.UpdatePodsByReplacement = false
		cluster.Spec.Processes = map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{
			fdbtypes.ProcessClassGeneral: {
				PodTemplate: &corev1.PodTemplateSpec{},
			},
		}
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("Check instance", func() {
		Context("when instance has no Pod", func() {
			It("should not need removal", func() {
				instance := FdbInstance{}
				needsRemoval, err := instanceNeedsRemoval(cluster, instance, nil)
				Expect(needsRemoval).To(BeFalse())
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when processGroupStatus is missing", func() {
			It("should return an error", func() {
				instance := FdbInstance{
					Metadata: &metav1.ObjectMeta{
						Labels: map[string]string{
							fdbtypes.FDBInstanceIDLabel: instanceName,
						},
					},
					Pod: &corev1.Pod{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{}},
						},
					},
				}
				needsRemoval, err := instanceNeedsRemoval(cluster, instance, nil)
				Expect(needsRemoval).To(BeFalse())
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(Equal(fmt.Sprintf("unknown instance %s in replace_misconfigured_pods", instanceName)))
			})
		})

		Context("when processGroupStatus has remove flag", func() {
			It("should not need a removal", func() {
				instance := FdbInstance{
					Metadata: &metav1.ObjectMeta{
						Labels: map[string]string{
							fdbtypes.FDBInstanceIDLabel: instanceName,
						},
					},
					Pod: &corev1.Pod{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{}},
						},
					},
				}
				status := &fdbtypes.ProcessGroupStatus{
					ProcessGroupID: instanceName,
					Remove:         true,
				}
				needsRemoval, err := instanceNeedsRemoval(cluster, instance, status)
				Expect(needsRemoval).To(BeFalse())
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when instanceID prefix changes", func() {
			It("should need a removal", func() {
				instance := FdbInstance{
					Metadata: &metav1.ObjectMeta{
						Labels: map[string]string{
							fdbtypes.FDBInstanceIDLabel:   instanceName,
							fdbtypes.FDBProcessClassLabel: string(fdbtypes.ProcessClassStorage),
						},
					},
					Pod: &corev1.Pod{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{}},
						},
					},
				}
				status := &fdbtypes.ProcessGroupStatus{
					ProcessGroupID: instanceName,
					Remove:         false,
				}
				needsRemoval, err := instanceNeedsRemoval(cluster, instance, status)
				Expect(needsRemoval).To(BeFalse())
				Expect(err).NotTo(HaveOccurred())

				// Change the instance ID should trigger a removal
				cluster.Spec.InstanceIDPrefix = "test"
				needsRemoval, err = instanceNeedsRemoval(cluster, instance, status)
				Expect(needsRemoval).To(BeTrue())
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when the public IP source changes", func() {
			It("should need a removal", func() {
				instance := FdbInstance{
					Metadata: &metav1.ObjectMeta{
						Labels: map[string]string{
							fdbtypes.FDBInstanceIDLabel:   instanceName,
							fdbtypes.FDBProcessClassLabel: string(fdbtypes.ProcessClassStorage),
						},
						Annotations: map[string]string{},
					},
					Pod: &corev1.Pod{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{}},
						},
					},
				}
				status := &fdbtypes.ProcessGroupStatus{
					ProcessGroupID: instanceName,
					Remove:         false,
				}
				needsRemoval, err := instanceNeedsRemoval(cluster, instance, status)
				Expect(needsRemoval).To(BeFalse())
				Expect(err).NotTo(HaveOccurred())

				ipSource := fdbtypes.PublicIPSourceService
				cluster.Spec.Services.PublicIPSource = &ipSource
				needsRemoval, err = instanceNeedsRemoval(cluster, instance, status)
				Expect(needsRemoval).To(BeTrue())
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})

	Context("when the public IP source is removed", func() {
		It("should need a removal", func() {
			instance := FdbInstance{
				Metadata: &metav1.ObjectMeta{
					Labels: map[string]string{
						fdbtypes.FDBInstanceIDLabel:   instanceName,
						fdbtypes.FDBProcessClassLabel: string(fdbtypes.ProcessClassStorage),
					},
					Annotations: map[string]string{
						fdbtypes.PublicIPSourceAnnotation: string(fdbtypes.PublicIPSourceService),
					},
				},
				Pod: &corev1.Pod{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{}},
					},
				},
			}
			status := &fdbtypes.ProcessGroupStatus{
				ProcessGroupID: instanceName,
				Remove:         false,
			}
			ipSource := fdbtypes.PublicIPSourceService
			cluster.Spec.Services.PublicIPSource = &ipSource

			needsRemoval, err := instanceNeedsRemoval(cluster, instance, status)
			Expect(needsRemoval).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())

			cluster.Spec.Services.PublicIPSource = nil
			needsRemoval, err = instanceNeedsRemoval(cluster, instance, status)
			Expect(needsRemoval).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("when the public IP source is set to default", func() {
		It("should not need a removal", func() {
			instance := FdbInstance{
				Metadata: &metav1.ObjectMeta{
					Labels: map[string]string{
						fdbtypes.FDBInstanceIDLabel:   instanceName,
						fdbtypes.FDBProcessClassLabel: string(fdbtypes.ProcessClassStorage),
					},
					Annotations: map[string]string{},
				},
				Pod: &corev1.Pod{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{}},
					},
				},
			}
			status := &fdbtypes.ProcessGroupStatus{
				ProcessGroupID: instanceName,
				Remove:         false,
			}
			needsRemoval, err := instanceNeedsRemoval(cluster, instance, status)
			Expect(needsRemoval).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())

			ipSource := fdbtypes.PublicIPSourcePod
			cluster.Spec.Services.PublicIPSource = &ipSource
			needsRemoval, err = instanceNeedsRemoval(cluster, instance, status)
			Expect(needsRemoval).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("when the storageServersPerPod is changed for a storage class instance", func() {
		It("should need a removal", func() {
			instance := FdbInstance{
				Metadata: &metav1.ObjectMeta{
					Labels: map[string]string{
						fdbtypes.FDBInstanceIDLabel:   instanceName,
						fdbtypes.FDBProcessClassLabel: string(fdbtypes.ProcessClassStorage),
					},
					Annotations: map[string]string{},
				},
				Pod: &corev1.Pod{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{}},
					},
				},
			}
			status := &fdbtypes.ProcessGroupStatus{
				ProcessGroupID: instanceName,
				Remove:         false,
			}
			needsRemoval, err := instanceNeedsRemoval(cluster, instance, status)
			Expect(needsRemoval).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())

			cluster.Spec.StorageServersPerPod = 2
			needsRemoval, err = instanceNeedsRemoval(cluster, instance, status)
			Expect(needsRemoval).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("when the storageServersPerPod is changed for a non storage class instance", func() {
		It("should not need a removal", func() {
			instance := FdbInstance{
				Metadata: &metav1.ObjectMeta{
					Labels: map[string]string{
						fdbtypes.FDBInstanceIDLabel:   fmt.Sprintf("%s-1337", fdbtypes.ProcessClassLog),
						fdbtypes.FDBProcessClassLabel: string(fdbtypes.ProcessClassLog),
					},
					Annotations: map[string]string{},
				},
				Pod: &corev1.Pod{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{}},
					},
				},
			}
			status := &fdbtypes.ProcessGroupStatus{
				ProcessGroupID: instanceName,
				Remove:         false,
			}
			needsRemoval, err := instanceNeedsRemoval(cluster, instance, status)
			Expect(needsRemoval).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())

			cluster.Spec.StorageServersPerPod = 2
			needsRemoval, err = instanceNeedsRemoval(cluster, instance, status)
			Expect(needsRemoval).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("when the nodeSelector changes", func() {
		It("should need a removal", func() {
			instance := FdbInstance{
				Metadata: &metav1.ObjectMeta{
					Labels: map[string]string{
						fdbtypes.FDBInstanceIDLabel:   instanceName,
						fdbtypes.FDBProcessClassLabel: string(fdbtypes.ProcessClassStorage),
					},
					Annotations: map[string]string{},
				},
				Pod: &corev1.Pod{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{}},
					},
				},
			}
			status := &fdbtypes.ProcessGroupStatus{
				ProcessGroupID: instanceName,
				Remove:         false,
			}
			needsRemoval, err := instanceNeedsRemoval(cluster, instance, status)
			Expect(needsRemoval).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())

			cluster.Spec.Processes[fdbtypes.ProcessClassGeneral].PodTemplate.Spec.NodeSelector = map[string]string{
				"dummy": "test",
			}
			needsRemoval, err = instanceNeedsRemoval(cluster, instance, status)
			Expect(needsRemoval).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("when UpdatePodsByReplacement is not set and the PodSpecHash doesn't match", func() {
		It("should not need a removal", func() {
			instance := FdbInstance{
				Metadata: &metav1.ObjectMeta{
					Labels: map[string]string{
						fdbtypes.FDBInstanceIDLabel:   instanceName,
						fdbtypes.FDBProcessClassLabel: string(fdbtypes.ProcessClassStorage),
					},
					Annotations: map[string]string{},
				},
				Pod: &corev1.Pod{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{}},
					},
				},
			}
			status := &fdbtypes.ProcessGroupStatus{
				ProcessGroupID: instanceName,
				Remove:         false,
			}
			needsRemoval, err := instanceNeedsRemoval(cluster, instance, status)
			Expect(needsRemoval).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("when UpdatePodsByReplacement is set and the PodSpecHash doesn't match", func() {
		It("should need a removal", func() {
			instance := FdbInstance{
				Metadata: &metav1.ObjectMeta{
					Labels: map[string]string{
						fdbtypes.FDBInstanceIDLabel:   instanceName,
						fdbtypes.FDBProcessClassLabel: string(fdbtypes.ProcessClassStorage),
					},
					Annotations: map[string]string{},
				},
				Pod: &corev1.Pod{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{}},
					},
				},
			}
			status := &fdbtypes.ProcessGroupStatus{
				ProcessGroupID: instanceName,
				Remove:         false,
			}
			err := internal.NormalizeClusterSpec(&cluster.Spec, internal.DeprecationOptions{UseFutureDefaults: true})
			Expect(err).NotTo(HaveOccurred())

			cluster.Spec.UpdatePodsByReplacement = true
			needsRemoval, err := instanceNeedsRemoval(cluster, instance, status)
			Expect(needsRemoval).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("when the memory resources are changed", func() {
		var status *fdbtypes.ProcessGroupStatus
		var instance FdbInstance

		BeforeEach(func() {
			err := internal.NormalizeClusterSpec(&cluster.Spec, internal.DeprecationOptions{UseFutureDefaults: true})
			Expect(err).NotTo(HaveOccurred())
			pod, err := GetPod(cluster, fdbtypes.ProcessClassStorage, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(err).NotTo(HaveOccurred())
			instance = FdbInstance{
				Metadata: &metav1.ObjectMeta{
					Labels: map[string]string{
						fdbtypes.FDBInstanceIDLabel:   pod.ObjectMeta.Labels[fdbtypes.FDBInstanceIDLabel],
						fdbtypes.FDBProcessClassLabel: string(fdbtypes.ProcessClassStorage),
					},
					Annotations: map[string]string{},
				},
				Pod: pod,
			}

			status = &fdbtypes.ProcessGroupStatus{
				ProcessGroupID: instanceName,
				Remove:         false,
			}

			needsRemoval, err := instanceNeedsRemoval(cluster, instance, status)
			Expect(needsRemoval).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
		})

		When("replacement for resource changes is activated", func() {
			BeforeEach(func() {
				t := true
				cluster.Spec.ReplaceInstancesWhenResourcesChange = &t
			})

			When("the memory is increased", func() {
				BeforeEach(func() {
					newMemory, err := resource.ParseQuantity("1Ti")
					Expect(err).NotTo(HaveOccurred())
					cluster.Spec.Processes[fdbtypes.ProcessClassGeneral].PodTemplate.Spec.Containers[0].Resources = corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: newMemory,
						},
					}
				})

				It("should need a removal", func() {
					needsRemoval, err := instanceNeedsRemoval(cluster, instance, status)
					Expect(needsRemoval).To(BeTrue())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("the memory is decreased", func() {
				BeforeEach(func() {
					newMemory, err := resource.ParseQuantity("1Ki")
					Expect(err).NotTo(HaveOccurred())
					cluster.Spec.Processes[fdbtypes.ProcessClassGeneral].PodTemplate.Spec.Containers[0].Resources = corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: newMemory,
						},
					}
				})

				It("should not need a removal", func() {
					needsRemoval, err := instanceNeedsRemoval(cluster, instance, status)
					Expect(needsRemoval).To(BeFalse())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("the CPU is increased", func() {
				BeforeEach(func() {
					newCPU, err := resource.ParseQuantity("1000")
					Expect(err).NotTo(HaveOccurred())
					cluster.Spec.Processes[fdbtypes.ProcessClassGeneral].PodTemplate.Spec.Containers[0].Resources = corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: newCPU,
						},
					}
				})

				It("should need a removal", func() {
					needsRemoval, err := instanceNeedsRemoval(cluster, instance, status)
					Expect(needsRemoval).To(BeTrue())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("the CPU is decreased", func() {
				BeforeEach(func() {
					newCPU, err := resource.ParseQuantity("1m")
					Expect(err).NotTo(HaveOccurred())
					cluster.Spec.Processes[fdbtypes.ProcessClassGeneral].PodTemplate.Spec.Containers[0].Resources = corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: newCPU,
						},
					}
				})

				It("should not need a removal", func() {
					needsRemoval, err := instanceNeedsRemoval(cluster, instance, status)
					Expect(needsRemoval).To(BeFalse())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("adding another sidecar", func() {
				BeforeEach(func() {
					newCPU, err := resource.ParseQuantity("1000")
					Expect(err).NotTo(HaveOccurred())
					cluster.Spec.Processes[fdbtypes.ProcessClassGeneral].PodTemplate.Spec.Containers = append(cluster.Spec.Processes[fdbtypes.ProcessClassGeneral].PodTemplate.Spec.Containers,
						corev1.Container{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU: newCPU,
								},
							},
						})
				})

				It("should need a removal", func() {
					needsRemoval, err := instanceNeedsRemoval(cluster, instance, status)
					Expect(needsRemoval).To(BeTrue())
					Expect(err).NotTo(HaveOccurred())
				})
			})
		})

		When("replacement for resource changes is deactivated", func() {
			BeforeEach(func() {
				t := false
				cluster.Spec.ReplaceInstancesWhenResourcesChange = &t
			})

			When("the memory is increased", func() {
				BeforeEach(func() {
					newMemory, err := resource.ParseQuantity("1Ti")
					Expect(err).NotTo(HaveOccurred())
					cluster.Spec.Processes[fdbtypes.ProcessClassGeneral].PodTemplate.Spec.Containers[0].Resources = corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: newMemory,
						},
					}
				})

				It("should not need a removal", func() {
					needsRemoval, err := instanceNeedsRemoval(cluster, instance, status)
					Expect(needsRemoval).To(BeFalse())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("the memory is decreased", func() {
				BeforeEach(func() {
					newMemory, err := resource.ParseQuantity("1Ki")
					Expect(err).NotTo(HaveOccurred())
					cluster.Spec.Processes[fdbtypes.ProcessClassGeneral].PodTemplate.Spec.Containers[0].Resources = corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: newMemory,
						},
					}
				})

				It("should not need a removal", func() {
					needsRemoval, err := instanceNeedsRemoval(cluster, instance, status)
					Expect(needsRemoval).To(BeFalse())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("the CPU is increased", func() {
				BeforeEach(func() {
					newCPU, err := resource.ParseQuantity("1000")
					Expect(err).NotTo(HaveOccurred())
					cluster.Spec.Processes[fdbtypes.ProcessClassGeneral].PodTemplate.Spec.Containers[0].Resources = corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: newCPU,
						},
					}
				})

				It("should not need a removal", func() {
					needsRemoval, err := instanceNeedsRemoval(cluster, instance, status)
					Expect(needsRemoval).To(BeFalse())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("the CPU is decreased", func() {
				BeforeEach(func() {
					newCPU, err := resource.ParseQuantity("1m")
					Expect(err).NotTo(HaveOccurred())
					cluster.Spec.Processes[fdbtypes.ProcessClassGeneral].PodTemplate.Spec.Containers[0].Resources = corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU: newCPU,
						},
					}
				})

				It("should not need a removal", func() {
					needsRemoval, err := instanceNeedsRemoval(cluster, instance, status)
					Expect(needsRemoval).To(BeFalse())
					Expect(err).NotTo(HaveOccurred())
				})
			})
		})
	})
})
