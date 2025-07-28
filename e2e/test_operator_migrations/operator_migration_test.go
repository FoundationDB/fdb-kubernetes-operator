/*
 * operator_migration_test.go
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

package operatormigration

/*
This test suite includes test that make sure that the migrations and exclusion strategy of the operator is working as
expected under different scenarios.
*/

import (
	"log"
	"strconv"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/e2e/fixtures"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

var (
	factory     *fixtures.Factory
	fdbCluster  *fixtures.FdbCluster
	testOptions *fixtures.FactoryOptions
)

func validateStorageClass(processClass fdbv1beta2.ProcessClass, targetStorageClass string) {
	Eventually(func() map[string]fdbv1beta2.None {
		storageClassNames := make(map[string]fdbv1beta2.None)
		volumeClaims := fdbCluster.GetVolumeClaimsForProcesses(processClass)
		for _, volumeClaim := range volumeClaims.Items {
			storageClassNames[*volumeClaim.Spec.StorageClassName] = fdbv1beta2.None{}
		}
		return storageClassNames
	}, 5*time.Minute).Should(Equal(map[string]fdbv1beta2.None{targetStorageClass: {}}))
}

func checkCoordinatorsTLSFlag(cluster *fdbv1beta2.FoundationDBCluster, listenOnTLS bool) {
	connectionString := cluster.Status.ConnectionString
	log.Println("connection string after conversion: ", connectionString)
	parsedConnectionString, err := fdbv1beta2.ParseConnectionString(connectionString)
	Expect(err).NotTo(HaveOccurred())

	for _, coordinator := range parsedConnectionString.Coordinators {
		if listenOnTLS {
			Expect(coordinator).To(HaveSuffix(":tls"))
		} else {
			Expect(coordinator).NotTo(HaveSuffix(":tls"))
		}
	}
}

func init() {
	testOptions = fixtures.InitFlags()
}

var _ = BeforeSuite(func() {
	factory = fixtures.CreateFactory(testOptions)
	fdbCluster = factory.CreateFdbCluster(
		fixtures.DefaultClusterConfig(false),
	)
	// Load some data into the cluster.
	factory.CreateDataLoaderIfAbsent(fdbCluster)
})

var _ = AfterSuite(func() {
	if CurrentSpecReport().Failed() {
		log.Printf("failed due to %s", CurrentSpecReport().FailureMessage())
	}
	factory.Shutdown()
})

var _ = Describe("Operator Migrations", Label("e2e", "pr"), func() {
	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			factory.DumpState(fdbCluster)
		}
		Expect(fdbCluster.WaitForReconciliation()).ToNot(HaveOccurred())
	})

	When("a migration is triggered and the namespace quota is limited", func() {
		prefix := "banana"
		var quota *corev1.ResourceQuota

		BeforeEach(func() {
			processCounts, err := fdbCluster.GetCluster().GetProcessCountsWithDefaults()
			Expect(err).NotTo(HaveOccurred())
			// Create Quota to limit the additional Pods that can be created to 5, the actual value here is 7 ,because we run
			// 2 Operator Pods.
			quota = &corev1.ResourceQuota{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testing-quota",
					Namespace: fdbCluster.Namespace(),
				},
				Spec: corev1.ResourceQuotaSpec{
					Hard: corev1.ResourceList{
						"count/pods": resource.MustParse(strconv.Itoa(processCounts.Total() + 7)),
					},
				},
			}
			Expect(factory.CreateIfAbsent(quota)).NotTo(HaveOccurred())
			Expect(fdbCluster.SetProcessGroupPrefix(prefix)).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			factory.Delete(quota)
		})

		It("should add the prefix to all instances", func() {
			lastForcedReconciliationTime := time.Now()
			forceReconcileDuration := 4 * time.Minute

			Eventually(func(g Gomega) bool {
				// Force a reconcile if needed to make sure we speed up the reconciliation if needed.
				if time.Since(lastForcedReconciliationTime) >= forceReconcileDuration {
					fdbCluster.ForceReconcile()
					lastForcedReconciliationTime = time.Now()
				}

				// Check if all process groups are migrated
				for _, processGroup := range fdbCluster.GetCluster().Status.ProcessGroups {
					if processGroup.IsMarkedForRemoval() && processGroup.IsExcluded() {
						continue
					}
					g.Expect(string(processGroup.ProcessGroupID)).To(HavePrefix(prefix))
				}

				return true
			}).WithTimeout(40 * time.Minute).WithPolling(5 * time.Second).Should(BeTrue())
			Expect(fdbCluster.WaitForReconciliation()).NotTo(HaveOccurred())
		})
	})

	When("changing the public IP source", func() {
		BeforeEach(func() {
			if fdbCluster.GetCluster().UseDNSInClusterFile() {
				Skip("using DNS and public IP from service is not tested")
			}

			log.Printf("set public IP source to %s", fdbv1beta2.PublicIPSourceService)
			Expect(
				fdbCluster.SetPublicIPSource(fdbv1beta2.PublicIPSourceService),
			).ShouldNot(HaveOccurred())
		})

		It("should change the public IP source and create/delete services", func() {
			Eventually(func() bool {
				pods := fdbCluster.GetPods()
				svcList := fdbCluster.GetServices()

				svcMap := make(map[string]struct{}, len(svcList.Items))
				for _, svc := range svcList.Items {
					svcMap[svc.Name] = struct{}{}
				}

				for _, pod := range pods.Items {
					if fdbv1beta2.PublicIPSource(
						pod.Annotations[fdbv1beta2.PublicIPAnnotation],
					) == fdbv1beta2.PublicIPSourcePod {
						continue
					}

					if _, ok := svcMap[pod.Name]; !ok {
						return false
					}

					delete(svcMap, pod.Name)
				}

				// We only expect one service here at the end since we run the cluster with a headless service.
				if fdbCluster.HasHeadlessService() {
					return len(svcMap) == 1
				}
				return len(svcMap) == 0
			}).Should(BeTrue())
		})

		AfterEach(func() {
			log.Printf("set public IP source to %s", fdbv1beta2.PublicIPSourcePod)
			Expect(
				fdbCluster.SetPublicIPSource(fdbv1beta2.PublicIPSourcePod),
			).ShouldNot(HaveOccurred())
			svcList := fdbCluster.GetServices()

			var expectedSvcCnt int
			if fdbCluster.HasHeadlessService() {
				expectedSvcCnt = 1
			}
			Expect(len(svcList.Items)).To(BeNumerically("==", expectedSvcCnt))
		})
	})

	When("changing the volume size", func() {
		var initialPods *corev1.PodList
		var newSize, initialStorageSize resource.Quantity

		BeforeEach(func() {
			var err error

			initialPods = fdbCluster.GetLogPods()
			// We use ProcessClassGeneral here because we are not setting any specific settings for the Log processes.
			initialStorageSize, err = fdbCluster.GetVolumeSize(fdbv1beta2.ProcessClassGeneral)
			Expect(err).NotTo(HaveOccurred())
			// Add 10G to the current size
			newSize = initialStorageSize.DeepCopy()
			newSize.Add(resource.MustParse("10G"))
			Expect(
				fdbCluster.SetVolumeSize(fdbv1beta2.ProcessClassGeneral, newSize),
			).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			Expect(
				fdbCluster.SetVolumeSize(fdbv1beta2.ProcessClassGeneral, initialStorageSize),
			).NotTo(HaveOccurred())
		})

		It("should replace all the log Pods and use the new volume size", func() {
			pods := fdbCluster.GetLogPods()
			Expect(pods.Items).NotTo(ContainElements(initialPods.Items))
			volumeClaims := fdbCluster.GetVolumeClaimsForProcesses(
				fdbv1beta2.ProcessClassLog,
			)
			Expect(len(volumeClaims.Items)).To(Equal(len(initialPods.Items)))
			for _, volumeClaim := range volumeClaims.Items {
				req := volumeClaim.Spec.Resources.Requests[corev1.ResourceStorage]
				Expect((&req).Value()).To(Equal(newSize.Value()))
			}
		})
	})

	When("the pod IP family is changed", func() {
		var initialPods []string
		var podIPFamily = fdbv1beta2.PodIPFamilyIPv4

		BeforeEach(func() {
			pods := fdbCluster.GetPods()
			for _, pod := range pods.Items {
				initialPods = append(initialPods, pod.Name)
			}

			spec := fdbCluster.GetCluster().Spec.DeepCopy()
			spec.Routing.PodIPFamily = ptr.To(podIPFamily)
			fdbCluster.UpdateClusterSpecWithSpec(spec)
			Expect(fdbCluster.WaitForReconciliation()).To(Succeed())
		})

		AfterEach(func() {
			spec := fdbCluster.GetCluster().Spec.DeepCopy()
			spec.Routing.PodIPFamily = nil
			fdbCluster.UpdateClusterSpecWithSpec(spec)
			Expect(
				fdbCluster.WaitForReconciliation(fixtures.SoftReconcileOption(true)),
			).To(Succeed())
		})

		It("should replace all pods and configure them properly", func() {
			pods := fdbCluster.GetPods()
			podIPFamilyString := strconv.Itoa(podIPFamily)
			var expectedContainerWithEnv string
			// In the case of the split image the sidecar will have that env variable.
			if fdbCluster.GetCluster().UseUnifiedImage() {
				expectedContainerWithEnv = fdbv1beta2.MainContainerName
			} else {
				expectedContainerWithEnv = fdbv1beta2.SidecarContainerName
			}

			newPods := make([]string, 0, len(pods.Items))
			for _, pod := range pods.Items {
				if !pod.DeletionTimestamp.IsZero() {
					continue
				}

				if pod.Status.Phase != corev1.PodRunning {
					log.Println(
						"ignoring pod:",
						pod.Name,
						"with pod phase",
						pod.Status.Phase,
						"message:",
						pod.Status.Message,
					)
					continue
				}

				var checked bool

				for _, container := range pod.Spec.Containers {
					if container.Name != expectedContainerWithEnv {
						continue
					}

					// Make sure the FDB_PUBLIC_IP env variable is set.
					for _, env := range container.Env {
						if env.Name == fdbv1beta2.EnvNamePublicIP {
							checked = true
							break
						}
					}

					Expect(checked).To(BeTrue())
					break
				}

				newPods = append(newPods, pod.Name)
				Expect(
					pod.Annotations,
				).To(HaveKeyWithValue(fdbv1beta2.IPFamilyAnnotation, podIPFamilyString))
			}

			Expect(newPods).NotTo(ContainElements(initialPods))

		})
	})

	When("maxConcurrentReplacements is lower than the number of storage pods", func() {
		var initialConcurrentReplacements *int
		var initialPodUpdateStrategy fdbv1beta2.PodUpdateStrategy
		var initialReplaceInstancesWhenResourcesChange *bool

		BeforeEach(func() {
			// Remember the current settings before updating the spec
			initialConcurrentReplacements = fdbCluster.GetCluster().Spec.AutomationOptions.MaxConcurrentReplacements
			initialPodUpdateStrategy = fdbCluster.GetClusterSpec().AutomationOptions.PodUpdateStrategy
			initialReplaceInstancesWhenResourcesChange = fdbCluster.GetCluster().Spec.ReplaceInstancesWhenResourcesChange
			// Allow to replacement of 3 pods concurrently, as there are 5 storage servers, there need to be at least 2 rounds of replacements to replace all.
			spec := fdbCluster.GetCluster().Spec.DeepCopy()
			spec.AutomationOptions.MaxConcurrentReplacements = ptr.To(3)
			spec.AutomationOptions.PodUpdateStrategy = fdbv1beta2.PodUpdateStrategyDelete
			spec.ReplaceInstancesWhenResourcesChange = ptr.To(true)
			fdbCluster.UpdateClusterSpecWithSpec(spec)
		})

		AfterEach(func() {
			// Reset to the initial settings
			spec := fdbCluster.GetCluster().Spec.DeepCopy()
			spec.AutomationOptions.MaxConcurrentReplacements = initialConcurrentReplacements
			spec.AutomationOptions.PodUpdateStrategy = initialPodUpdateStrategy
			spec.ReplaceInstancesWhenResourcesChange = initialReplaceInstancesWhenResourcesChange
			fdbCluster.UpdateClusterSpecWithSpec(spec)
		})

		When("a change that requires a replacement of all storage pods", func() {
			var initialVolumeClaims []types.UID
			var newCPURequest, initialCPURequest resource.Quantity

			BeforeEach(func() {
				initialVolumeClaims = fdbCluster.GetListOfUIDsFromVolumeClaims(
					fdbv1beta2.ProcessClassStorage,
				)
				spec := fdbCluster.GetCluster().Spec.DeepCopy()
				initialCPURequest = spec.Processes[fdbv1beta2.ProcessClassStorage].PodTemplate.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU]
				newCPURequest = initialCPURequest.DeepCopy()

				// An increase in request requires a replacement when ReplaceInstancesWhenResourcesChange is set to true
				newCPURequest.Add(resource.MustParse("1m"))
				spec.Processes[fdbv1beta2.ProcessClassStorage].PodTemplate.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = newCPURequest
				fdbCluster.UpdateClusterSpecWithSpec(spec)

				// Wait for the reconciliation to finish
				Expect(fdbCluster.WaitForReconciliation()).NotTo(HaveOccurred())
			})

			AfterEach(func() {
				// Undo the change to cpu requests
				spec := fdbCluster.GetCluster().Spec.DeepCopy()
				spec.Processes[fdbv1beta2.ProcessClassStorage].PodTemplate.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = initialCPURequest
				fdbCluster.UpdateClusterSpecWithSpec(spec)

				// Wait for the reconciliation to finish
				Expect(fdbCluster.WaitForReconciliation()).NotTo(HaveOccurred())
			})

			It("should replace all storage pods", func() {
				// A replacement of a storage pod will create a new PVC. After reconciliation the set of PVCs should be completely changed.
				Expect(
					initialVolumeClaims,
				).NotTo(ContainElements(fdbCluster.GetListOfUIDsFromVolumeClaims(fdbv1beta2.ProcessClassStorage)), "PVC should not be present in the new set of PVCs")
			})
		})
	})

	When("migrating a cluster to make use of DNS in the cluster file", func() {
		BeforeEach(func() {
			if fdbCluster.GetCluster().UseDNSInClusterFile() {
				Skip("cluster already uses DNS")
			}

			Expect(fdbCluster.SetUseDNSInClusterFile(true)).ToNot(HaveOccurred())
		})

		It("should migrate the cluster", func() {
			cluster := fdbCluster.GetCluster()
			Eventually(func() string {
				return fdbCluster.GetStatus().Cluster.ConnectionString
			}).Should(ContainSubstring(cluster.GetDNSDomain()))
		})

		AfterEach(func() {
			Expect(fdbCluster.SetUseDNSInClusterFile(false)).ToNot(HaveOccurred())
		})
	})

	When("Migrating a cluster to a different storage class", func() {
		var defaultStorageClass, targetStorageClass string

		BeforeEach(func() {
			// This will only return StorageClasses that have a label foundationdb.org/operator-testing=true defined.
			storageClasses := factory.GetStorageClasses(map[string]string{
				"foundationdb.org/operator-testing": "true",
			})
			if len(storageClasses.Items) < 2 {
				Skip("This test requires at least two available StorageClasses")
			}

			defaultStorageClass = factory.GetDefaultStorageClass()
			// Select all StorageClasses that are not the default one as candidate.
			candidates := make([]string, 0, len(storageClasses.Items))
			for _, storageClass := range storageClasses.Items {
				if storageClass.Name == defaultStorageClass {
					continue
				}

				candidates = append(candidates, storageClass.Name)
			}

			targetStorageClass = candidates[factory.Intn(len(candidates))]

			Expect(fdbCluster.UpdateStorageClass(
				targetStorageClass,
				fdbv1beta2.ProcessClassLog,
			)).NotTo(HaveOccurred())
		})

		It("should migrate the cluster", func() {
			validateStorageClass(fdbv1beta2.ProcessClassLog, targetStorageClass)
		})

		AfterEach(func() {
			if defaultStorageClass != "" {
				Expect(fdbCluster.UpdateStorageClass(
					defaultStorageClass,
					fdbv1beta2.ProcessClassLog,
				)).NotTo(HaveOccurred())
			}
		})
	})

	When("Changing the TLS setting", func() {
		var initialTLSSetting bool

		BeforeEach(func() {
			initialTLSSetting = fdbCluster.GetCluster().Spec.MainContainer.EnableTLS
		})

		AfterEach(func() {
			Expect(
				fdbCluster.SetTLS(
					initialTLSSetting,
					fdbCluster.GetCluster().Spec.SidecarContainer.EnableTLS,
				),
			).NotTo(HaveOccurred())
			Expect(fdbCluster.HasTLSEnabled()).To(Equal(initialTLSSetting))
			checkCoordinatorsTLSFlag(fdbCluster.GetCluster(), initialTLSSetting)
		})

		When("the pod spec stays the same", func() {
			It("should update the TLS setting  and keep the cluster available", func() {
				// Only change the TLS setting for the cluster and not for the sidecar otherwise we have to recreate
				// all Pods which takes a long time since we recreate the Pods one by one.
				Expect(
					fdbCluster.SetTLS(
						!initialTLSSetting,
						fdbCluster.GetCluster().Spec.SidecarContainer.EnableTLS,
					),
				).NotTo(HaveOccurred())
				Expect(fdbCluster.HasTLSEnabled()).To(Equal(!initialTLSSetting))
				checkCoordinatorsTLSFlag(fdbCluster.GetCluster(), !initialTLSSetting)
			})
		})

		When("the pod spec is changed", func() {
			It("should update the TLS setting  and keep the cluster available", func() {
				spec := fdbCluster.GetCluster().Spec.DeepCopy()
				spec.MainContainer.EnableTLS = !initialTLSSetting

				// Add a new env variable to ensure this will cause some additional replacements.
				processSettings := spec.Processes[fdbv1beta2.ProcessClassGeneral]
				for i, container := range processSettings.PodTemplate.Spec.Containers {
					if container.Name != fdbv1beta2.MainContainerName {
						continue
					}

					container.Env = append(container.Env, corev1.EnvVar{
						Name:  "TESTING_TLS_CHANGE",
						Value: "EMPTY",
					})

					processSettings.PodTemplate.Spec.Containers[i] = container
					break
				}

				spec.Processes[fdbv1beta2.ProcessClassGeneral] = processSettings

				fdbCluster.UpdateClusterSpecWithSpec(spec)
				Expect(fdbCluster.WaitForReconciliation()).To(Succeed())
				Expect(fdbCluster.HasTLSEnabled()).To(Equal(!initialTLSSetting))
				checkCoordinatorsTLSFlag(fdbCluster.GetCluster(), !initialTLSSetting)
			})
		})
	})

	// TODO (johscheuer): Enable once the CRD in the CI setup is updated.
	PWhen("migrating the storage engine", func() {
		var newStorageEngine fdbv1beta2.StorageEngine

		BeforeEach(func() {
			spec := fdbCluster.GetCluster().Spec.DeepCopy()
			initialEngine := spec.DatabaseConfiguration.NormalizeConfiguration(
				fdbCluster.GetCluster(),
			).StorageEngine
			log.Println("initialEngine", initialEngine)
			if initialEngine == fdbv1beta2.StorageEngineSSD2 {
				newStorageEngine = fdbv1beta2.StorageEngineRocksDbV1
			} else {
				newStorageEngine = fdbv1beta2.StorageEngineSSD2
			}

			migrationType := fdbv1beta2.StorageMigrationTypeGradual
			spec.DatabaseConfiguration.PerpetualStorageWiggleLocality = nil
			spec.DatabaseConfiguration.StorageMigrationType = &migrationType
			spec.DatabaseConfiguration.PerpetualStorageWiggle = ptr.To(1)
			spec.DatabaseConfiguration.StorageEngine = newStorageEngine
			fdbCluster.UpdateClusterSpecWithSpec(spec)
		})

		It("should add the prefix to all instances", func() {
			lastForcedReconciliationTime := time.Now()
			forceReconcileDuration := 4 * time.Minute

			Eventually(func() fdbv1beta2.StorageEngine {
				// Force a reconcile if needed to make sure we speed up the reconciliation if needed.
				if time.Since(lastForcedReconciliationTime) >= forceReconcileDuration {
					fdbCluster.ForceReconcile()
					lastForcedReconciliationTime = time.Now()
				}

				return fdbCluster.GetStatus().Cluster.DatabaseConfiguration.StorageEngine
			}).WithTimeout(40 * time.Minute).WithPolling(5 * time.Second).Should(Equal(newStorageEngine))
			Expect(fdbCluster.WaitForReconciliation()).NotTo(HaveOccurred())
		})
	})
})
