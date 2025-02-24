/*
 * operator_creation_velocity_test.go
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

package operatorcreationvelocity

/*
This test suite includes tests to validate the creation speed of single and HA FoundationDB clusters.
Those tests will printout how long the cluster creation took including an overview how long the different steps during creation took.

The output for a single cluster creation could look like this:

reconciled name=fdb-cluster-ry9qwphz, namespace=jscheuermann-ug1bmlj5
step ProcessGroupCreation took 182.054625ms
step PodCreation took 186.665708ms
step PodsRunning took 1m20.8948455s
step CoordinatorSelection took 35.708µs
step ConfigMapSync took 1m49.643362166s
step DatabaseConfiguration took 17.583µs
step FullyReconcile took 1.584µs
Single-DC cluster creation took:  3m14.722935375s

The output shows that the longest time the operator was waiting for the Pods to get into the running state and to synchronize the ConfigMap to all Pods.

Currently the test have no validation on how long the cluster creation took, since this is mostly influenced on how long the Pod scheduling and getting the Pod into a running state takes.
*/

import (
	"log"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/v2/e2e/fixtures"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func init() {
	testOptions = fixtures.InitFlags()
}

var (
	factory     *fixtures.Factory
	namespace   string
	testOptions *fixtures.FactoryOptions
)

var _ = BeforeSuite(func() {
	factory = fixtures.CreateFactory(testOptions)
	// Create a namespace and wait until the operator Pods are running. We don't want to measure that time.
	namespace = factory.SingleNamespace()
	factory.WaitUntilOperatorPodsRunning(namespace)
})

var _ = AfterSuite(func() {
	factory.Shutdown()
})

var _ = Describe("Test Operator Velocity", Label("e2e"), func() {
	When("creating a single FDB cluster", func() {
		It("should roll out knob changes within expected time", func() {
			startTime := time.Now()

			fdbCluster := factory.CreateFdbCluster(
				&fixtures.ClusterConfig{
					Namespace:       namespace,
					CreationTracker: fixtures.NewDefaultCreationTrackerLogger(),
				},
				factory.GetClusterOptions(
					fixtures.WithOneMinuteMinimumUptimeSecondsForBounce,
				)...,
			)

			runTime := time.Since(startTime)
			log.Println("Single-DC cluster creation took: ", runTime.String())
			Expect(fdbCluster.Destroy()).ToNot(HaveOccurred())
		})
	})

	When("creating a multi-DC FDB cluster", func() {
		It("benchmark multi-DC cluster creation", func() {
			startTime := time.Now()

			haCluster := factory.CreateFdbHaCluster(
				&fixtures.ClusterConfig{
					HaMode:          fixtures.HaFourZoneSingleSat,
					CreationTracker: fixtures.NewDefaultCreationTrackerLogger(),
				},
				factory.GetClusterOptions()...,
			)

			runTime := time.Since(startTime)
			log.Println("Multi-DC cluster creation took: ", runTime.String())
			haCluster.Delete()
		})
	})
})
