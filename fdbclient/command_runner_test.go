/*
 * command_runner_test.go
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
	"context"
	"strings"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("command_runner", func() {
	When("running a command", func() {
		var output []byte
		var err error

		BeforeEach(func() {
			runner := realCommandRunner{
				log: logr.Discard(),
			}

			output, err = runner.runCommand(context.TODO(), "echo", "hello")
		})

		It("should execute the command successfully", func() {
			Expect(err).NotTo(HaveOccurred())
			Expect(string(output)).To(Equal("hello\n"))
		})
	})

	When("filtering out the excluded FDB environment variables", func() {
		var envVariablesKeys []string

		BeforeEach(func() {
			GinkgoT().Setenv("FDB_NETWORK_OPTION_EXTERNAL_CLIENT_DIRECTORY", "")
			GinkgoT().Setenv("FDB_NETWORK_OPTION_IGNORE_EXTERNAL_CLIENT_FAILURES", "")
			GinkgoT().Setenv("FDB_NETWORK_OPTION_CLIENT_THREADS_PER_VERSION", "")

			GinkgoT().Setenv("FDB_TLS_CERTIFICATE_FILE", "")

			for _, env := range getEnvironmentVariablesWithoutExcludedFdbEnv() {
				envVariablesKeys = append(envVariablesKeys, strings.Split(env, "=")[0])
			}
		})

		It("should exclude the listed FDB variables but include all others", func() {
			Expect(envVariablesKeys).NotTo(ContainElement("FDB_NETWORK_OPTION_EXTERNAL_CLIENT_DIRECTORY"))
			Expect(envVariablesKeys).NotTo(ContainElement("FDB_NETWORK_OPTION_IGNORE_EXTERNAL_CLIENT_FAILURES"))
			Expect(envVariablesKeys).NotTo(ContainElement("FDB_NETWORK_OPTION_CLIENT_THREADS_PER_VERSION"))
			Expect(envVariablesKeys).To(ContainElement("FDB_TLS_CERTIFICATE_FILE"))
		})
	})
})
