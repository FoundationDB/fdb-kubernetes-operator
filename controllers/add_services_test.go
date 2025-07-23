/*
 * add_services_test.go
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
	"context"
	"sort"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"k8s.io/utils/ptr"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("add_services", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var err error
	var requeue *requeue
	var initialServices *corev1.ServiceList
	var newServices *corev1.ServiceList

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		source := fdbv1beta2.PublicIPSourceService
		cluster.Spec.Routing.PublicIPSource = &source
		cluster.Spec.Routing.HeadlessService = ptr.To(true)
		Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())

		result, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		generation, err := reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(generation).To(Equal(int64(1)))

		initialServices = &corev1.ServiceList{}
		Expect(k8sClient.List(context.TODO(), initialServices)).NotTo(HaveOccurred())

		Expect(
			internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{}),
		).NotTo(HaveOccurred())
	})

	JustBeforeEach(func() {
		requeue = addServices{}.reconcile(
			context.TODO(),
			clusterReconciler,
			cluster,
			nil,
			globalControllerLogger,
		)
		_, err = reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())

		newServices = &corev1.ServiceList{}
		Expect(k8sClient.List(context.TODO(), newServices)).NotTo(HaveOccurred())
		sort.Slice(newServices.Items, func(i1, i2 int) bool {
			return newServices.Items[i1].Name < newServices.Items[i2].Name
		})
	})

	Context("with a reconciled cluster", func() {
		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should not create any services", func() {
			Expect(newServices.Items).To(HaveLen(len(initialServices.Items)))
		})

		Context("with a change to the match labels", func() {
			BeforeEach(func() {
				cluster.Spec.LabelConfig.MatchLabels = map[string]string{
					fdbv1beta2.FDBClusterLabel: cluster.Name,
					"fdb-test-label":           "true",
				}
			})

			It("should not create any services", func() {
				Expect(newServices.Items).To(HaveLen(len(initialServices.Items)))
			})

			It("should set the selector on the services", func() {
				for _, service := range newServices.Items {
					labels := map[string]string{
						fdbv1beta2.FDBClusterLabel: cluster.Name,
						"fdb-test-label":           "true",
					}
					if service.ObjectMeta.Labels[fdbv1beta2.FDBProcessGroupIDLabel] != "" {
						labels[fdbv1beta2.FDBProcessGroupIDLabel] = service.ObjectMeta.Labels[fdbv1beta2.FDBProcessGroupIDLabel]
					}
					Expect(service.Spec.Selector).To(Equal(labels))
				}
			})

			It("should set the metadata on the services", func() {
				for _, service := range newServices.Items {
					Expect(service.Labels["fdb-test-label"]).To(Equal("true"))
				}
			})
		})
	})

	Context("with a process group with no service defined", func() {
		var newProcessGroupID fdbv1beta2.ProcessGroupID
		var pickedProcessGroup *fdbv1beta2.ProcessGroupStatus

		BeforeEach(func() {
			_, processGroupIDs, err := cluster.GetCurrentProcessGroupsAndProcessCounts()
			Expect(err).NotTo(HaveOccurred())
			newProcessGroupID = cluster.GetNextRandomProcessGroupID(
				fdbv1beta2.ProcessClassStorage,
				processGroupIDs[fdbv1beta2.ProcessClassStorage],
			)
			pickedProcessGroup = fdbv1beta2.NewProcessGroupStatus(
				newProcessGroupID,
				fdbv1beta2.ProcessClassStorage,
				nil,
			)
			cluster.Status.ProcessGroups = append(cluster.Status.ProcessGroups, pickedProcessGroup)
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should create an extra service", func() {
			Expect(newServices.Items).To(HaveLen(len(initialServices.Items) + 1))

			var checked bool
			serviceName := cluster.Name + "-" + string(newProcessGroupID)
			for _, svc := range newServices.Items {
				if svc.Name != serviceName {
					continue
				}

				Expect(
					svc.Labels[fdbv1beta2.FDBProcessGroupIDLabel],
				).To(Equal(string(newProcessGroupID)))
				Expect(
					svc.Labels[fdbv1beta2.FDBProcessClassLabel],
				).To(Equal(string(fdbv1beta2.ProcessClassStorage)))
				Expect(
					svc.OwnerReferences,
				).To(Equal(internal.BuildOwnerReference(cluster.TypeMeta, cluster.ObjectMeta)))
				Expect(svc.Spec.ClusterIP).NotTo(Equal("None"))
				checked = true
			}

			Expect(checked).To(BeTrue())
		})

		Context("with the pod public IP source", func() {
			BeforeEach(func() {
				source := fdbv1beta2.PublicIPSourcePod
				cluster.Spec.Routing.PublicIPSource = &source
			})

			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should not create any services", func() {
				Expect(newServices.Items).To(HaveLen(len(initialServices.Items)))
			})
		})

		When("the process group is being removed", func() {
			BeforeEach(func() {
				pickedProcessGroup.MarkForRemoval()
			})

			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should create the services", func() {
				Expect(newServices.Items).To(HaveLen(len(initialServices.Items) + 1))
			})

			When("the process group is fully excluded", func() {
				BeforeEach(func() {
					pickedProcessGroup.SetExclude()
				})

				It("should not requeue", func() {
					Expect(requeue).To(BeNil())
				})

				It("should not create any services", func() {
					Expect(newServices.Items).To(HaveLen(len(initialServices.Items)))
				})
			})
		})
	})

	Context("with no headless service", func() {
		BeforeEach(func() {
			service := &corev1.Service{}
			Expect(
				k8sClient.Get(
					context.TODO(),
					types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name},
					service,
				),
			).NotTo(HaveOccurred())
			Expect(k8sClient.Delete(context.TODO(), service)).NotTo(HaveOccurred())
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should create a headless service", func() {
			Expect(newServices.Items).To(HaveLen(len(initialServices.Items)))

			firstService := newServices.Items[0]
			Expect(firstService.Name).To(Equal("operator-test-1"))
			Expect(firstService.Labels[fdbv1beta2.FDBProcessGroupIDLabel]).To(Equal(""))
			Expect(firstService.Spec.ClusterIP).To(Equal("None"))
		})

		Context("with the headless service disabled", func() {
			BeforeEach(func() {
				cluster.Spec.Routing.UseDNSInClusterFile = ptr.To(false)
				cluster.Spec.Routing.HeadlessService = ptr.To(false)
			})

			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should not create any services", func() {
				Expect(newServices.Items).To(HaveLen(len(initialServices.Items) - 1))
			})
		})
	})

	Context("with the podIPFamily 6", func() {
		BeforeEach(func() {
			cluster.Spec.Routing.PodIPFamily = ptr.To(6)
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should recreate the same services", func() {
			Expect(newServices.Items).To(HaveLen(len(initialServices.Items)))
			for _, newService := range newServices.Items {
				Expect(newService.Spec.IPFamilies).To(HaveLen(1))
				Expect(newService.Spec.IPFamilies[0]).To(Equal(corev1.IPv6Protocol))
			}
		})
	})
})
