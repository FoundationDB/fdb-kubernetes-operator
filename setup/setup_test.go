/*
 * setup_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2023 Apple Inc. and the FoundationDB project authors
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

package setup

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"io/fs"
	"os"
	"path"
)

var _ = Describe("setup", func() {
	var options Options

	When("no log output file is defined", func() {
		It("should return stdout as writer", func() {
			writer, err := setupLogger(options)
			Expect(err).NotTo(HaveOccurred())
			Expect(writer).To(BeIdenticalTo(os.Stdout))
		})
	})

	When("a log output file is defined", func() {
		var tmpDir, logFile string

		BeforeEach(func() {
			tmpDir = GinkgoT().TempDir()
			logFile = path.Join(tmpDir, "operator.logs")

			options = Options{
				LogFile: logFile,
			}
		})

		It("should create the log file with the right permissions", func() {
			_, err := setupLogger(options)
			Expect(err).NotTo(HaveOccurred())

			resultFile, err := os.Stat(logFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(logFile).To(Equal(path.Join(tmpDir, resultFile.Name())))
			// Default file mode is 0644
			Expect(resultFile.Mode()).To(Equal(fs.FileMode(0644)))
		})

		When("the log file already exists with the wrong permissions", func() {
			BeforeEach(func() {
				Expect(os.WriteFile(logFile, nil, 0600)).NotTo(HaveOccurred())
			})

			It("should correct the permission", func() {
				_, err := setupLogger(options)
				Expect(err).NotTo(HaveOccurred())

				resultFile, err := os.Stat(logFile)
				Expect(err).NotTo(HaveOccurred())
				Expect(logFile).To(Equal(path.Join(tmpDir, resultFile.Name())))
				// Default file mode is 0644
				Expect(resultFile.Mode()).To(Equal(fs.FileMode(0644)))
			})
		})

		When("file permissions are specified", func() {
			BeforeEach(func() {
				options.LogFilePermission = "0600"
			})

			It("should correct the permission", func() {
				_, err := setupLogger(options)
				Expect(err).NotTo(HaveOccurred())

				resultFile, err := os.Stat(logFile)
				Expect(err).NotTo(HaveOccurred())
				Expect(logFile).To(Equal(path.Join(tmpDir, resultFile.Name())))
				Expect(resultFile.Mode()).To(Equal(fs.FileMode(0600)))
			})
		})
	})
})
