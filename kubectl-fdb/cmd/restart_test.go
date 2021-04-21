/*
 * restart_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021 Apple Inc. and the FoundationDB project authors
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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

var _ = Describe("[plugin] root command", func() {
	When("running the root command without args", func() {
		var outBuffer bytes.Buffer
		var errBuffer bytes.Buffer
		var inBuffer bytes.Buffer

		BeforeEach(func() {
			// We use these buffers to check the input/output
			outBuffer = bytes.Buffer{}
			errBuffer = bytes.Buffer{}
			inBuffer = bytes.Buffer{}
		})

		It("should not throw an error", func() {
			cmd := NewRootCmd(genericclioptions.IOStreams{In: &inBuffer, Out: &outBuffer, ErrOut: &errBuffer})

			args := []string{"restart", "-c", "sample"}
			cmd.SetArgs(args)
			Expect(cmd.Execute()).NotTo(HaveOccurred())
		})
	})
})
