/*
 * update_pdbs_test.go
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

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
)

var _ = Describe("update_pdbs", func() {

	var cluster *fdbtypes.FoundationDBCluster
	var err error
	var shouldContinue bool
	var originalCount int
	var pdbs *policyv1beta1.PodDisruptionBudgetList

	BeforeEach(func() {
		cluster = createDefaultCluster()
		enabled := true
		cluster.Spec.EnablePodDisruptionBudget = &enabled

		err = k8sClient.Create(context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())

		result, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeFalse())

		generation, err := reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(generation).To(Equal(int64(1)))

		pdbs = &policyv1beta1.PodDisruptionBudgetList{}
		err = k8sClient.List(context.TODO(), pdbs)
		Expect(err).NotTo(HaveOccurred())

		originalCount = len(pdbs.Items)
		Expect(originalCount).To(Equal(4))
	})

	JustBeforeEach(func() {
		shouldContinue, err = UpdatePDBs{}.Reconcile(clusterReconciler, context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())
		_, err = reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())

		pdbs = &policyv1beta1.PodDisruptionBudgetList{}
		err = k8sClient.List(context.TODO(), pdbs)
		Expect(err).NotTo(HaveOccurred())
		sort.Slice(pdbs.Items, func(i1, i2 int) bool {
			return pdbs.Items[i1].Name < pdbs.Items[i2].Name
		})
	})

	Context("with a reconciled cluster", func() {
		It("should not requeue", func() {
			Expect(shouldContinue).To(BeTrue())
		})

		It("should leave the PDBs", func() {
			Expect(pdbs.Items).To(HaveLen(originalCount))
			Expect(pdbs.Items[0].Name).To(Equal("operator-test-1-cluster-controller"))
		})
	})

	Context("with an increase to the storage process count", func() {
		BeforeEach(func() {
			cluster.Spec.ProcessCounts.Storage = 5
		})

		It("should update the storage PDB", func() {
			Expect(len(pdbs.Items)).To(BeNumerically(">", 3))
			pdb := pdbs.Items[3]
			Expect(pdb.Labels[FDBProcessClassLabel]).To(Equal("storage"))
			Expect(pdb.Spec.MinAvailable).NotTo(BeNil())
			Expect(pdb.Spec.MinAvailable.IntVal).To(Equal(int32(4)))
		})
	})

	Context("when disabling PDBs", func() {
		BeforeEach(func() {
			enabled := false
			cluster.Spec.EnablePodDisruptionBudget = &enabled
		})

		It("should remove the PDBs", func() {
			Expect(pdbs.Items).To(HaveLen(0))
		})
	})

	Context("when disabling a process type", func() {
		BeforeEach(func() {
			cluster.Spec.ProcessCounts.ClusterController = -1
		})

		It("should remove the PDB", func() {
			Expect(pdbs.Items).To(HaveLen(originalCount - 1))
			Expect(pdbs.Items[0].Name).To(Equal("operator-test-1-log"))
		})
	})
})
