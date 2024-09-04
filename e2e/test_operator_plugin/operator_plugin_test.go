/*
 * operator_plugin_test.go
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

package operator

/*
This test suite includes functional tests for the kubectl-fdb plugin.
*/

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/fixtures"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
	fdbCluster = factory.CreateFdbCluster(
		fixtures.DefaultClusterConfig(false),
		factory.GetClusterOptions()...,
	)
})

var _ = AfterSuite(func() {
	if CurrentSpecReport().Failed() {
		log.Printf("failed due to %s", CurrentSpecReport().FailureMessage())
	}
	factory.Shutdown()
})

var _ = Describe("Operator Plugin", Label("e2e", "pr"), func() {
	AfterEach(func() {
		if CurrentSpecReport().Failed() {
			factory.DumpState(fdbCluster)
		}
		Expect(fdbCluster.WaitForReconciliation()).ToNot(HaveOccurred())
		factory.StopInvariantCheck()
		// Make sure all data is present in the cluster
		fdbCluster.EnsureTeamTrackersAreHealthy()
		fdbCluster.EnsureTeamTrackersHaveMinReplicas()
	})

	When("getting the plugin version from the operator pod", func() {
		It("should print the version", func() {
			// Pick one operator pod and execute the kubectl version command to ensure that kubectl-fdb is present
			// and can be executed.
			operatorPod := factory.RandomPickOnePod(factory.GetOperatorPods(fdbCluster.Namespace()).Items)
			log.Println("operatorPod", operatorPod.Name)
			Eventually(func(g Gomega) string {
				stdout, stderr, err := factory.ExecuteCmdOnPod(context.Background(), &operatorPod, "manager", fmt.Sprintf("kubectl-fdb -n %s version --version-check=false", fdbCluster.Namespace()), false)
				g.Expect(err).NotTo(HaveOccurred(), stderr)
				return stdout
			}).WithTimeout(10 * time.Minute).WithPolling(2 * time.Second).Should(And(ContainSubstring("kubectl-fdb:"), ContainSubstring("foundationdb-operator:")))
		})
	})
})
