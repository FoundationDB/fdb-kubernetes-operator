/*
 * common_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2022 Apple Inc. and the FoundationDB project authors
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

package fdbclient

import (
	"fmt"
	"os"
	"path"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("common_test", func() {
	When("creating the cluster file", func() {
		var clusterFile string
		var tmpDir string
		uid := "testuid"
		connectionString := "test@test:127.0.0.1:4500"

		JustBeforeEach(func() {
			var err error
			tmpDir = GinkgoT().TempDir()
			clusterFile, err = ensureClusterFileIsPresent(path.Join(tmpDir, uid), connectionString)
			Expect(err).NotTo(HaveOccurred())
		})

		When("the cluster file doesn't exist", func() {
			It("should create the cluster file with the correct content", func() {
				Expect(clusterFile).To(Equal(path.Join(tmpDir, uid)))
				content, err := os.ReadFile(clusterFile)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(content)).To(Equal(connectionString))
			})
		})

		When("the cluster file exist with the wrong content", func() {
			BeforeEach(func() {
				err := os.WriteFile(path.Join(GinkgoT().TempDir(), uid), []byte("wrong"), 0777)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should update the cluster file with the correct content", func() {
				Expect(clusterFile).To(Equal(path.Join(tmpDir, uid)))
				content, err := os.ReadFile(clusterFile)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(content)).To(Equal(connectionString))
			})
		})

		When("the cluster file exist with the correct content", func() {
			BeforeEach(func() {
				err := os.WriteFile(path.Join(GinkgoT().TempDir(), uid), []byte(connectionString), 0777)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should keep the cluster file with the correct content", func() {
				Expect(clusterFile).To(Equal(path.Join(tmpDir, uid)))
				content, err := os.ReadFile(clusterFile)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(content)).To(Equal(connectionString))
			})
		})
	})

	When("creating the cluster for cli", func() {
		var tmpDir string
		uid := "testuid"
		var file *os.File

		BeforeEach(func() {
			tmpDir = GinkgoT().TempDir()
			GinkgoT().Setenv("TMPDIR", tmpDir)

			var err error
			file, err = createClusterFileForCommandLine(&fdbv1beta2.FoundationDBCluster{
				ObjectMeta: metav1.ObjectMeta{
					UID: types.UID(uid),
				},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			Expect(file.Close()).To(Succeed())
		})

		It("should create the temp cluster file for the cli", func() {
			expectedDir := path.Join(tmpDir, fmt.Sprintf("%s-cli", uid))
			Expect(expectedDir).To(BeADirectory())
			Expect(file.Name()).To(BeAnExistingFile())
		})
	})
})
