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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
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
	ctrlClient := backup.fdbCluster.getClient()

	lastReconcile := time.Now()
	gomega.Eventually(func(g gomega.Gomega) fdbv1beta2.FoundationDBRestoreState {
		restore := &fdbv1beta2.FoundationDBRestore{}
		g.Expect(ctrlClient.Get(context.Background(), client.ObjectKeyFromObject(backup.backup), restore)).To(gomega.Succeed())
		log.Println("restore state:", restore.Status.State)

		if time.Since(lastReconcile) > time.Minute {
			lastReconcile = time.Now()
			patch := client.MergeFrom(restore.DeepCopy())
			if restore.Annotations == nil {
				restore.Annotations = make(map[string]string)
			}
			restore.Annotations["foundationdb.org/reconcile"] = strconv.FormatInt(
				time.Now().UnixNano(),
				10,
			)

			// This will apply an Annotation to the object which will trigger the reconcile loop.
			// This should speed up the reconcile phase.
			gomega.Expect(ctrlClient.Patch(
				context.Background(),
				restore,
				patch)).To(gomega.Succeed())
		}

		return restore.Status.State
	}).WithTimeout(20 * time.Minute).WithPolling(1 * time.Second).Should(gomega.Equal(fdbv1beta2.CompletedFoundationDBRestoreState))
}
