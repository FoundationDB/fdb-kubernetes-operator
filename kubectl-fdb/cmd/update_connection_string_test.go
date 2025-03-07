/*
 * update_connection_string_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2025 Apple Inc. and the FoundationDB project authors
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

package cmd

import (
	"bytes"
	"context"
	"fmt"
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("[plugin] update connection string command", func() {
	var outBuffer bytes.Buffer
	var errBuffer bytes.Buffer
	var inBuffer bytes.Buffer
	var cmd *cobra.Command
	var err error
	var connectionString string
	var initialConnectionString string

	BeforeEach(func() {
		// We use these buffers to check the input/output
		outBuffer = bytes.Buffer{}
		errBuffer = bytes.Buffer{}
		inBuffer = bytes.Buffer{}
		cmd = NewRootCmd(genericclioptions.IOStreams{In: &inBuffer, Out: &outBuffer, ErrOut: &errBuffer}, &MockVersionChecker{})
		initialConnectionString = cluster.Status.ConnectionString
	})

	JustBeforeEach(func() {
		err = updateConnectionStringCmd(cmd, k8sClient, cluster, connectionString)
	})

	When("the connection string is already correct", func() {
		BeforeEach(func() {
			connectionString = cluster.Status.ConnectionString
		})

		It("should not return an error and keep the resource", func() {
			Expect(err).NotTo(HaveOccurred())
			Expect(outBuffer.String()).To(ContainSubstring("already has connections string"))
			Expect(initialConnectionString).To(Equal(connectionString))
		})
	})

	When("the connection string is different", func() {
		When("the connection string is valid", func() {
			BeforeEach(func() {
				connectionString = "test:4321id@127.0.0.1:4500"
				Expect(k8sClient.Create(context.Background(), &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("%s-config", cluster.Name),
						Namespace: cluster.Namespace,
					},
					Data: map[string]string{
						fdbv1beta2.ClusterFileKey: cluster.Status.ConnectionString,
					},
				})).To(Succeed())
			})

			It("should not return an error and update resource", func() {
				Expect(err).NotTo(HaveOccurred())
				Expect(outBuffer.String()).To(BeEmpty())

				fetchedCluster := &fdbv1beta2.FoundationDBCluster{}
				Expect(k8sClient.Get(context.Background(), ctrlClient.ObjectKeyFromObject(cluster), fetchedCluster)).To(Succeed())

				Expect(fetchedCluster.Status.ConnectionString).To(Equal(connectionString))
				Expect(fetchedCluster.Status.ConnectionString).NotTo(Equal(initialConnectionString))

				fetchedConfigMap := &corev1.ConfigMap{}
				Expect(k8sClient.Get(context.Background(), ctrlClient.ObjectKey{Namespace: cluster.Namespace, Name: fmt.Sprintf("%s-config", cluster.Name)}, fetchedConfigMap)).To(Succeed())
				Expect(fetchedConfigMap.Data).To(HaveLen(1))
				Expect(fetchedConfigMap.Data).To(HaveKeyWithValue(fdbv1beta2.ClusterFileKey, connectionString))
			})
		})

		When("the connection string is invalid", func() {
			BeforeEach(func() {
				connectionString = "test-invalid@127.0.0.1:4500"
			})

			It("should return an error with the information that the connection string is invalid", func() {
				Expect(err).To(MatchError(ContainSubstring("invalid connection string")))
			})
		})
	})
})
