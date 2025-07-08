/*
 * replace_misconfigured_process_groups_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2019-2025 Apple Inc. and the FoundationDB project authors
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
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("replace_misconfigured_process_groups", func() {
	When("a version incompatible upgrade is ongoing", func() {
		var cluster *fdbv1beta2.FoundationDBCluster

		BeforeEach(func() {
			cluster = internal.CreateDefaultCluster()
			Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())
			result, err := reconcileCluster(cluster)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeFalse())
			Expect(k8sClient.Get(context.TODO(), ctrlClient.ObjectKeyFromObject(cluster), cluster)).NotTo(HaveOccurred())

			version, err := fdbv1beta2.ParseFdbVersion(cluster.Spec.Version)
			Expect(err).NotTo(HaveOccurred())
			cluster.Spec.Version = version.NextMinorVersion().String()
		})

		It("should not perform any replacements and requeue", func() {
			result := replaceMisconfiguredProcessGroups{}.reconcile(context.Background(), clusterReconciler, cluster, nil, testLogger)
			Expect(result).NotTo(BeNil())
			Expect(result.delayedRequeue).To(BeTrue())
			Expect(result.message).To(Equal("Replacements because of misconfiguration are skipped because of an ongoing version incompatible upgrade"))
		})
	})
})
