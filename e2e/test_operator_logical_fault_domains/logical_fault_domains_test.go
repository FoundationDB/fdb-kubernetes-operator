/*
 * logical_fault_domains_test.go
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

package operator

/*

This test suite contains test to validate the correct behaviour for the logical fault domain setup. The suite will test
different cases that could be observed in real FoundationDB clusters, like enabling logical fault domains and changing
the number of logical fault domains.
*/

import (
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/fixtures"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"
	"log"
)

var (
	factory     *fixtures.Factory
	fdbCluster  *fixtures.FdbCluster
	testOptions *fixtures.FactoryOptions
)

func init() {
	testOptions = fixtures.InitFlags()
}

var _ = BeforeSuite(func() {
	factory = fixtures.CreateFactory(testOptions)

	clusterOptions := factory.GetClusterOptions()
	clusterOptions = append(clusterOptions, fixtures.WithHostsAsFailureDomain)
	fdbCluster = factory.CreateFdbCluster(
		fixtures.DefaultClusterConfig(false),
		clusterOptions...,
	)
})

var _ = AfterSuite(func() {
	if CurrentSpecReport().Failed() {
		log.Printf("failed due to %s", CurrentSpecReport().FailureMessage())
	}
	factory.Shutdown()
})

var _ = PDescribe("Logical Fault Domains", Label("e2e"), func() {
	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			factory.DumpState(fdbCluster)
		}
		Expect(fdbCluster.WaitForReconciliation()).ToNot(HaveOccurred())
		factory.StopInvariantCheck()
	})

	When("the cluster is not using logical fault domains", func() {
		It("should add the fault domains to the process group status", func() {
			pods := fdbCluster.GetPods()

			// Fetch all unique nodes of this cluster.
			uniqueNodes := make(map[string]fdbv1beta2.None)
			for _, pod := range pods.Items {
				uniqueNodes[pod.Spec.NodeName] = fdbv1beta2.None{}
			}

			cluster := fdbCluster.GetCluster()

			// Fetch all fault domains.
			faultDomains := make(map[string]int)
			for _, processGroup := range cluster.Status.ProcessGroups {
				Expect(processGroup.FaultDomain).NotTo(BeEmpty())
				faultDomains[processGroup.FaultDomain]++
			}

			// Fault domains should equal the unique nodes we have.
			Expect(faultDomains).To(HaveLen(len(uniqueNodes)))
		})
	})

	When("enabling logical fault domains", func() {
		var initialPods *corev1.PodList

		BeforeEach(func() {
			initialPods = fdbCluster.GetPods()
			fdbCluster.SetDistributionConfig(fdbv1beta2.DistributionConfig{
				Enabled: pointer.Bool(true),
			})
		})

		// Disable logical fault domains again.
		AfterEach(func() {
			fdbCluster.SetDistributionConfig(fdbv1beta2.DistributionConfig{
				Enabled: pointer.Bool(false),
			})
		})

		It("should enable logical fault domains", func() {
			status := fdbCluster.GetStatus()
			_ = status
			cluster := fdbCluster.GetCluster()

			// check processes are replaced
			_ = initialPods

			faultDomains := make(map[string]int)
			for _, processGroup := range cluster.Status.ProcessGroups {
				log.Println("fault-domains", processGroup.ProcessGroupID, "-", processGroup.FaultDomain)
				Expect(processGroup.FaultDomain).NotTo(BeEmpty())
				faultDomains[processGroup.FaultDomain]++
			}

			Expect(faultDomains).To(HaveLen(9))

			// TODO check actual content --> Ensure Pods are not running on the same nodes
		})
	})

	/*
		TODO add those test cases:

		 - Shrink number of logical fault domains
		 - Grow number of logical fault domains
		 - Change logical fault domain prefix
		 - Doing a replacement with logical FT enabled.
	*/
})
