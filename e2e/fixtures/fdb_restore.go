/*
 * fdb_restore.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2026 Apple Inc. and the FoundationDB project authors
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
	"k8s.io/utils/ptr"

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
// the operator documentation: https://github.com/FoundationDB/fdb-kubernetes-operator/blob/main/docs/manual/backup.md
func (factory *Factory) CreateRestoreForCluster(
	ctx context.Context,
	backup *FdbBackup,
	backupVersion *uint64,
) *FdbRestore {
	gomega.Expect(backup).NotTo(gomega.BeNil())
	// If a backup is still running or paused on this cluster the restore will be stuck in queued. The stop will
	// discontinue the backup, which allows the restore to get started.
	if backup.backup.Spec.BackupState != fdbv1beta2.BackupStateStopped {
		log.Println(
			"backup was not stopped, will stop backup before restore. Current desired state:",
			backup.backup.Spec.BackupState,
		)
		backup.Stop(ctx)
	}

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

	gomega.Expect(factory.CreateIfAbsent(ctx, restore.restore)).NotTo(gomega.HaveOccurred())

	factory.AddShutdownHook(func() error {
		restore.Destroy(context.Background())
		return nil
	})

	restore.waitForRestoreToComplete(ctx, backup)

	return restore
}

// waitForRestoreToComplete waits until the restore completed.
func (restore *FdbRestore) waitForRestoreToComplete(ctx context.Context, backup *FdbBackup) {
	ctrlClient := restore.fdbCluster.getClient()

	lastReconcile := time.Now()
	gomega.Eventually(func(g gomega.Gomega) fdbv1beta2.FoundationDBRestoreState {
		currentRestore := &fdbv1beta2.FoundationDBRestore{}
		g.Expect(ctrlClient.Get(ctx, client.ObjectKeyFromObject(restore.restore), currentRestore)).
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
				ctx,
				currentRestore,
				patch)).To(gomega.Succeed())

			out, _, err := restore.fdbCluster.ExecuteCmdOnPod(
				ctx,
				*backup.GetBackupPod(ctx),
				fdbv1beta2.MainContainerName,
				"fdbrestore status --dest_cluster_file $FDB_CLUSTER_FILE",
				false,
			)

			log.Println(out)
			g.Expect(err).To(gomega.Succeed())

			// Dump the operator state and logs.
			restore.fdbCluster.factory.DumpOperatorLogs(ctx, restore.fdbCluster, ptr.To[int64](300))
		}

		return currentRestore.Status.State
	}).WithTimeout(30 * time.Minute).WithPolling(1 * time.Second).Should(gomega.Equal(fdbv1beta2.CompletedFoundationDBRestoreState))
}

// Destroy will delete the FoundationDBRestore for the associated FdbBackup if it exists.
func (restore *FdbRestore) Destroy(ctx context.Context) {
	gomega.Eventually(func(g gomega.Gomega) {
		err := restore.fdbCluster.factory.GetControllerRuntimeClient().
			Delete(ctx, restore.restore)
		if k8serrors.IsNotFound(err) {
			return
		}

		g.Expect(err).NotTo(gomega.HaveOccurred())
	}).WithTimeout(4 * time.Minute).WithPolling(1 * time.Second).Should(gomega.Succeed())

	// Ensure that the resource is removed.
	gomega.Eventually(func(g gomega.Gomega) {
		currentRestore := &fdbv1beta2.FoundationDBRestore{}
		err := restore.fdbCluster.getClient().
			Get(ctx, client.ObjectKeyFromObject(restore.restore), currentRestore)
		g.Expect(k8serrors.IsNotFound(err)).To(gomega.BeTrue())
	}).WithTimeout(10 * time.Minute).WithPolling(1 * time.Second).To(gomega.Succeed())
}
