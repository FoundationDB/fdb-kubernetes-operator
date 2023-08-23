/*
 * replacements_test.go
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

package replacements

import (
	"context"
	"fmt"

	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podmanager"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"k8s.io/utils/pointer"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"github.com/go-logr/logr"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("replace_misconfigured_pods", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var log logr.Logger
	logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(GinkgoWriter)))

	BeforeEach(func() {
		log = logf.Log.WithName("replacements")
		cluster = internal.CreateDefaultCluster()
		err := internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{UseFutureDefaults: true})
		Expect(err).NotTo(HaveOccurred())

		cluster.Spec.LabelConfig.FilterOnOwnerReferences = pointer.Bool(false)
	})

	When("checking process groups for replacements", func() {
		var pod *corev1.Pod
		var status *fdbv1beta2.ProcessGroupStatus
		var pClass fdbv1beta2.ProcessClass
		var remove bool

		JustBeforeEach(func() {
			processGroupName := fmt.Sprintf("%s-%d", pClass, 1337)
			status = &fdbv1beta2.ProcessGroupStatus{
				ProcessGroupID: fdbv1beta2.ProcessGroupID(processGroupName),
				ProcessClass:   pClass,
			}

			if remove {
				status.MarkForRemoval()
			}

			pod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						fdbv1beta2.FDBProcessGroupIDLabel: processGroupName,
						fdbv1beta2.FDBProcessClassLabel:   string(status.ProcessClass),
					},
					Annotations: map[string]string{},
				},
			}

			spec, err := internal.GetPodSpec(cluster, status.ProcessClass, 1337)
			Expect(err).NotTo(HaveOccurred())

			pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey], err = internal.GetPodSpecHash(cluster, status.ProcessClass, 1337, spec)
			Expect(err).NotTo(HaveOccurred())

			pod.Spec = *spec
			err = internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})
			Expect(err).NotTo(HaveOccurred())
		})

		Describe("Check process group", func() {
			When("process group has no Pod", func() {
				It("should not need removal", func() {
					needsRemoval, err := processGroupNeedsRemoval(cluster, nil, nil, log)
					Expect(needsRemoval).To(BeFalse())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("process group has remove flag", func() {
				BeforeEach(func() {
					pClass = fdbv1beta2.ProcessClassStorage
					remove = true
				})

				It("should not need a removal", func() {
					needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
					Expect(needsRemoval).To(BeFalse())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("process group ID prefix changes", func() {
				When("the process class is storage", func() {
					BeforeEach(func() {
						pClass = fdbv1beta2.ProcessClassStorage
						remove = false
					})

					It("should need a removal", func() {
						needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
						Expect(needsRemoval).To(BeFalse())
						Expect(err).NotTo(HaveOccurred())

						// Change the process group ID should trigger a removal
						cluster.Spec.ProcessGroupIDPrefix = "test"
						needsRemoval, err = processGroupNeedsRemoval(cluster, pod, status, log)
						Expect(needsRemoval).To(BeTrue())
						Expect(err).NotTo(HaveOccurred())
					})
				})

				When("the process class is transaction", func() {
					BeforeEach(func() {
						pClass = fdbv1beta2.ProcessClassTransaction
						remove = false
					})

					It("should need a removal", func() {
						needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
						Expect(needsRemoval).To(BeFalse())
						Expect(err).NotTo(HaveOccurred())

						// Change the process group ID should trigger a removal
						cluster.Spec.ProcessGroupIDPrefix = "test"
						needsRemoval, err = processGroupNeedsRemoval(cluster, pod, status, log)
						Expect(needsRemoval).To(BeTrue())
						Expect(err).NotTo(HaveOccurred())
					})
				})
			})

			When("the public IP source changes", func() {
				BeforeEach(func() {
					pClass = fdbv1beta2.ProcessClassStorage
					remove = false
				})

				It("should need a removal", func() {
					needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
					Expect(needsRemoval).To(BeFalse())
					Expect(err).NotTo(HaveOccurred())

					ipSource := fdbv1beta2.PublicIPSourceService
					cluster.Spec.Routing.PublicIPSource = &ipSource
					needsRemoval, err = processGroupNeedsRemoval(cluster, pod, status, log)
					Expect(needsRemoval).To(BeTrue())
					Expect(err).NotTo(HaveOccurred())
				})
			})
		})

		When("the public IP source is removed", func() {
			BeforeEach(func() {
				pClass = fdbv1beta2.ProcessClassStorage
				remove = false
			})

			It("should need a removal", func() {
				pod.ObjectMeta.Annotations = map[string]string{
					fdbv1beta2.PublicIPSourceAnnotation: string(fdbv1beta2.PublicIPSourceService),
				}

				ipSource := fdbv1beta2.PublicIPSourceService
				cluster.Spec.Routing.PublicIPSource = &ipSource

				needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
				Expect(needsRemoval).To(BeFalse())
				Expect(err).NotTo(HaveOccurred())

				cluster.Spec.Routing.PublicIPSource = nil
				needsRemoval, err = processGroupNeedsRemoval(cluster, pod, status, log)
				Expect(needsRemoval).To(BeTrue())
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when the public IP source is set to default", func() {
			BeforeEach(func() {
				pClass = fdbv1beta2.ProcessClassStorage
				remove = false
			})

			It("should not need a removal", func() {
				needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
				Expect(needsRemoval).To(BeFalse())
				Expect(err).NotTo(HaveOccurred())

				ipSource := fdbv1beta2.PublicIPSourcePod
				cluster.Spec.Routing.PublicIPSource = &ipSource
				needsRemoval, err = processGroupNeedsRemoval(cluster, pod, status, log)
				Expect(needsRemoval).To(BeFalse())
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when the storageServersPerPod is changed for a storage class process group", func() {
			BeforeEach(func() {
				pClass = fdbv1beta2.ProcessClassStorage
				remove = false
			})

			It("should need a removal", func() {
				needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
				Expect(needsRemoval).To(BeFalse())
				Expect(err).NotTo(HaveOccurred())

				cluster.Spec.StorageServersPerPod = 2
				needsRemoval, err = processGroupNeedsRemoval(cluster, pod, status, log)
				Expect(needsRemoval).To(BeTrue())
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when the storageServersPerPod is changed for a non storage class process group", func() {
			BeforeEach(func() {
				pClass = fdbv1beta2.ProcessClassLog
				remove = false
			})

			It("should not need a removal", func() {
				needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
				Expect(needsRemoval).To(BeFalse())
				Expect(err).NotTo(HaveOccurred())

				cluster.Spec.StorageServersPerPod = 2
				needsRemoval, err = processGroupNeedsRemoval(cluster, pod, status, log)
				Expect(needsRemoval).To(BeFalse())
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when the nodeSelector changes", func() {
			BeforeEach(func() {
				pClass = fdbv1beta2.ProcessClassStorage
				remove = false
			})

			It("should need a removal", func() {
				needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
				Expect(needsRemoval).To(BeFalse())
				Expect(err).NotTo(HaveOccurred())

				cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.Spec.NodeSelector = map[string]string{
					"dummy": "test",
				}
				needsRemoval, err = processGroupNeedsRemoval(cluster, pod, status, log)
				Expect(needsRemoval).To(BeTrue())
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when nodeSelector changes and RemoteStorage is true", func() {
			BeforeEach(func() {
				pClass = fdbv1beta2.ProcessClassStorage
				remove = false
			})

			It("should not need a removal", func() {
				cluster.Spec.RemoteStorage = pointer.Bool(true)
				needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
				Expect(needsRemoval).To(BeFalse())
				Expect(err).NotTo(HaveOccurred())

				cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.Spec.NodeSelector = map[string]string{
					"dummy": "test",
				}
				needsRemoval, err = processGroupNeedsRemoval(cluster, pod, status, log)
				Expect(needsRemoval).To(BeFalse())
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when the nodeSelector doesn't match but the PodSpecHash matches", func() {
			BeforeEach(func() {
				pClass = fdbv1beta2.ProcessClassStorage
				remove = false
			})

			It("should not need a removal", func() {
				processClass := internal.GetProcessClassFromMeta(cluster, pod.ObjectMeta)
				processGroupID := internal.GetProcessGroupIDFromMeta(cluster, pod.ObjectMeta)
				idNum, err := processGroupID.GetIDNumber()
				Expect(err).NotTo(HaveOccurred())
				pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey], err = internal.GetPodSpecHash(cluster, processClass, idNum, nil)
				Expect(err).NotTo(HaveOccurred())
				pod.Spec.NodeSelector = map[string]string{
					"dummy": "test",
				}
				needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
				Expect(needsRemoval).To(BeFalse())
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when UpdatePodsByReplacement is not set and the PodSpecHash doesn't match", func() {
			BeforeEach(func() {
				pClass = fdbv1beta2.ProcessClassStorage
				remove = false
			})

			It("should not need a removal", func() {
				pod.Spec = corev1.PodSpec{
					Containers: []corev1.Container{{}},
				}
				needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
				Expect(needsRemoval).To(BeFalse())
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when PodUpdateStrategyReplacement is set and the PodSpecHash doesn't match", func() {
			BeforeEach(func() {
				pClass = fdbv1beta2.ProcessClassStorage
				remove = false
			})

			It("should need a removal", func() {
				pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey] = "-1"
				cluster.Spec.AutomationOptions.PodUpdateStrategy = fdbv1beta2.PodUpdateStrategyReplacement
				needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
				Expect(needsRemoval).To(BeTrue())
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when PodUpdateStrategyTransactionReplacement is set and the PodSpecHash doesn't match for storage", func() {
			BeforeEach(func() {
				pClass = fdbv1beta2.ProcessClassStorage
				remove = false
			})

			It("should not need a removal", func() {
				pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey] = "-1"
				cluster.Spec.AutomationOptions.PodUpdateStrategy = fdbv1beta2.PodUpdateStrategyTransactionReplacement
				needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
				Expect(needsRemoval).To(BeFalse())
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when PodUpdateStrategyTransactionReplacement is set and the PodSpecHash doesn't match for transaction", func() {
			BeforeEach(func() {
				pClass = fdbv1beta2.ProcessClassLog
				remove = false
			})

			It("should need a removal", func() {
				pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey] = "-1"
				cluster.Spec.AutomationOptions.PodUpdateStrategy = fdbv1beta2.PodUpdateStrategyTransactionReplacement
				needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
				Expect(needsRemoval).To(BeTrue())
				Expect(err).NotTo(HaveOccurred())
			})
		})

		When("PVC name doesn't match", func() {
			It("should need a removal", func() {
				pvc, err := internal.GetPvc(cluster, fdbv1beta2.ProcessClassStorage, 1)
				Expect(err).NotTo(HaveOccurred())
				pvc.Name = "Test-storage"
				needsRemoval, err := processGroupNeedsRemovalForPVC(cluster, *pvc, log)
				Expect(err).NotTo(HaveOccurred())
				Expect(needsRemoval).To(BeTrue())
			})
		})

		When("PVC name and PVC spec match", func() {
			It("should not need a removal", func() {
				pvc, err := internal.GetPvc(cluster, fdbv1beta2.ProcessClassStorage, 1)
				Expect(err).NotTo(HaveOccurred())
				needsRemoval, err := processGroupNeedsRemovalForPVC(cluster, *pvc, log)
				Expect(err).NotTo(HaveOccurred())
				Expect(needsRemoval).To(BeFalse())
			})
		})

		When("PVC hash doesn't match", func() {
			It("should need a removal", func() {
				pvc, err := internal.GetPvc(cluster, fdbv1beta2.ProcessClassStorage, 1)
				Expect(err).NotTo(HaveOccurred())
				pvc.Annotations[fdbv1beta2.LastSpecKey] = "1"
				needsRemoval, err := processGroupNeedsRemovalForPVC(cluster, *pvc, log)
				Expect(err).NotTo(HaveOccurred())
				Expect(needsRemoval).To(BeTrue())
			})
		})

		Context("when the memory resources are changed", func() {
			var status *fdbv1beta2.ProcessGroupStatus
			var pod *corev1.Pod

			BeforeEach(func() {
				err := internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{UseFutureDefaults: true})
				Expect(err).NotTo(HaveOccurred())
				pod, err = internal.GetPod(cluster, fdbv1beta2.ProcessClassStorage, 0)
				Expect(err).NotTo(HaveOccurred())
				status = &fdbv1beta2.ProcessGroupStatus{
					ProcessGroupID: fdbv1beta2.ProcessGroupID(fmt.Sprintf("%s-%d", fdbv1beta2.ProcessClassStorage, 1337)),
					ProcessClass:   fdbv1beta2.ProcessClassStorage,
				}

				needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
				Expect(needsRemoval).To(BeFalse())
				Expect(err).NotTo(HaveOccurred())
			})

			When("replacement for resource changes is activated", func() {
				JustBeforeEach(func() {
					cluster.Spec.ReplaceInstancesWhenResourcesChange = pointer.Bool(true)
				})

				When("the memory is increased", func() {
					JustBeforeEach(func() {
						newMemory, err := resource.ParseQuantity("1Ti")
						Expect(err).NotTo(HaveOccurred())
						cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.Spec.Containers[0].Resources = corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: newMemory,
							},
						}
					})

					It("should need a removal", func() {
						needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
						Expect(needsRemoval).To(BeTrue())
						Expect(err).NotTo(HaveOccurred())
					})
				})

				When("the memory is decreased", func() {
					JustBeforeEach(func() {
						newMemory, err := resource.ParseQuantity("1Ki")
						Expect(err).NotTo(HaveOccurred())
						cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.Spec.Containers[0].Resources = corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: newMemory,
							},
						}
					})

					It("should not need a removal", func() {
						needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
						Expect(needsRemoval).To(BeFalse())
						Expect(err).NotTo(HaveOccurred())
					})
				})

				When("the CPU is increased", func() {
					BeforeEach(func() {
						newCPU, err := resource.ParseQuantity("1000")
						Expect(err).NotTo(HaveOccurred())
						cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.Spec.Containers[0].Resources = corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: newCPU,
							},
						}
					})

					It("should need a removal", func() {
						needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
						Expect(needsRemoval).To(BeTrue())
						Expect(err).NotTo(HaveOccurred())
					})
				})

				When("the CPU is decreased", func() {
					BeforeEach(func() {
						newCPU, err := resource.ParseQuantity("1m")
						Expect(err).NotTo(HaveOccurred())
						cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.Spec.Containers[0].Resources = corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: newCPU,
							},
						}
					})

					It("should not need a removal", func() {
						needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
						Expect(needsRemoval).To(BeFalse())
						Expect(err).NotTo(HaveOccurred())
					})
				})

				When("adding another sidecar", func() {
					JustBeforeEach(func() {
						newCPU, err := resource.ParseQuantity("1000")
						Expect(err).NotTo(HaveOccurred())
						cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.Spec.Containers = append(cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.Spec.Containers,
							corev1.Container{
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										corev1.ResourceCPU: newCPU,
									},
								},
							})
					})

					It("should need a removal", func() {
						needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
						Expect(needsRemoval).To(BeTrue())
						Expect(err).NotTo(HaveOccurred())
					})
				})
			})

			When("replacement for resource changes is deactivated", func() {
				BeforeEach(func() {
					cluster.Spec.ReplaceInstancesWhenResourcesChange = pointer.Bool(false)
				})

				When("the memory is increased", func() {
					BeforeEach(func() {
						newMemory, err := resource.ParseQuantity("1Ti")
						Expect(err).NotTo(HaveOccurred())
						cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.Spec.Containers[0].Resources = corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: newMemory,
							},
						}
					})

					It("should not need a removal", func() {
						needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
						Expect(needsRemoval).To(BeFalse())
						Expect(err).NotTo(HaveOccurred())
					})
				})

				When("the memory is decreased", func() {
					BeforeEach(func() {
						newMemory, err := resource.ParseQuantity("1Ki")
						Expect(err).NotTo(HaveOccurred())
						cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.Spec.Containers[0].Resources = corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: newMemory,
							},
						}
					})

					It("should not need a removal", func() {
						needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
						Expect(needsRemoval).To(BeFalse())
						Expect(err).NotTo(HaveOccurred())
					})
				})

				When("the CPU is increased", func() {
					BeforeEach(func() {
						newCPU, err := resource.ParseQuantity("1000")
						Expect(err).NotTo(HaveOccurred())
						cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.Spec.Containers[0].Resources = corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: newCPU,
							},
						}
					})

					It("should not need a removal", func() {
						needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
						Expect(needsRemoval).To(BeFalse())
						Expect(err).NotTo(HaveOccurred())
					})
				})

				When("the CPU is decreased", func() {
					BeforeEach(func() {
						newCPU, err := resource.ParseQuantity("1m")
						Expect(err).NotTo(HaveOccurred())
						cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.Spec.Containers[0].Resources = corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: newCPU,
							},
						}
					})

					It("should not need a removal", func() {
						needsRemoval, err := processGroupNeedsRemoval(cluster, pod, status, log)
						Expect(needsRemoval).To(BeFalse())
						Expect(err).NotTo(HaveOccurred())
					})
				})
			})
		})
	})

	When("using MaxConcurrentMisconfiguredReplacements", func() {
		var pvcMap map[fdbv1beta2.ProcessGroupID]corev1.PersistentVolumeClaim

		BeforeEach(func() {
			pvcMap = map[fdbv1beta2.ProcessGroupID]corev1.PersistentVolumeClaim{}

			for i := 0; i < 10; i++ {
				_, id := internal.GetProcessGroupID(cluster, fdbv1beta2.ProcessClassStorage, i)
				newPVC, err := internal.GetPvc(cluster, fdbv1beta2.ProcessClassStorage, i)
				Expect(err).NotTo(HaveOccurred())
				pvcMap[id] = *newPVC
				newPod, err := internal.GetPod(cluster, fdbv1beta2.ProcessClassStorage, i)
				Expect(err).NotTo(HaveOccurred())
				Expect(k8sClient.Create(context.Background(), newPod)).NotTo(HaveOccurred())
				// Populate process groups
				cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus(id, fdbv1beta2.ProcessClassStorage, nil))
			}

			for i := 0; i < 1; i++ {
				_, id := internal.GetProcessGroupID(cluster, fdbv1beta2.ProcessClassTransaction, i)
				newPVC, err := internal.GetPvc(cluster, fdbv1beta2.ProcessClassTransaction, i)
				Expect(err).NotTo(HaveOccurred())
				pvcMap[id] = *newPVC
				newPod, err := internal.GetPod(cluster, fdbv1beta2.ProcessClassTransaction, i)
				Expect(err).NotTo(HaveOccurred())
				Expect(k8sClient.Create(context.Background(), newPod)).NotTo(HaveOccurred())
				// Populate process groups
				cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus(id, fdbv1beta2.ProcessClassTransaction, nil))
			}

			// Force a replacement of all processes
			cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.Spec.NodeSelector = map[string]string{
				"dummy": "test",
			}
		})

		When("No replacements are allowed", func() {
			BeforeEach(func() {
				cluster.Spec.AutomationOptions.MaxConcurrentReplacements = pointer.Int(0)
			})

			It("should not have a replacements", func() {
				hasReplacement, err := ReplaceMisconfiguredProcessGroups(context.Background(), podmanager.StandardPodLifecycleManager{}, k8sClient, log, cluster, pvcMap)
				Expect(err).NotTo(HaveOccurred())
				Expect(hasReplacement).To(BeFalse())

				cntReplacements := 0
				for _, pGroup := range cluster.Status.ProcessGroups {
					if !pGroup.IsMarkedForRemoval() {
						continue
					}

					cntReplacements++
				}

				Expect(cntReplacements).To(BeNumerically("==", 0))
			})
		})

		When("Two replacements are allowed", func() {
			BeforeEach(func() {
				cluster.Spec.AutomationOptions.MaxConcurrentReplacements = pointer.Int(2)
			})

			It("should have two replacements", func() {
				hasReplacement, err := ReplaceMisconfiguredProcessGroups(context.Background(), podmanager.StandardPodLifecycleManager{}, k8sClient, log, cluster, pvcMap)
				Expect(err).NotTo(HaveOccurred())
				Expect(hasReplacement).To(BeTrue())

				cntReplacements := 0
				for _, pGroup := range cluster.Status.ProcessGroups {
					if !pGroup.IsMarkedForRemoval() {
						continue
					}

					cntReplacements++
				}

				Expect(cntReplacements).To(BeNumerically("==", 2))
			})
		})

		When("Setting is unset", func() {
			It("should replace all process groups", func() {
				hasReplacement, err := ReplaceMisconfiguredProcessGroups(context.Background(), podmanager.StandardPodLifecycleManager{}, k8sClient, log, cluster, pvcMap)
				Expect(err).NotTo(HaveOccurred())
				Expect(hasReplacement).To(BeTrue())

				cntReplacements := 0
				for _, pGroup := range cluster.Status.ProcessGroups {
					if !pGroup.IsMarkedForRemoval() {
						continue
					}

					cntReplacements++
				}

				Expect(cntReplacements).To(BeNumerically("==", len(cluster.Status.ProcessGroups)))
			})
		})

		When("the image doesn't match with the desired image", func() {
			BeforeEach(func() {
				cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.Spec.NodeSelector = map[string]string{}
			})

			When("the process is a storage process", func() {
				BeforeEach(func() {
					podName, _ := internal.GetProcessGroupID(cluster, fdbv1beta2.ProcessClassStorage, 0)
					currentPod := &corev1.Pod{}
					Expect(k8sClient.Get(context.Background(), ctrlClient.ObjectKey{Name: podName, Namespace: cluster.Namespace}, currentPod)).NotTo(HaveOccurred())

					spec := currentPod.Spec.DeepCopy()
					var cIdx int
					for idx, con := range spec.Containers {
						if con.Name != fdbv1beta2.MainContainerName {
							continue
						}

						cIdx = idx
						break
					}

					spec.Containers[cIdx].Image = "banana"
					currentPod.Spec = *spec
					Expect(k8sClient.Update(context.Background(), currentPod)).NotTo(HaveOccurred())
				})

				It("should not have any replacements", func() {
					hasReplacement, err := ReplaceMisconfiguredProcessGroups(context.Background(), podmanager.StandardPodLifecycleManager{}, k8sClient, log, cluster, pvcMap)
					Expect(err).NotTo(HaveOccurred())
					Expect(hasReplacement).To(BeFalse())

					cntReplacements := 0
					for _, pGroup := range cluster.Status.ProcessGroups {
						if !pGroup.IsMarkedForRemoval() {
							continue
						}

						cntReplacements++
					}

					Expect(cntReplacements).To(BeNumerically("==", 0))
				})
			})
		})
	})
})
