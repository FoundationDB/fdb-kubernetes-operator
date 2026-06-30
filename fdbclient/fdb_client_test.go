/*
 * fdb_client_test.go
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

// todo only runif requested.
package fdbclient

import (
	"os"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient"
	"github.com/apple/foundationdb/bindings/go/src/fdb"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// This test suite expects that a running fdb cluster exists. The cluster file can be provided by setting the environment
// variable "FDB_CLUSTER_FILE", if not set it will default to /usr/local/etc/foundationdb/fdb.cluster.
// Those tests can be executed with:
// go test ${go_test_flags} ./... -ginkgo.label-filter="manual"
//
// In the future those tests will be added to our CI setup to ensure we have a reproducible way to test the admin client.
var _ = Describe("fdb_client_test", Label("manual"), func() {
	When("a real admin client is used", func() {
		var adminClient fdbadminclient.AdminClient

		BeforeEach(func() {
			clusterFilePath := os.Getenv("FDB_CLUSTER_FILE")
			if clusterFilePath == "" {
				clusterFilePath = "/usr/local/etc/foundationdb/fdb.cluster"
			}

			fdb.MustAPIVersion(710)
			connectionString, err := os.ReadFile(clusterFilePath)
			Expect(err).NotTo(HaveOccurred())
			adminClient, err = NewCliAdminClient(&fdbv1beta2.FoundationDBCluster{
				ObjectMeta: metav1.ObjectMeta{
					UID: "uuid",
				},
				Spec: fdbv1beta2.FoundationDBClusterSpec{
					AutomationOptions: fdbv1beta2.FoundationDBClusterAutomationOptions{
						DatabaseInteractionMode: ptr.To(fdbv1beta2.DatabaseInteractionModeMgmtAPI),
					},
				},
				Status: fdbv1beta2.FoundationDBClusterStatus{
					ConnectionString: string(connectionString),
				},
			}, nil, GinkgoLogr)

			Expect(err).NotTo(HaveOccurred())
		})

		When("excluding a non-existing process", func() {
			It("should return an error message that the address is invalid", func() {
				err := adminClient.ExcludeProcesses([]fdbv1beta2.ProcessAddress{
					{
						StringAddress: "bad",
					},
				})

				Expect(err).To(HaveOccurred())
				Expect(
					err.Error(),
				).To(ContainSubstring("error from special key 0xff0xff/error_message is: ERROR: 'bad' is not a valid network endpoint address"))
			})
		})

	})
})
