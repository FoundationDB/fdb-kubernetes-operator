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
	"log"
	"strconv"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	"sigs.k8s.io/controller-runtime/pkg/client"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FdbRestore represents a fdbv1beta2.FoundationDBRestore resource for doing restores to a FdbCluster.
type FdbRestore struct {
	restore    *fdbv1beta2.FoundationDBRestore
	fdbCluster *FdbCluster
}

// CreateRestoreForCluster will create a FoundationDBRestore resource based on the provided backup resource.
// For more information how the backup system with the operator is working please look at
// the operator documentation: https://github.com/FoundationDB/fdb-kubernetes-operator/v2/blob/master/docs/manual/backup.md
func (factory *Factory) CreateRestoreForCluster(
	backup *FdbBackup,
	backupVersion *uint64,
) *FdbRestore {
	gomega.Expect(backup).NotTo(gomega.BeNil())
	restore := &FdbRestore{
		restore: &fdbv1beta2.FoundationDBRestore{
			ObjectMeta: metav1.ObjectMeta{
				Name:      backup.fdbCluster.Name(),
				Namespace: backup.fdbCluster.Namespace(),
			},
			Spec: fdbv1beta2.FoundationDBRestoreSpec{
				DestinationClusterName: backup.fdbCluster.Name(),
				BlobStoreConfiguration: backup.backup.Spec.BlobStoreConfiguration,
				CustomParameters:       backup.backup.Spec.CustomParameters,
				BackupVersion:          backupVersion,
				EncryptionKeyPath:      backup.backup.Spec.EncryptionKeyPath,
			},
		},
		fdbCluster: backup.fdbCluster,
	}

	gomega.Expect(factory.CreateIfAbsent(restore.restore)).NotTo(gomega.HaveOccurred())

	factory.AddShutdownHook(func() error {
		restore.Destroy()
		return nil
	})

	restore.waitForRestoreToComplete(backup)

	return restore
}

// waitForRestoreToComplete waits until the restore completed.
func (restore *FdbRestore) waitForRestoreToComplete(backup *FdbBackup) {
	ctrlClient := restore.fdbCluster.getClient()

	lastReconcile := time.Now()
	gomega.Eventually(func(g gomega.Gomega) fdbv1beta2.FoundationDBRestoreState {
		currentRestore := &fdbv1beta2.FoundationDBRestore{}
		g.Expect(ctrlClient.Get(context.Background(), client.ObjectKeyFromObject(restore.restore), currentRestore)).
			To(gomega.Succeed())
		log.Println("restore state:", currentRestore.Status.State)

		if time.Since(lastReconcile) > time.Minute {
			lastReconcile = time.Now()
			patch := client.MergeFrom(currentRestore.DeepCopy())
			if currentRestore.Annotations == nil {
				currentRestore.Annotations = make(map[string]string)
			}
			currentRestore.Annotations["foundationdb.org/reconcile"] = strconv.FormatInt(
				time.Now().UnixNano(),
				10,
			)

			// This will apply an Annotation to the object which will trigger the reconcile loop.
			// This should speed up the reconcile phase.
			gomega.Expect(ctrlClient.Patch(
				context.Background(),
				currentRestore,
				patch)).To(gomega.Succeed())

			out, _, err := restore.fdbCluster.ExecuteCmdOnPod(
				*backup.GetBackupPod(),
				fdbv1beta2.MainContainerName,
				"fdbrestore status --dest_cluster_file $FDB_CLUSTER_FILE",
				false,
			)

			log.Println(out)
			g.Expect(err).To(gomega.Succeed())
		}

		return currentRestore.Status.State
	}).WithTimeout(20 * time.Minute).WithPolling(1 * time.Second).Should(gomega.Equal(fdbv1beta2.CompletedFoundationDBRestoreState))
}

// Destroy will delete the FoundationDBRestore for the associated FdbBackup if it exists.
func (restore *FdbRestore) Destroy() {
	gomega.Eventually(func(g gomega.Gomega) {
		err := restore.fdbCluster.factory.GetControllerRuntimeClient().
			Delete(context.Background(), restore.restore)
		if k8serrors.IsNotFound(err) {
			return
		}

		g.Expect(err).NotTo(gomega.HaveOccurred())
	}).WithTimeout(4 * time.Minute).WithPolling(1 * time.Second).Should(gomega.Succeed())

	// Ensure that the resource is removed.
	gomega.Eventually(func(g gomega.Gomega) {
		currentRestore := &fdbv1beta2.FoundationDBRestore{}
		err := restore.fdbCluster.getClient().
			Get(context.Background(), client.ObjectKeyFromObject(restore.restore), currentRestore)
		g.Expect(k8serrors.IsNotFound(err)).To(gomega.BeTrue())
	}).WithTimeout(10 * time.Minute).WithPolling(1 * time.Second).To(gomega.Succeed())
}
