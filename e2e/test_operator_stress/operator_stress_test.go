/*
 * operator_stress_test.go
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

package operatorstress

/*
This test suite includes tests that run the same operation multiple times to make sure those operations are robust and reliable.
Currently this test suite checks the replacement of Pods and the cluster creation.
*/

import (
	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/fixtures"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	factory     *fixtures.Factory
	testOptions *fixtures.FactoryOptions
)

func init() {
	testOptions = fixtures.InitFlags()
}

var _ = BeforeSuite(func() {
	factory = fixtures.CreateFactory(testOptions)
})

var _ = AfterSuite(func() {
	factory.Shutdown()
})

var _ = Describe("Operator Stress", Label("e2e"), func() {
	When("creating and deleting a cluster multiple times", func() {
		It("should create a healthy and available cluster", func() {
			// Since Ginkgo doesn't support what we want, we run this multiple times.
			// We create and delete a cluster 10 times to ensure we don't have any flaky behaviour in the operator.
			for i := 0; i < 10; i++ {
				fdbCluster := factory.CreateFdbCluster(
					fixtures.DefaultClusterConfig(false),
					factory.GetClusterOptions()...,
				)
				Expect(fdbCluster.IsAvailable()).To(BeTrue())
				Expect(fdbCluster.Destroy()).NotTo(HaveOccurred())
			}
		})
	})

	When("replacing processes in a continuously manner", func() {
		var fdbCluster *fixtures.FdbCluster

		BeforeEach(func() {
			fdbCluster = factory.CreateFdbCluster(
				fixtures.DefaultClusterConfig(false),
				factory.GetClusterOptions()...,
			)
			Expect(fdbCluster.InvariantClusterStatusAvailable()).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			Expect(fdbCluster.Destroy()).NotTo(HaveOccurred())
		})

		It("should replace the targeted Pod", func() {
			// Since Ginkgo doesn't support what we want, we run this multiple times.
			for i := 0; i < 10; i++ {
				Expect(fdbCluster.ClearProcessGroupsToRemove()).ShouldNot(HaveOccurred())
				pod := factory.ChooseRandomPod(fdbCluster.GetPods())
				fdbCluster.ReplacePod(*pod, true)
			}
		})
	})
})
