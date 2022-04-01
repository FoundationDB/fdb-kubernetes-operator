/*
 * log_file_cleaner_test.go
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

package internal

import (
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// dummyFileInfo implements the os.FileInfo interface
type dummyFileInfo struct {
	isDir   bool
	modTime time.Time
	name    string
}

func (f dummyFileInfo) Name() string {
	return f.name
}

func (f dummyFileInfo) Size() int64 {
	return 0
}

func (f dummyFileInfo) Mode() os.FileMode {
	return 0
}

func (f dummyFileInfo) ModTime() time.Time {
	return f.modTime
}

func (f dummyFileInfo) IsDir() bool {
	return f.isDir
}

func (f dummyFileInfo) Sys() interface{} {
	return nil
}

var _ = Describe("", func() {
	When("we check old log files", func() {
		type testCase struct {
			fileInfo os.FileInfo
			time     time.Time
			expected bool
		}
		DescribeTable("should return if the file should be removed",
			func(tc testCase) {
				result, err := shouldRemoveLogFile(tc.fileInfo, tc.time, 30*time.Minute)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(tc.expected))
			},
			Entry("when the FileInfo is a dir",
				testCase{
					fileInfo: dummyFileInfo{
						isDir:   true,
						modTime: time.Now(),
					},
					time:     time.Now(),
					expected: false,
				},
			),
			Entry("when the file is an operator log file",
				testCase{
					fileInfo: dummyFileInfo{
						name:    "operator.json",
						isDir:   false,
						modTime: time.Now(),
					},
					time:     time.Now(),
					expected: false,
				},
			),
			Entry("when the file is not older than 30 minutes",
				testCase{
					fileInfo: dummyFileInfo{
						name:    "trace.10.1.14.36.1.1625057172.rmuWOn.0.1.xml",
						isDir:   false,
						modTime: time.Now().Add(-25 * time.Minute),
					},
					time:     time.Now(),
					expected: false,
				},
			),
			Entry("when the file is a library log file",
				testCase{
					fileInfo: dummyFileInfo{
						name:    "trace.10.1.14.36.1.1625057172.rmuWOn.0.1.xml",
						isDir:   false,
						modTime: time.Now().Add(-45 * time.Minute),
					},
					time:     time.Now(),
					expected: false,
				},
			),
			Entry("when the file is a fdbcli log file",
				testCase{
					fileInfo: dummyFileInfo{
						name:    "trace.10.1.14.36.1337.1625057172.rmuWOn.0.1.xml",
						isDir:   false,
						modTime: time.Now().Add(-45 * time.Minute),
					},
					time:     time.Now(),
					expected: true,
				},
			),
		)
	})
})
