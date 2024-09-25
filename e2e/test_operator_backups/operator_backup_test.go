/*
 * operator_backup_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2024 Apple Inc. and the FoundationDB project authors
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

package operatorbackup

/*
This test suite contains tests related to backup and restore with the operator.
*/

import (
	"log"

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

	// Create a blobstore for testing backups and restore
	factory.CreateBlobstoreIfAbsent(fdbCluster.Namespace())
})

var _ = AfterSuite(func() {
	if CurrentSpecReport().Failed() {
		log.Printf("failed due to %s", CurrentSpecReport().FailureMessage())
	}
	factory.Shutdown()
})

var _ = Describe("Operator Backup", Label("e2e", "pr"), func() {
	When("a cluster has backups enabled and then restored", func() {
		var keyValues []fixtures.KeyValue
		var prefix byte = 'a'
		var backup *fixtures.FdbBackup

		BeforeEach(func() {
			log.Println("creating backup for cluster")
			backup = factory.CreateBackupForCluster(fdbCluster)
			keyValues = fdbCluster.GenerateRandomValues(10, prefix)
			fdbCluster.WriteKeyValues(keyValues)
			clusterVersion := fdbCluster.GetClusterVersion()
			backup.WaitForRestorableVersion(clusterVersion)
			backup.Stop()
		})

		It("should restore the cluster successfully", func() {
			fdbCluster.ClearRange([]byte{prefix}, 60)
			factory.CreateRestoreForCluster(backup)
			restoreValues := fdbCluster.GetRange([]byte{prefix}, 25, 60)
			Expect(restoreValues).Should(Equal(keyValues))
		})
	})
})
