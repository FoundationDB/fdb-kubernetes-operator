/*
 * maintenance_mode_checker_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2022 Apple Inc. and the FoundationDB project authors
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
	"k8s.io/utils/pointer"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
)

// TODO (johscheuer): Modify tests to represent different scenarios with the new maintenance integration.
var _ = Describe("maintenance_mode_checker", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var err error
	//var requeue *requeue
	//targetProcessGroup := "operator-test-1-storage-1"

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		// TODO: Also test the Reset option
		cluster.Spec.AutomationOptions.MaintenanceModeOptions.UseMaintenanceModeChecker = pointer.Bool(true)
		Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())

		result, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeFalse())

		generation, err := reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(generation).To(Equal(int64(1)))
	})

	JustBeforeEach(func() {
		//requeue = maintenanceModeChecker{}.reconcile(context.TODO(), clusterReconciler, cluster, nil, globalControllerLogger)
		//Expect(err).NotTo(HaveOccurred())
		_, err = reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
	})
})
