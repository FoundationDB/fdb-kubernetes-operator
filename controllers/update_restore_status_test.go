/*
 * update_restore_status_test.go
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

package controllers

import (
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("update_restore_status_test", func() {
	DescribeTable("when parsing the status from the restore status output", func(input string, expected fdbv1beta2.FoundationDBRestoreState) {
		Expect(parseRestoreStatus(input)).To(Equal(expected))
	},
		Entry("empty status", "", fdbv1beta2.UnknownFoundationDBRestoreState),
		Entry("completed status", "Tag: default  UID: 3213  State: completed  Blocks: 5/5  BlocksInProgress: 0  Files: 74  BytesWritten: 2303  CurrentVersion: 123 FirstConsistentVersion: 321  ApplyVersionLag: 0  LastError: None  URL: blobstore://.. Range: ''-'\xff'  Range: '\xff\x02/blobRange/'-'\xff\x02/blobRange0'  Range: '\xff/metacluster/clusterRegistration'-'\xff/metacluster/clusterRegistration\x00'  Range: '\xff/tagQuota/'-'\xff/tagQuota0'  Range: '\xff/tenant/'-'\xff/tenant0'  AddPrefix: ''  RemovePrefix: ''  Version: 123", fdbv1beta2.CompletedFoundationDBRestoreState),
	)
})
