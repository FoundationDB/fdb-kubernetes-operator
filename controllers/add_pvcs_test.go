/*
 * add_pvcs_test.go
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

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("add_pvcs", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var err error
	var requeue *requeue
	var initialPVCs *corev1.PersistentVolumeClaimList
	var newPVCs *corev1.PersistentVolumeClaimList

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		Expect(
			internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{}),
		).NotTo(HaveOccurred())
		Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())

		result, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).To(BeZero())

		generation, err := reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(generation).To(Equal(int64(1)))

		initialPVCs = &corev1.PersistentVolumeClaimList{}
		Expect(k8sClient.List(context.TODO(), initialPVCs)).NotTo(HaveOccurred())
	})

	JustBeforeEach(func() {
		requeue = addPVCs{}.reconcile(
			context.TODO(),
			clusterReconciler,
			cluster,
			nil,
			globalControllerLogger,
		)
		Expect(err).NotTo(HaveOccurred())
		_, err = reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())

		newPVCs = &corev1.PersistentVolumeClaimList{}
		Expect(k8sClient.List(context.TODO(), newPVCs)).NotTo(HaveOccurred())
		sort.Slice(newPVCs.Items, func(i1, i2 int) bool {
			return newPVCs.Items[i1].Name < newPVCs.Items[i2].Name
		})
	})

	Context("with a reconciled cluster", func() {
		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should not create any PVCs", func() {
			Expect(newPVCs.Items).To(HaveLen(len(initialPVCs.Items)))
		})
	})

	Context("with a storage process group with no PVC defined", func() {
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

		It("should create an extra PVC", func() {
			var checked bool
			pvcName := cluster.Name + "-" + string(newProcessGroupID) + "-data"
			for _, pvc := range newPVCs.Items {
				if pvc.Name != pvcName {
					continue
				}

				Expect(
					pvc.Labels[fdbv1beta2.FDBProcessGroupIDLabel],
				).To(Equal(string(newProcessGroupID)))
				Expect(
					pvc.Labels[fdbv1beta2.FDBProcessClassLabel],
				).To(Equal(string(fdbv1beta2.ProcessClassStorage)))
				Expect(
					pvc.OwnerReferences,
				).To(Equal(internal.BuildOwnerReference(cluster.TypeMeta, cluster.ObjectMeta)))
				checked = true
			}

			Expect(checked).To(BeTrue())
		})

		When("the process group is being removed", func() {
			BeforeEach(func() {
				pickedProcessGroup.MarkForRemoval()
			})

			When("the process is not excluded", func() {
				It("should not requeue", func() {
					Expect(requeue).To(BeNil())
				})

				It("should create the PVCs", func() {
					Expect(newPVCs.Items).To(HaveLen(len(initialPVCs.Items) + 1))
				})
			})

			When("the process is fully excluded", func() {
				BeforeEach(func() {
					pickedProcessGroup.SetExclude()
				})

				It("should not requeue", func() {
					Expect(requeue).To(BeNil())
				})

				It("should not create any PVCs", func() {
					Expect(newPVCs.Items).To(HaveLen(len(initialPVCs.Items)))
				})
			})
		})
	})

	Context("with a stateless process group with no PVC defined", func() {
		BeforeEach(func() {
			_, processGroupIDs, err := cluster.GetCurrentProcessGroupsAndProcessCounts()
			Expect(err).NotTo(HaveOccurred())
			newProcessGroupID := cluster.GetNextRandomProcessGroupID(
				fdbv1beta2.ProcessClassStateless,
				processGroupIDs[fdbv1beta2.ProcessClassStateless],
			)
			cluster.Status.ProcessGroups = append(
				cluster.Status.ProcessGroups,
				fdbv1beta2.NewProcessGroupStatus(
					newProcessGroupID,
					fdbv1beta2.ProcessClassStateless,
					nil,
				),
			)
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should not create an extra PVC", func() {
			Expect(newPVCs.Items).To(HaveLen(len(initialPVCs.Items)))
		})
	})
})
