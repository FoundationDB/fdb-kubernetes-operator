/*
 * fdb_restore.go
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

package fixtures

import (
	"context"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CreateRestoreForCluster will create a FoundationDBRestore resource based on the provided backup resource.
// For more information how the backup system with the operator is working please look at
// the operator documentation: https://github.com/FoundationDB/fdb-kubernetes-operator/blob/master/docs/manual/backup.md
func (factory *Factory) CreateRestoreForCluster(backup *FdbBackup) {
	gomega.Expect(backup).NotTo(gomega.BeNil())
	restore := &fdbv1beta2.FoundationDBRestore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      backup.fdbCluster.Name(),
			Namespace: backup.fdbCluster.Namespace(),
		},
		Spec: fdbv1beta2.FoundationDBRestoreSpec{
			DestinationClusterName: backup.fdbCluster.Name(),
			BlobStoreConfiguration: backup.backup.Spec.BlobStoreConfiguration,
			CustomParameters:       backup.backup.Spec.CustomParameters,
		},
	}
	gomega.Expect(factory.CreateIfAbsent(restore)).NotTo(gomega.HaveOccurred())

	factory.AddShutdownHook(func() error {
		return factory.GetControllerRuntimeClient().Delete(context.Background(), restore)
	})

	waitForRestoreToComplete(backup)
}

// waitForRestoreToComplete waits until the restore completed.
func waitForRestoreToComplete(backup *FdbBackup) {
	gomega.Eventually(func(g gomega.Gomega) string {
		backupPod := backup.GetBackupPod()

		out, _, err := backup.fdbCluster.ExecuteCmdOnPod(
			*backupPod,
			fdbv1beta2.MainContainerName,
			"fdbrestore status --dest_cluster_file $FDB_CLUSTER_FILE",
			false,
		)
		g.Expect(err).NotTo(gomega.HaveOccurred())

		return out
	}).WithTimeout(20 * time.Minute).WithPolling(2 * time.Second).Should(gomega.ContainSubstring("State: completed"))
}
