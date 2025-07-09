/*
 * foundationdbbrestore_types_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020-2022 Apple Inc. and the FoundationDB project authors
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

package v1beta2

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("[api] FoundationDBRestore", func() {
	When("getting the backup URL", func() {
		DescribeTable(
			"should generate the correct backup URL",
			func(restore FoundationDBRestore, expected string) {
				Expect(restore.BackupURL()).To(Equal(expected))
			},
			Entry("A restore with a blobstore config",
				FoundationDBRestore{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mybackup",
					},
					Spec: FoundationDBRestoreSpec{
						BlobStoreConfiguration: &BlobStoreConfiguration{
							AccountName: "account@account",
						},
					},
				},
				"blobstore://account@account:443/mybackup?bucket=fdb-backups"),
			Entry("A restore with a blobstore config with backup name",
				FoundationDBRestore{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mybackup",
					},
					Spec: FoundationDBRestoreSpec{
						BlobStoreConfiguration: &BlobStoreConfiguration{
							AccountName: "account@account",
							BackupName:  "test",
						},
					},
				},
				"blobstore://account@account:443/test?bucket=fdb-backups"),
			Entry("A restore with a blobstore config with a bucket name",
				FoundationDBRestore{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mybackup",
					},
					Spec: FoundationDBRestoreSpec{
						BlobStoreConfiguration: &BlobStoreConfiguration{
							AccountName: "account@account",
							Bucket:      "my-bucket",
						},
					},
				},
				"blobstore://account@account:443/mybackup?bucket=my-bucket"),
			Entry("A restore with a blobstore config with a bucket and backup name",
				FoundationDBRestore{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mybackup",
					},
					Spec: FoundationDBRestoreSpec{
						BlobStoreConfiguration: &BlobStoreConfiguration{
							AccountName: "account@account",
							BackupName:  "test",
							Bucket:      "my-bucket",
						},
					},
				},
				"blobstore://account@account:443/test?bucket=my-bucket"),
			Entry(
				"A restore with a blobstore config with HTTP parameters and backup and bucket name",
				FoundationDBRestore{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mybackup",
					},
					Spec: FoundationDBRestoreSpec{
						BlobStoreConfiguration: &BlobStoreConfiguration{
							AccountName: "account@account",
							BackupName:  "test",
							Bucket:      "my-bucket",
							URLParameters: []URLParameter{
								"secure_connection=0",
							},
						},
					},
				},
				"blobstore://account@account:80/test?bucket=my-bucket&secure_connection=0",
			),
			Entry("A restore with a blobstore config with HTTP parameters",
				FoundationDBRestore{
					ObjectMeta: metav1.ObjectMeta{
						Name: "mybackup",
					},
					Spec: FoundationDBRestoreSpec{
						BlobStoreConfiguration: &BlobStoreConfiguration{
							AccountName: "account@account",
							URLParameters: []URLParameter{
								"secure_connection=0",
							},
						},
					},
				},
				"blobstore://account@account:80/mybackup?bucket=fdb-backups&secure_connection=0"),
		)
	})
})
