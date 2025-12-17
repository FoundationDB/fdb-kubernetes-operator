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
	"encoding/json"
	"log"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/e2e/fixtures"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
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
	factory.CreateBlobstoreIfAbsent(factory.SingleNamespace())
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
		var restore *fixtures.FdbRestore

		BeforeEach(func() {
			fdbCluster = factory.CreateFdbCluster(
				fixtures.DefaultClusterConfig(false),
			)
		})

		// Delete the backup and restore resource after each test. And make sure that the data in the cluster is cleared.
		AfterEach(func() {
			if backup != nil {
				backup.Destroy()
			}
			if restore != nil {
				restore.Destroy()
			}

			namespace := fdbCluster.Namespace()
			// Delete the FDB cluster to have a clean start.
			Expect(fdbCluster.DestroyWithWaitForTearDown(true)).To(Succeed())
			// Restart the operator pods.
			factory.RecreateOperatorPods(namespace)
		})

		When("the default backup system is used", func() {
			var useRestorableVersion bool
			var backupConfiguration *fixtures.FdbBackupConfiguration
			var currentRestorableVersion *uint64
			var skipRestore bool

			JustBeforeEach(func() {
				log.Println("creating backup for cluster")
				var restorableVersion uint64

				if ptr.Deref(
					backupConfiguration.BackupMode,
					fdbv1beta2.BackupModeContinuous,
				) == fdbv1beta2.BackupModeContinuous {
					// For the continuous backup we want to start the backup first and then write some data.
					backup = factory.CreateBackupForCluster(fdbCluster, backupConfiguration)
					keyValues = fdbCluster.GenerateRandomValues(10, prefix)
					fdbCluster.WriteKeyValues(keyValues)
					restorableVersion = backup.WaitForRestorableVersion(
						fdbCluster.GetClusterVersion(),
					)
					backup.Stop()
				} else {
					// In case of the one time backup we have to first write the keys and then do the backup.
					keyValues = fdbCluster.GenerateRandomValues(10, prefix)
					fdbCluster.WriteKeyValues(keyValues)
					currentVersion := fdbCluster.GetClusterVersion()
					backup = factory.CreateBackupForCluster(fdbCluster, backupConfiguration)
					restorableVersion = backup.WaitForRestorableVersion(
						currentVersion,
					)
				}

				// Delete the data and restore it again.
				fdbCluster.ClearRange([]byte{prefix}, 60)
				if useRestorableVersion {
					currentRestorableVersion = ptr.To(restorableVersion)
				}
				if !skipRestore {
					restore = factory.CreateRestoreForCluster(backup, currentRestorableVersion)
				}
			})

			When("the continuous backup mode is used", func() {
				BeforeEach(func() {
					backupConfiguration = &fixtures.FdbBackupConfiguration{
						BackupType: ptr.To(fdbv1beta2.BackupTypeDefault),
						BackupMode: ptr.To(fdbv1beta2.BackupModeContinuous),
					}
				})

				When("no restorable version is specified", func() {
					It("should restore the cluster successfully with a restorable version", func() {
						Expect(fdbCluster.GetRange([]byte{prefix}, 25, 60)).Should(Equal(keyValues))
					})
				})

				When("encryption is enabled", func() {
					BeforeEach(func() {
						if !factory.GetFDBVersion().SupportsBackupEncryption() {
							Skip(
								"version doesn't support the encryption feature",
							)
						}

						backupConfiguration.EncryptionEnabled = true
					})

					When("running describe command", func() {
						BeforeEach(func() {
							skipRestore = true
						})

						JustBeforeEach(func() {
							describeCommandOutput := backup.RunDescribeCommand()

							var describeData map[string]interface{}
							err := json.Unmarshal([]byte(describeCommandOutput), &describeData)
							Expect(err).NotTo(HaveOccurred())

							restorable := describeData["Restorable"].(bool)
							Expect(restorable).To(BeTrue())
							
							// TODO (09harsh): Uncomment this when we have the fileLevelEncryption in json parser
							// here: https://github.com/apple/foundationdb/blob/main/fdbclient/BackupContainer.actor.cpp#L193-L250
							//fileLevelEncryption := describeData["FileLevelEncryption"].(bool)
							//Expect(fileLevelEncryption).To(BeTrue())
						})

						It(
							"should be able to restore the cluster successfully with a restorable version",
							func() {
								restore = factory.CreateRestoreForCluster(
									backup,
									currentRestorableVersion,
								)
								Expect(
									fdbCluster.GetRange([]byte{prefix}, 25, 60),
								).Should(Equal(keyValues))
							},
						)
					})
				})

				// TODO (johscheuer): Enable test once the CRD in CI is updated.
				PWhen("using a restorable version", func() {
					BeforeEach(func() {
						useRestorableVersion = true
					})

					It("should restore the cluster successfully with a restorable version", func() {
						Expect(fdbCluster.GetRange([]byte{prefix}, 25, 60)).Should(Equal(keyValues))
					})
				})
			})

			When("the one time backup mode is used", func() {
				BeforeEach(func() {
					backupConfiguration = &fixtures.FdbBackupConfiguration{
						BackupType: ptr.To(fdbv1beta2.BackupTypeDefault),
						BackupMode: ptr.To(fdbv1beta2.BackupModeOneTime),
					}
				})

				When("no restorable version is specified", func() {
					It("should restore the cluster successfully with a restorable version", func() {
						Expect(fdbCluster.GetRange([]byte{prefix}, 25, 60)).Should(Equal(keyValues))
					})
				})

				When("encryption is enabled", func() {
					BeforeEach(func() {
						if !factory.GetFDBVersion().SupportsBackupEncryption() {
							Skip(
								"version doesn't support the encryption feature",
							)
						}

						backupConfiguration.EncryptionEnabled = true
					})

					It("should restore the cluster successfully with a restorable version", func() {
						Expect(fdbCluster.GetRange([]byte{prefix}, 25, 60)).Should(Equal(keyValues))
					})
				})

				// TODO (johscheuer): Enable test once the CRD in CI is updated.
				PWhen("using a restorable version", func() {
					BeforeEach(func() {
						useRestorableVersion = true
					})

					It("should restore the cluster successfully with a restorable version", func() {
						Expect(fdbCluster.GetRange([]byte{prefix}, 25, 60)).Should(Equal(keyValues))
					})
				})
			})
		})

		When("the partitioned backup system is used", func() {
			BeforeEach(func() {
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
				cluster := fdbCluster.GetCluster()
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
				fdbCluster.UpdateClusterSpecWithSpec(spec)
				Expect(fdbCluster.WaitForReconciliation()).To(Succeed())

				backup = factory.CreateBackupForCluster(
					fdbCluster,
					&fixtures.FdbBackupConfiguration{
						BackupType: ptr.To(fdbv1beta2.BackupTypePartitionedLog),
					},
				)
				keyValues = fdbCluster.GenerateRandomValues(10, prefix)
				fdbCluster.WriteKeyValues(keyValues)
				backup.WaitForRestorableVersion(fdbCluster.GetClusterVersion())
				backup.Stop()
			})

			It("should restore the cluster successfully", func() {
				fdbCluster.ClearRange([]byte{prefix}, 60)
				factory.CreateRestoreForCluster(backup, nil)
				Expect(fdbCluster.GetRange([]byte{prefix}, 25, 60)).Should(Equal(keyValues))
			})

			AfterEach(func() {
				// We have to make sure that the backup is deleted before proceeding.
				backup.Destroy()
				// Remove additional backup workers from the cluster.
				cluster := fdbCluster.GetCluster()
				spec := cluster.Spec.DeepCopy()
				spec.ProcessCounts.BackupWorker = -1
				delete(spec.Processes, fdbv1beta2.ProcessClassBackup)

				fdbCluster.UpdateClusterSpecWithSpec(spec)
				Expect(fdbCluster.WaitForReconciliation()).To(Succeed())
			})
		})
	})
})
