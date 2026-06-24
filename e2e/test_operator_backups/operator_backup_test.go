/*
 * operator_backup_test.go
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

package operatorbackup

/*
This test suite contains tests related to backup and restore with the operator.
*/

import (
	"log"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/e2e/fixtures"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	factory     *fixtures.Factory
	fdbCluster  *fixtures.FdbCluster
	testOptions *fixtures.FactoryOptions
)

func init() {
	testOptions = fixtures.InitFlags()
}

var _ = BeforeSuite(func(ctx SpecContext) {
	factory = fixtures.CreateFactory(testOptions)

	badBackupVersion, err := fdbv1beta2.ParseFdbVersion("7.3.50")
	Expect(err).NotTo(HaveOccurred())
	goodBackupVersion, err := fdbv1beta2.ParseFdbVersion("7.3.62")
	Expect(err).NotTo(HaveOccurred())

	version := factory.GetFDBVersion()
	if version.IsAtLeast(badBackupVersion) && !version.IsAtLeast(goodBackupVersion) {
		Skip("version has a bug in the backup version that prevents tests to succeed")
	}

	if factory.GetFDBVersion().String() == "7.1.63" {
		Skip("Skip backup tests with 7.1.63 as this version has a bug in the fdbbackup agent")
	}

	// Create a blobstore for testing backups and restore.
	factory.CreateBlobstoreIfAbsent(ctx, factory.SingleNamespace(ctx))
})

var _ = AfterSuite(func(ctx SpecContext) {
	if CurrentSpecReport().Failed() {
		log.Printf("failed due to %s", CurrentSpecReport().FailureMessage())
	}
	factory.Shutdown(ctx)
})

var _ = Describe("Operator Backup", Label("e2e", "pr", "foundationdb-pr"), func() {
	When("a cluster has backups enabled and then restored", func() {
		var keyValues []fixtures.KeyValue
		var prefix byte = 'a'
		var backup *fixtures.FdbBackup
		var restore *fixtures.FdbRestore

		BeforeEach(func(ctx SpecContext) {
			fdbCluster = factory.CreateFdbCluster(ctx,
				fixtures.DefaultClusterConfig(false),
			)
		})

		// Delete the backup and restore resource after each test. And make sure that the data in the cluster is cleared.
		AfterEach(func(ctx SpecContext) {
			if backup != nil {
				backup.Destroy(ctx)
			}
			if restore != nil {
				restore.Destroy(ctx)
			}

			namespace := fdbCluster.Namespace()
			// Delete the FDB cluster to have a clean start.
			Expect(fdbCluster.DestroyWithWaitForTearDown(ctx, true)).To(Succeed())
			// Restart the operator pods.
			factory.RecreateOperatorPods(ctx, namespace)
		})

		When("the default backup system is used", func() {
			// shouldPauseBackup controls whether the backup is paused or stopped after reaching a restorable version.
			// Use pause when the test needs to resume the same backup (e.g. to run modify), since pause suspends
			// the backup agents while keeping the backup active. Use stop (the default) when the backup is complete
			// and a new backup would be acceptable, since stop terminates the backup via fdbbackup discontinue.
			var useRestorableVersion, skipRestore, shouldPauseBackup bool
			var backupConfiguration *fixtures.FdbBackupConfiguration
			var currentRestorableVersion *uint64

			JustBeforeEach(func(ctx SpecContext) {
				log.Printf(
					"creating backup for cluster, skipRestore: %t, useRestorableVersion: %t\n",
					skipRestore,
					useRestorableVersion,
				)
				var restorableVersion uint64
				keyValues = fdbCluster.GenerateRandomValues(10, prefix)

				switch ptr.Deref(
					backupConfiguration.BackupMode,
					fdbv1beta2.BackupModeContinuous,
				) {
				case fdbv1beta2.BackupModeContinuous:
					// For the continuous backup we want to start the backup first and then write some data.
					backup = factory.CreateBackupForCluster(ctx, fdbCluster, backupConfiguration)
					fdbCluster.WriteKeyValues(ctx, keyValues)
					if backup.GetBackup(ctx).GetBackupType() != fdbv1beta2.BackupTypeUnmanaged {
						restorableVersion = backup.WaitForRestorableVersion(
							ctx,
							fdbCluster.GetClusterVersion(ctx),
						)
						if shouldPauseBackup {
							backup.Pause(ctx)
						} else {
							backup.Stop(ctx)
						}
					}
				case fdbv1beta2.BackupModeOneTime:
					// In case of the one time backup we have to first write the keys and then do the backup.
					fdbCluster.WriteKeyValues(ctx, keyValues)
					currentVersion := fdbCluster.GetClusterVersion(ctx)
					backup = factory.CreateBackupForCluster(ctx, fdbCluster, backupConfiguration)
					if backup.GetBackup(ctx).GetBackupType() != fdbv1beta2.BackupTypeUnmanaged {
						restorableVersion = backup.WaitForRestorableVersion(
							ctx,
							currentVersion,
						)
					}
				}

				// Delete the data and restore it again.
				fdbCluster.ClearRange(ctx, []byte{prefix}, 60)
				if useRestorableVersion {
					currentRestorableVersion = ptr.To(restorableVersion)
				}
				if !skipRestore {
					restore = factory.CreateRestoreForCluster(ctx, backup, currentRestorableVersion)
				}
			})

			When("the backup should not be managed", func() {
				BeforeEach(func(_ SpecContext) {
					// The backup is not managed by the operator, so we can skip it.
					skipRestore = true
					backupConfiguration = &fixtures.FdbBackupConfiguration{
						BackupType: ptr.To(fdbv1beta2.BackupTypeUnmanaged),
					}
				})

				When("the backup was not started externally", func() {
					It(
						"should only have data for the backup deployment", func(ctx SpecContext) {
							currentBackup := backup.GetBackup(ctx)
							Expect(currentBackup.Status.DeploymentConfigured).Should(BeTrue())
							Expect(currentBackup.Status.BackupDetails.Running).Should(BeFalse())
							Expect(currentBackup.Status.BackupDetails.Restorable).Should(BeFalse())
						},
					)
				})
			})

			When("the continuous backup mode is used", func() {
				BeforeEach(func(_ SpecContext) {
					skipRestore = false
					backupConfiguration = &fixtures.FdbBackupConfiguration{
						BackupType: ptr.To(fdbv1beta2.BackupTypeDefault),
						BackupMode: ptr.To(fdbv1beta2.BackupModeContinuous),
					}
				})

				When("no restorable version is specified", func() {
					JustBeforeEach(func(ctx SpecContext) {
						// running describe command
						describeCommandOutput := backup.RunDescribeCommand(ctx)
						if factory.GetFDBVersion().SupportsBackupEncryption() {
							Expect(*describeCommandOutput.FileLevelEncryption).To(BeFalse())
						}
						Expect(*describeCommandOutput.Restorable).To(BeTrue())
						Expect(*describeCommandOutput.Partitioned).To(BeFalse())
					})

					It(
						"should restore the cluster successfully with a restorable version",
						func(ctx SpecContext) {
							Expect(
								fdbCluster.GetRange(ctx, []byte{prefix}, 25, 60),
							).Should(Equal(keyValues))
						},
					)
				})

				When("encryption is enabled", func() {
					BeforeEach(func(_ SpecContext) {
						if !factory.GetFDBVersion().SupportsBackupEncryption() {
							Skip(
								"version doesn't support the encryption feature",
							)
						}

						backupConfiguration.EncryptionEnabled = true
					})

					AfterEach(func(ctx SpecContext) {
						if backup == nil {
							return
						}

						backup.Start(ctx)

						var statusBeforeAbort *fdbv1beta2.FoundationDBLiveBackupStatus
						Eventually(func(g Gomega) {
							statusBeforeAbort = backup.RunStatusCommand(ctx)
							g.Expect(statusBeforeAbort.Status.Running).To(BeTrue())
							g.Expect(statusBeforeAbort.Status.Name).
								To(Equal("RunningDifferentially"))
							g.Expect(statusBeforeAbort.UID).NotTo(BeNil())
							g.Expect(ptr.Deref(statusBeforeAbort.Restorable, false)).To(BeTrue())
							g.Expect(statusBeforeAbort.LatestRestorablePoint).NotTo(BeNil())
						}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
						uidBeforeAbort := *statusBeforeAbort.UID

						backup.RunAbortCommand(ctx)

						Eventually(func(g Gomega) {
							statusAfterAbort := backup.RunStatusCommand(ctx)
							g.Expect(statusAfterAbort.Status.Running).To(BeFalse())
							g.Expect(statusAfterAbort.Status.Name).To(Equal("Aborted"))
							g.Expect(statusAfterAbort.UID).NotTo(BeNil())
							g.Expect(*statusAfterAbort.UID).To(Equal(uidBeforeAbort))
						}).WithTimeout(2 * time.Minute).WithPolling(2 * time.Second).Should(Succeed())
					})

					When("running fdbbackup commands", func() {
						BeforeEach(func(_ SpecContext) {
							skipRestore = true
							shouldPauseBackup = true
						})

						JustBeforeEach(func(ctx SpecContext) {
							// running status command
							statusCommandOutput := backup.RunStatusCommand(ctx)
							Expect(statusCommandOutput.SnapshotIntervalSeconds).To(Equal(864000))
							Expect(statusCommandOutput.UID).NotTo(BeNil())
							backupUID := *statusCommandOutput.UID

							// running list command
							listCommandOutput := backup.RunListCommand(ctx)
							Expect(listCommandOutput).To(HaveLen(1))

							// running modify command to change the snapshot period
							// restart the backup before modifying since the backup is paused
							modifiedSnapshotPeriod := 900000
							backup.Start(ctx)
							backup.SetSnapshotInterval(ctx, modifiedSnapshotPeriod)
							// validating snapshot interval changed and the backup UID is same
							statusCommandOutput = backup.RunStatusCommand(ctx)
							Expect(
								statusCommandOutput.SnapshotIntervalSeconds,
							).To(Equal(modifiedSnapshotPeriod))
							Expect(statusCommandOutput.UID).NotTo(BeNil())
							Expect(*statusCommandOutput.UID).To(Equal(backupUID))
							backup.Stop(ctx)

							// running describe command
							describeCommandOutput := backup.RunDescribeCommand(ctx)
							Expect(*describeCommandOutput.FileLevelEncryption).To(BeTrue())
							Expect(*describeCommandOutput.Restorable).To(BeTrue())
							Expect(*describeCommandOutput.Partitioned).To(BeFalse())
						})

						It(
							"should be able to restore the cluster successfully with a restorable version",
							func(ctx SpecContext) {
								restore = factory.CreateRestoreForCluster(
									ctx,
									backup,
									currentRestorableVersion,
								)
								Expect(
									fdbCluster.GetRange(ctx, []byte{prefix}, 25, 60),
								).Should(Equal(keyValues))
							},
						)
					})
				})

				// TODO (johscheuer): Enable test once the CRD in CI is updated.
				PWhen("using a restorable version", func() {
					BeforeEach(func(_ SpecContext) {
						useRestorableVersion = true
					})

					It(
						"should restore the cluster successfully with a restorable version",
						func(ctx SpecContext) {
							Expect(
								fdbCluster.GetRange(ctx, []byte{prefix}, 25, 60),
							).Should(Equal(keyValues))
						},
					)
				})

				When("the replica count is changed", func() {
					JustBeforeEach(func(ctx SpecContext) {
						currentBackup := backup.GetBackup(ctx)
						backupSpec := currentBackup.Spec.DeepCopy()
						backupSpec.AgentCount = ptr.To(currentBackup.GetDesiredAgentCount() * 2)
						backup.UpdateBackupSpecWithSpec(backupSpec)
					})

					It("should update the running backup agent pods", func(ctx SpecContext) {
						desiredAgentCount := backup.GetBackup(ctx).GetDesiredAgentCount()
						Eventually(func() int {
							return len(backup.GetBackupPods(ctx).Items)
						}).WithTimeout(5 * time.Minute).WithPolling(5 * time.Second).Should(BeNumerically("==", desiredAgentCount))
					})
				})

				When("a pod is stuck in a terminal state", func() {
					terminalPodName := "terminal-pod"

					JustBeforeEach(func(ctx SpecContext) {
						currentBackup := backup.GetBackup(ctx)

						deployment := &appsv1.Deployment{}
						Expect(factory.GetControllerRuntimeClient().Get(ctx, ctrlClient.ObjectKey{
							Name:      internal.GetBackupDeploymentName(currentBackup),
							Namespace: currentBackup.Namespace,
						}, deployment)).To(Succeed())

						// Create a pod that is assigned to a node that doesn't exist.
						spec := deployment.Spec.Template.Spec
						spec.NodeName = "doesnotexist"
						terminalPod := &corev1.Pod{
							ObjectMeta: metav1.ObjectMeta{
								Name:      terminalPodName,
								Namespace: currentBackup.Namespace,
								Labels:    deployment.Spec.Template.Labels,
							},
							Spec: spec,
						}

						Expect(factory.GetControllerRuntimeClient().DeleteAllOf(ctx, &corev1.Pod{},
							ctrlClient.InNamespace(currentBackup.Namespace),
							ctrlClient.MatchingLabels(map[string]string{fdbv1beta2.BackupDeploymentPodLabel: currentBackup.Name + "-backup-agents"}),
						),
						).To(Succeed())

						Expect(
							factory.GetControllerRuntimeClient().Create(ctx, terminalPod),
						).To(Succeed())

						for _, backupAgentPod := range backup.GetBackupPods(ctx).Items {
							if backupAgentPod.Name == terminalPodName {
								continue
							}

							if !backupAgentPod.DeletionTimestamp.IsZero() {
								continue
							}

							Expect(
								factory.GetControllerRuntimeClient().
									Delete(ctx, ptr.To(backupAgentPod)),
							).To(Succeed())
						}
					})

					It("should delete the pod in terminal state", func(ctx SpecContext) {
						backup.ForceReconcile(ctx)
						Eventually(func(g Gomega) {
							for _, backupAgentPod := range backup.GetBackupPods(ctx).Items {
								log.Println(
									backupAgentPod.Name,
									"state:",
									backupAgentPod.Status.Phase,
								)
								g.Expect(backupAgentPod.Name).NotTo(Equal(terminalPodName))
							}
						}).WithTimeout(5 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
					})
				})
			})

			When("the one time backup mode is used", func() {
				BeforeEach(func(_ SpecContext) {
					skipRestore = false
					backupConfiguration = &fixtures.FdbBackupConfiguration{
						BackupType: ptr.To(fdbv1beta2.BackupTypeDefault),
						BackupMode: ptr.To(fdbv1beta2.BackupModeOneTime),
					}
				})

				When("no restorable version is specified", func() {
					It(
						"should restore the cluster successfully with a restorable version",
						func(ctx SpecContext) {
							Expect(
								fdbCluster.GetRange(ctx, []byte{prefix}, 25, 60),
							).Should(Equal(keyValues))
						},
					)
				})

				When("encryption is enabled", func() {
					BeforeEach(func(_ SpecContext) {
						if !factory.GetFDBVersion().SupportsBackupEncryption() {
							Skip(
								"version doesn't support the encryption feature",
							)
						}

						backupConfiguration.EncryptionEnabled = true
					})

					It(
						"should restore the cluster successfully with a restorable version",
						func(ctx SpecContext) {
							Expect(
								fdbCluster.GetRange(ctx, []byte{prefix}, 25, 60),
							).Should(Equal(keyValues))
						},
					)
				})

				// TODO (johscheuer): Enable test once the CRD in CI is updated.
				PWhen("using a restorable version", func() {
					BeforeEach(func(_ SpecContext) {
						useRestorableVersion = true
					})

					It(
						"should restore the cluster successfully with a restorable version",
						func(ctx SpecContext) {
							Expect(
								fdbCluster.GetRange(ctx, []byte{prefix}, 25, 60),
							).Should(Equal(keyValues))
						},
					)
				})
			})
		})

		When("the partitioned backup system is used", func() {
			BeforeEach(func(ctx SpecContext) {
				// Versions before 7.4 have a few issues and will not work properly with the experimental feature.
				requiredFdbVersion, err := fdbv1beta2.ParseFdbVersion("7.4.5")
				Expect(err).NotTo(HaveOccurred())

				version := factory.GetFDBVersion()
				if !version.IsAtLeast(requiredFdbVersion) {
					Skip("version has a bug in the backup version that prevents tests to succeed")
				}
				log.Println("creating backup for cluster with partitioned log system")
				// Add additional backup workers to the cluster. Those will be used by the partitioned backup system.
				// The backup worker(not to be confused with the backup agent) will be used to back up the lof mutations.
				// We still need the backup agents to back up the key ranges.
				cluster := fdbCluster.GetCluster(ctx)
				spec := cluster.Spec.DeepCopy()
				processCounts, err := cluster.GetProcessCountsWithDefaults()
				Expect(err).NotTo(HaveOccurred())
				// We should create the same count of backup worker as we create log processes.
				spec.ProcessCounts.BackupWorker = processCounts.Log

				// We take the spec from the general process class as the starting point.
				generalProcessSpec := spec.Processes[fdbv1beta2.ProcessClassGeneral]
				processSpec := generalProcessSpec.DeepCopy()

				processSpec.PodTemplate.Spec.Volumes = append(
					processSpec.PodTemplate.Spec.Volumes,
					corev1.Volume{
						Name: "backup-credentials",
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: factory.GetBackupSecretName(),
							},
						},
					},
				)

				for idx, container := range processSpec.PodTemplate.Spec.Containers {
					if container.Name != fdbv1beta2.MainContainerName {
						continue
					}

					// Make sure we add the FDB_BLOB_CREDENTIALS to ensure the backup worker has access to the
					// blob store.
					container.Env = append(container.Env, corev1.EnvVar{
						Name:  "FDB_BLOB_CREDENTIALS",
						Value: "/tmp/backup-credentials/credentials",
					})

					container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
						Name:      "backup-credentials",
						ReadOnly:  true,
						MountPath: "/tmp/backup-credentials",
					})

					processSpec.PodTemplate.Spec.Containers[idx] = container
					break
				}
				spec.Processes[fdbv1beta2.ProcessClassBackup] = *processSpec
				fdbCluster.UpdateClusterSpecWithSpec(ctx, spec)
				Expect(fdbCluster.WaitForReconciliation(ctx)).To(Succeed())

				backup = factory.CreateBackupForCluster(ctx,
					fdbCluster,
					&fixtures.FdbBackupConfiguration{
						BackupType: ptr.To(fdbv1beta2.BackupTypePartitionedLog),
					},
				)
				keyValues = fdbCluster.GenerateRandomValues(10, prefix)
				fdbCluster.WriteKeyValues(ctx, keyValues)
				backup.WaitForRestorableVersion(ctx, fdbCluster.GetClusterVersion(ctx))
				backup.Stop(ctx)
			})

			It("should restore the cluster successfully", func(ctx SpecContext) {
				fdbCluster.ClearRange(ctx, []byte{prefix}, 60)
				factory.CreateRestoreForCluster(ctx, backup, nil)
				Expect(fdbCluster.GetRange(ctx, []byte{prefix}, 25, 60)).Should(Equal(keyValues))
			})

			AfterEach(func(ctx SpecContext) {
				// We have to make sure that the backup is deleted before proceeding.
				backup.Destroy(ctx)
				// Remove additional backup workers from the cluster.
				cluster := fdbCluster.GetCluster(ctx)
				spec := cluster.Spec.DeepCopy()
				spec.ProcessCounts.BackupWorker = -1
				delete(spec.Processes, fdbv1beta2.ProcessClassBackup)

				fdbCluster.UpdateClusterSpecWithSpec(ctx, spec)
				Expect(fdbCluster.WaitForReconciliation(ctx)).To(Succeed())
			})
		})
	})
})
