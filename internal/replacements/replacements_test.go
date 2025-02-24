/*
 * replacements_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021-2024 Apple Inc. and the FoundationDB project authors
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

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/podmanager"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"k8s.io/utils/pointer"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/go-logr/logr"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("replace_misconfigured_pods", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var log logr.Logger
	var deprecationOptions internal.DeprecationOptions
	logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(GinkgoWriter)))

	BeforeEach(func() {
		log = logf.Log.WithName("replacements")
		cluster = internal.CreateDefaultCluster()
		deprecationOptions = internal.DeprecationOptions{UseFutureDefaults: false}
		Expect(internal.NormalizeClusterSpec(cluster, deprecationOptions)).NotTo(HaveOccurred())
		cluster.Spec.LabelConfig.FilterOnOwnerReferences = pointer.Bool(false)
	})

	When("checking process groups for replacements", func() {
		var pod *corev1.Pod
		var processGroup *fdbv1beta2.ProcessGroupStatus
		var needsRemoval bool
		var err error
		replaceOnSecurityContextChange := true

		JustBeforeEach(func() {
			needsRemoval, err = processGroupNeedsRemovalForPod(cluster, pod, processGroup, log, replaceOnSecurityContextChange)
		})

		When("a storage Pod is checked", func() {
			BeforeEach(func() {
				processGroupName := fmt.Sprintf("%s-%d", fdbv1beta2.ProcessClassStorage, 1337)
				processGroup = &fdbv1beta2.ProcessGroupStatus{
					ProcessGroupID: fdbv1beta2.ProcessGroupID(processGroupName),
					ProcessClass:   fdbv1beta2.ProcessClassStorage,
				}

				pod = &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							fdbv1beta2.FDBProcessGroupIDLabel: processGroupName,
							fdbv1beta2.FDBProcessClassLabel:   string(processGroup.ProcessClass),
						},
						Annotations: map[string]string{},
					},
				}

				spec, err := internal.GetPodSpec(cluster, processGroup)
				Expect(err).NotTo(HaveOccurred())

				pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey], err = internal.GetPodSpecHash(cluster, processGroup, spec)
				Expect(err).NotTo(HaveOccurred())

				pod.Spec = *spec
				Expect(internal.NormalizeClusterSpec(cluster, deprecationOptions)).NotTo(HaveOccurred())
			})

			When("process group has no Pod", func() {
				It("should not need removal", func() {
					Expect(needsRemoval).To(BeFalse())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("process group has remove flag", func() {
				BeforeEach(func() {
					processGroup.MarkForRemoval()
				})

				It("should not need a removal", func() {
					Expect(needsRemoval).To(BeFalse())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("process group ID prefix changes", func() {
				BeforeEach(func() {
					// Change the process group ID should trigger a removal
					cluster.Spec.ProcessGroupIDPrefix = "test"
				})

				It("should need a removal", func() {
					Expect(needsRemoval).To(BeTrue())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("the public IP source changes", func() {
				BeforeEach(func() {
					ipSource := fdbv1beta2.PublicIPSourceService
					cluster.Spec.Routing.PublicIPSource = &ipSource
				})

				It("should need a removal", func() {
					Expect(needsRemoval).To(BeTrue())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("the public IP source is removed", func() {
				BeforeEach(func() {
					pod.ObjectMeta.Annotations = map[string]string{
						fdbv1beta2.PublicIPSourceAnnotation: string(fdbv1beta2.PublicIPSourceService),
					}
					cluster.Spec.Routing.PublicIPSource = nil
				})

				It("should need a removal", func() {
					Expect(needsRemoval).To(BeTrue())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("the public IP source is set to default", func() {
				BeforeEach(func() {
					ipSource := fdbv1beta2.PublicIPSourcePod
					cluster.Spec.Routing.PublicIPSource = &ipSource
				})

				It("should not need a removal", func() {
					Expect(needsRemoval).To(BeFalse())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("the storageServersPerPod is changed for a storage class process group", func() {
				BeforeEach(func() {
					cluster.Spec.StorageServersPerPod = 2
				})

				It("should need a removal", func() {
					Expect(needsRemoval).To(BeTrue())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("the nodeSelector changes", func() {
				BeforeEach(func() {
					cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral].PodTemplate.Spec.NodeSelector = map[string]string{
						"dummy": "test",
					}
				})

				It("should need a removal", func() {
					Expect(needsRemoval).To(BeTrue())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("the nodeSelector doesn't match but the PodSpecHash matches", func() {
				BeforeEach(func() {
					pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey], err = internal.GetPodSpecHash(cluster, processGroup, nil)
					Expect(err).NotTo(HaveOccurred())
					pod.Spec.NodeSelector = map[string]string{
						"dummy": "test",
					}
				})

				It("should not need a removal", func() {
					Expect(needsRemoval).To(BeFalse())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("the image type changes", func() {
				BeforeEach(func() {
					imageType := fdbv1beta2.ImageTypeUnified
					cluster.Spec.ImageType = &imageType
				})

				When("one storage server per pod should be used", func() {
					BeforeEach(func() {
						cluster.Spec.StorageServersPerPod = 1
					})

					It("should need a removal", func() {
						Expect(needsRemoval).To(BeTrue())
						Expect(err).NotTo(HaveOccurred())
					})
				})

				When("two storage server per pod should be used", func() {
					BeforeEach(func() {
						cluster.Spec.StorageServersPerPod = 2
						// Make sure the Pod is updated, otherwise a replacement will be needed because the storage
						// server per pod have changed.
						spec, err := internal.GetPodSpec(cluster, processGroup)
						Expect(err).NotTo(HaveOccurred())

						pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey], err = internal.GetPodSpecHash(cluster, processGroup, spec)
						Expect(err).NotTo(HaveOccurred())

						pod.Spec = *spec
						Expect(internal.NormalizeClusterSpec(cluster, deprecationOptions)).NotTo(HaveOccurred())
					})

					It("should not need a removal", func() {
						Expect(needsRemoval).To(BeFalse())
						Expect(err).NotTo(HaveOccurred())
					})
				})
			})

			When("UpdatePodsByReplacement is not set and the PodSpecHash doesn't match", func() {
				BeforeEach(func() {
					pod.Spec = corev1.PodSpec{
						Containers: []corev1.Container{{}},
					}
				})

				It("should not need a removal", func() {
					Expect(needsRemoval).To(BeFalse())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("PodUpdateStrategyReplacement is set and the PodSpecHash doesn't match", func() {
				BeforeEach(func() {
					pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey] = "-1"
					cluster.Spec.AutomationOptions.PodUpdateStrategy = fdbv1beta2.PodUpdateStrategyReplacement
				})

				It("should need a removal", func() {
					Expect(needsRemoval).To(BeTrue())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("PodUpdateStrategyTransactionReplacement is set and the PodSpecHash doesn't match", func() {
				BeforeEach(func() {
					pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey] = "-1"
					cluster.Spec.AutomationOptions.PodUpdateStrategy = fdbv1beta2.PodUpdateStrategyTransactionReplacement
				})

				It("should not need a removal", func() {
					Expect(needsRemoval).To(BeFalse())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("checking if the PVC requires a replacement", func() {
				var pvc *corev1.PersistentVolumeClaim

				BeforeEach(func() {
					pvc, err = internal.GetPvc(cluster, processGroup)
					Expect(err).NotTo(HaveOccurred())
				})

				JustBeforeEach(func() {
					needsRemoval, err = processGroupNeedsRemovalForPVC(cluster, pvc, log, processGroup)
				})

				When("PVC name doesn't match", func() {
					BeforeEach(func() {
						pvc.Name = "Test-storage"
					})

					It("should need a removal", func() {
						Expect(err).NotTo(HaveOccurred())
						Expect(needsRemoval).To(BeTrue())
					})
				})

				When("PVC name and PVC spec match", func() {
					It("should not need a removal", func() {
						Expect(err).NotTo(HaveOccurred())
						Expect(needsRemoval).To(BeFalse())
					})
				})

				When("PVC hash doesn't match", func() {
					BeforeEach(func() {
						pvc.Annotations[fdbv1beta2.LastSpecKey] = "1"
					})

					It("should need a removal", func() {
						Expect(err).NotTo(HaveOccurred())
						Expect(needsRemoval).To(BeTrue())
					})
				})
			})

			When("replacement for resource changes is activated", func() {
				BeforeEach(func() {
					cluster.Spec.ReplaceInstancesWhenResourcesChange = pointer.Bool(true)
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

					It("should need a removal", func() {
						Expect(needsRemoval).To(BeTrue())
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
						Expect(needsRemoval).To(BeFalse())
						Expect(err).NotTo(HaveOccurred())
					})
				})

				When("adding another sidecar", func() {
					BeforeEach(func() {
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
						Expect(needsRemoval).To(BeFalse())
						Expect(err).NotTo(HaveOccurred())
					})
				})
			})

			When("the securityContext doesn't match", func() {
				When("the last applied spec hash is different from desired spec hash", func() {
					BeforeEach(func() {
						pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey] = "banana"
					})

					When("FSGroup is changed", func() {
						BeforeEach(func() {
							pod.Spec.SecurityContext = &corev1.PodSecurityContext{FSGroup: pointer.Int64(1234)}
						})

						When("replaceOnSecurityContextChange is true", func() {
							It("should need a removal", func() {
								Expect(needsRemoval).To(BeTrue())
								Expect(err).NotTo(HaveOccurred())
							})
						})

						When("replaceOnSecurityContextChange is false", func() {
							BeforeEach(func() {
								replaceOnSecurityContextChange = false
							})

							It("should *not* need a removal", func() {
								Expect(needsRemoval).To(BeFalse())
								Expect(err).NotTo(HaveOccurred())
							})
						})
					})
				})

				When("the last applied spec hash is not different from desired spec hash", func() {
					When("FSGroup is changed", func() {
						BeforeEach(func() {
							pod.Spec.SecurityContext = &corev1.PodSecurityContext{FSGroup: pointer.Int64(1234)}
						})

						It("should not need a removal (guard against server-side defaults)", func() {
							Expect(needsRemoval).To(BeFalse())
							Expect(err).NotTo(HaveOccurred())
						})
					})
				})
			})
		})

		When("a log Pod is checked", func() {
			BeforeEach(func() {
				processGroupName := fmt.Sprintf("%s-%d", fdbv1beta2.ProcessClassLog, 1337)
				processGroup = &fdbv1beta2.ProcessGroupStatus{
					ProcessGroupID: fdbv1beta2.ProcessGroupID(processGroupName),
					ProcessClass:   fdbv1beta2.ProcessClassLog,
				}

				pod = &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							fdbv1beta2.FDBProcessGroupIDLabel: processGroupName,
							fdbv1beta2.FDBProcessClassLabel:   string(processGroup.ProcessClass),
						},
						Annotations: map[string]string{},
					},
				}

				spec, err := internal.GetPodSpec(cluster, processGroup)
				Expect(err).NotTo(HaveOccurred())

				pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey], err = internal.GetPodSpecHash(cluster, processGroup, spec)
				Expect(err).NotTo(HaveOccurred())

				pod.Spec = *spec
				Expect(internal.NormalizeClusterSpec(cluster, deprecationOptions)).NotTo(HaveOccurred())
			})

			When("the storageServersPerPod is changed for a non storage class process group", func() {
				BeforeEach(func() {
					cluster.Spec.StorageServersPerPod = 2
				})

				It("should not need a removal", func() {
					Expect(needsRemoval).To(BeFalse())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("PodUpdateStrategyTransactionReplacement is set and the PodSpecHash doesn't match for transaction", func() {
				BeforeEach(func() {
					pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey] = "-1"
					cluster.Spec.AutomationOptions.PodUpdateStrategy = fdbv1beta2.PodUpdateStrategyTransactionReplacement
				})

				It("should need a removal", func() {
					Expect(needsRemoval).To(BeTrue())
					Expect(err).NotTo(HaveOccurred())
				})
			})

			When("process group ID prefix changes", func() {
				BeforeEach(func() {
					// Change the process group ID should trigger a removal
					cluster.Spec.ProcessGroupIDPrefix = "test"
				})

				It("should need a removal", func() {
					Expect(needsRemoval).To(BeTrue())
					Expect(err).NotTo(HaveOccurred())
				})
			})
		})
	})

	When("using MaxConcurrentMisconfiguredReplacements", func() {
		BeforeEach(func() {

			for i := 0; i < 10; i++ {
				_, id := cluster.GetProcessGroupID(fdbv1beta2.ProcessClassStorage, i)
				processGroup := &fdbv1beta2.ProcessGroupStatus{
					ProcessClass:   fdbv1beta2.ProcessClassStorage,
					ProcessGroupID: id,
				}
				newPVC, err := internal.GetPvc(cluster, processGroup)
				Expect(err).NotTo(HaveOccurred())
				Expect(k8sClient.Create(context.Background(), newPVC)).To(Succeed())

				newPod, err := internal.GetPod(cluster, processGroup)
				Expect(err).NotTo(HaveOccurred())
				Expect(k8sClient.Create(context.Background(), newPod)).To(Succeed())

				// Populate process groups
				cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus(id, fdbv1beta2.ProcessClassStorage, nil))
			}

			for i := 0; i < 1; i++ {
				_, id := cluster.GetProcessGroupID(fdbv1beta2.ProcessClassTransaction, i)
				processGroup := &fdbv1beta2.ProcessGroupStatus{
					ProcessClass:   fdbv1beta2.ProcessClassTransaction,
					ProcessGroupID: id,
				}

				newPVC, err := internal.GetPvc(cluster, processGroup)
				Expect(err).NotTo(HaveOccurred())
				Expect(k8sClient.Create(context.Background(), newPVC)).To(Succeed())

				newPod, err := internal.GetPod(cluster, processGroup)
				Expect(err).NotTo(HaveOccurred())
				Expect(k8sClient.Create(context.Background(), newPod)).To(Succeed())
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
				hasReplacement, err := ReplaceMisconfiguredProcessGroups(context.Background(), podmanager.StandardPodLifecycleManager{}, k8sClient, log, cluster, true)
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
				hasReplacement, err := ReplaceMisconfiguredProcessGroups(context.Background(), podmanager.StandardPodLifecycleManager{}, k8sClient, log, cluster, true)
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
				hasReplacement, err := ReplaceMisconfiguredProcessGroups(context.Background(), podmanager.StandardPodLifecycleManager{}, k8sClient, log, cluster, true)
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
					podName, _ := cluster.GetProcessGroupID(fdbv1beta2.ProcessClassStorage, 0)
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
					hasReplacement, err := ReplaceMisconfiguredProcessGroups(context.Background(), podmanager.StandardPodLifecycleManager{}, k8sClient, log, cluster, true)
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

var _ = DescribeTable("file_security_context_changed",
	func(desired, current *corev1.PodSpec, wantResult bool) {
		var log logr.Logger
		logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(GinkgoWriter)))
		result := fileSecurityContextChanged(desired, current, log)
		Expect(result).To(Equal(wantResult))
	},
	Entry("SecurityContext stays nil", &corev1.PodSpec{}, &corev1.PodSpec{}, false),
	Entry("SecurityContext turns nil from empty",
		&corev1.PodSpec{SecurityContext: &corev1.PodSecurityContext{}},
		&corev1.PodSpec{},
		false,
	),
	Entry("SecurityContext turns empty from nil",
		&corev1.PodSpec{},
		&corev1.PodSpec{SecurityContext: &corev1.PodSecurityContext{}},
		false,
	),
	Entry("FSGroup is added",
		&corev1.PodSpec{SecurityContext: &corev1.PodSecurityContext{FSGroup: new(int64)}},
		&corev1.PodSpec{SecurityContext: &corev1.PodSecurityContext{}},
		true,
	),
	Entry("FSGroup is removed",
		&corev1.PodSpec{SecurityContext: &corev1.PodSecurityContext{}},
		&corev1.PodSpec{SecurityContext: &corev1.PodSecurityContext{FSGroup: new(int64)}},
		true,
	),
	Entry("FSGroup is changed",
		&corev1.PodSpec{SecurityContext: &corev1.PodSecurityContext{FSGroup: pointer.Int64(42)}},
		&corev1.PodSpec{SecurityContext: &corev1.PodSecurityContext{FSGroup: new(int64)}},
		true,
	),
	Entry("FSGroupChangePolicy is changed",
		&corev1.PodSpec{SecurityContext: &corev1.PodSecurityContext{
			FSGroupChangePolicy: &[]corev1.PodFSGroupChangePolicy{corev1.FSGroupChangeAlways}[0]}},
		&corev1.PodSpec{SecurityContext: &corev1.PodSecurityContext{
			FSGroupChangePolicy: &[]corev1.PodFSGroupChangePolicy{corev1.FSGroupChangeOnRootMismatch}[0]}},
		true,
	),
	Entry("nothing is changed, empty settings",
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{},
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{}},
			}},
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{},
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{}},
			}},
		false,
	),
	Entry("only non-file related fields are added to container spec",
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{},
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{WindowsOptions: &corev1.WindowsSecurityContextOptions{HostProcess: new(bool)}}},
			}},
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{},
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{}},
			}},
		false,
	),
	Entry("only non-file related fields are changed on the container spec",
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{},
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{WindowsOptions: &corev1.WindowsSecurityContextOptions{HostProcess: new(bool)}}},
			}},
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{},
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{Privileged: new(bool)}},
			}},
		false,
	),
	Entry("only non-file related fields are removed from container spec",
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{},
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{}},
			}},
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{},
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{WindowsOptions: &corev1.WindowsSecurityContextOptions{HostProcess: new(bool)}}},
			}},
		false,
	),
	Entry("only non-file related fields are added to pod spec",
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{SupplementalGroups: []int64{1, 2, 3}},
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{}},
			}},
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{},
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{}},
			}},
		false,
	),
	Entry("only non-file related fields are removed from pod spec",
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{},
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{}},
			}},
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{SupplementalGroups: []int64{1, 2, 3}},
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{}},
			}},
		false,
	),
	Entry("only non-file related fields are changed on the pod spec",
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{SupplementalGroups: []int64{1, 2, 3}},
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{}},
			}},
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{SupplementalGroups: []int64{1, 2, 3, 4, 5}},
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{}},
			}},
		false,
	),
	Entry("RunAsUser is added to the pod spec",
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{RunAsUser: pointer.Int64(42)},
			Containers:      []corev1.Container{{}}}, // needs a "matching" container to compare effective settings
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{},
			Containers:      []corev1.Container{{}}},
		true,
	),
	Entry("RunAsUser is added to the container spec",
		&corev1.PodSpec{
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{RunAsUser: pointer.Int64(42)}}}},
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{},
			Containers:      []corev1.Container{{}}},
		true,
	),
	Entry("RunAsUser is removed from the container spec",
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{},
			Containers:      []corev1.Container{{}}},
		&corev1.PodSpec{
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{RunAsUser: pointer.Int64(42)}},
			}},
		true,
	),
	Entry("RunAsUser is removed from the container spec but not from the pod (no effective change)",
		&corev1.PodSpec{
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{RunAsUser: pointer.Int64(42)}},
			}},
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{RunAsUser: pointer.Int64(42)},
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{RunAsUser: pointer.Int64(42)}},
			}},
		false,
	),
	Entry("RunAsUser is changed on container spec",
		&corev1.PodSpec{
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{RunAsUser: pointer.Int64(111)}},
			}},
		&corev1.PodSpec{
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{RunAsUser: pointer.Int64(42)}},
			}},
		true,
	),
	Entry("RunAsGroup is changed on pod spec",
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{RunAsGroup: pointer.Int64(111)},
			Containers:      []corev1.Container{{}}},
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{RunAsGroup: pointer.Int64(42)},
			Containers:      []corev1.Container{{}}},
		true,
	),
	Entry("RunAsGroup is moved from podSpec to container spec",
		&corev1.PodSpec{
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{RunAsGroup: pointer.Int64(42)}},
			}},
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{RunAsGroup: pointer.Int64(42)},
			Containers:      []corev1.Container{{}}},
		false,
	),
	Entry("RunAsGroup is moved from container spec to podSpec",
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{RunAsGroup: pointer.Int64(42)},
			Containers:      []corev1.Container{{}}},
		&corev1.PodSpec{
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{RunAsGroup: pointer.Int64(42)}},
			}},
		false,
	),
	Entry("RunAsGroup is moved from podSpec to container spec and FSGroup changes",
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{
				FSGroup: pointer.Int64(42),
			},
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{
					RunAsGroup: pointer.Int64(42)}},
			}},
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{
				RunAsGroup: pointer.Int64(42)},
			Containers: []corev1.Container{{}}},
		true,
	),
	Entry("mix of changes (file and non-file related) that do not result in a change",
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{
				SupplementalGroups: []int64{5, 6},
			},
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{
					RunAsGroup: pointer.Int64(42)}},
			}},
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{
				RunAsGroup: pointer.Int64(42)},
			Containers: []corev1.Container{{}}},
		false,
	),
	Entry("mix of changes (file and non-file related) that result in a change",
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{
				SupplementalGroups: []int64{5, 6},
				FSGroup:            new(int64),
			},
			Containers: []corev1.Container{
				{SecurityContext: &corev1.SecurityContext{
					RunAsGroup: pointer.Int64(42)}},
			}},
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{
				RunAsGroup: pointer.Int64(42)},
			Containers: []corev1.Container{{}}},
		true,
	),
	// this is likely useless as I would assume that we would not be looking at replacing a pod with
	// no containers in the first place, but if we somehow are it seems better to not replace non-existent processes
	Entry("No containers exist and RunAsUser is added to the pod spec",
		&corev1.PodSpec{
			SecurityContext: &corev1.PodSecurityContext{RunAsUser: new(int64)},
			Containers:      []corev1.Container{{Name: "fdb"}},
		},
		&corev1.PodSpec{SecurityContext: &corev1.PodSecurityContext{}},
		false,
	),
)
