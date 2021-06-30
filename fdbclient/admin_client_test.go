/*
 * admin_client_test.go
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

package fdbclient

import (
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
)

// Used to create a dummy os.FileInfo
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

var _ = Describe("admin_client_test", func() {
	Describe("helper methods", func() {
		Describe("parseExclusionOutput", func() {
			It("should map the output description to exclusion success", func() {
				output := "  10.1.56.36(Whole machine)  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.43(Whole machine)  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.52(Whole machine)  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.53(Whole machine)  ---- WARNING: Missing from cluster! Be sure that you excluded the correct processes" +
					" before removing them from the cluster!\n" +
					"  10.1.56.35(Whole machine)  ---- WARNING: Exclusion in progress! It is not safe to remove this process from the cluster\n" +
					"  10.1.56.56(Whole machine)  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"WARNING: 10.1.56.56:4500 is a coordinator!\n" +
					"Type `help coordinators' for information on how to change the\n" +
					"cluster's coordination servers before removing them."
				results := parseExclusionOutput(output)
				Expect(results).To(Equal(map[string]string{
					"10.1.56.36": "Success",
					"10.1.56.43": "Success",
					"10.1.56.52": "Success",
					"10.1.56.53": "Missing",
					"10.1.56.35": "In Progress",
					"10.1.56.56": "Success",
				}))
			})

			It("should handle a lack of suffices in the output", func() {
				output := "  10.1.56.36  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.43  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.52  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.53  ---- WARNING: Missing from cluster! Be sure that you excluded the correct processes" +
					" before removing them from the cluster!\n" +
					"  10.1.56.35  ---- WARNING: Exclusion in progress! It is not safe to remove this process from the cluster\n" +
					"  10.1.56.56  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"WARNING: 10.1.56.56:4500 is a coordinator!\n" +
					"Type `help coordinators' for information on how to change the\n" +
					"cluster's coordination servers before removing them."
				results := parseExclusionOutput(output)
				Expect(results).To(Equal(map[string]string{
					"10.1.56.36": "Success",
					"10.1.56.43": "Success",
					"10.1.56.52": "Success",
					"10.1.56.53": "Missing",
					"10.1.56.35": "In Progress",
					"10.1.56.56": "Success",
				}))
			})

			It("should handle ports in the output", func() {
				output := "  10.1.56.36:4500  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.43:4500  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.52:4500  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"  10.1.56.53:4500  ---- WARNING: Missing from cluster! Be sure that you excluded the correct processes" +
					" before removing them from the cluster!\n" +
					"  10.1.56.35:4500  ---- WARNING: Exclusion in progress! It is not safe to remove this process from the cluster\n" +
					"  10.1.56.56:4500  ---- Successfully excluded. It is now safe to remove this process from the cluster.\n" +
					"WARNING: 10.1.56.56:4500 is a coordinator!\n" +
					"Type `help coordinators' for information on how to change the\n" +
					"cluster's coordination servers before removing them."
				results := parseExclusionOutput(output)
				Expect(results).To(Equal(map[string]string{
					"10.1.56.36:4500": "Success",
					"10.1.56.43:4500": "Success",
					"10.1.56.52:4500": "Success",
					"10.1.56.53:4500": "Missing",
					"10.1.56.35:4500": "In Progress",
					"10.1.56.56:4500": "Success",
				}))
			})
		})
	})

	When("Removing warnings in JSON", func() {
		type testCase struct {
			input       string
			expected    string
			expectedErr error
		}

		DescribeTable("Test remove warnings in JSON string",
			func(tc testCase) {
				result, err := removeWarningsInJSON(tc.input)
				// We need the if statement to make ginkgo happy:
				//   Refusing to compare <nil> to <nil>.
				//   Be explicit and use BeNil() instead.
				//   This is to avoid mistakes where both sides of an assertion are erroneously uninitialized.
				// ¯\_(ツ)_/¯
				if tc.expectedErr == nil {
					Expect(err).To(BeNil())
				} else {
					Expect(err).To(Equal(tc.expectedErr))
				}
				Expect(result).To(Equal(tc.expected))
			},
			Entry("Valid JSON without warning",
				testCase{
					input:       "{}",
					expected:    "{}",
					expectedErr: nil,
				},
			),
			Entry("Valid JSON with warning",
				testCase{
					input: `
 # Warning Slow response
 
 {}`,
					expected:    "{}",
					expectedErr: nil,
				},
			),
			Entry("Invalid JSON",
				testCase{
					input:       "}",
					expected:    "",
					expectedErr: fmt.Errorf("the JSON string doesn't contain a starting '{'"),
				},
			),
		)
	})

	When("", func() {
		type testCase struct {
			fileInfo os.FileInfo
			time     time.Time
			expected bool
		}
		DescribeTable("Should return wther the file should be removed",
			func(tc testCase) {
				result, err := shouldRemoveLogFile(tc.fileInfo, tc.time)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(Equal(tc.expected))
			},
			Entry("When the FileInfo is a dir",
				testCase{
					fileInfo: dummyFileInfo{
						isDir:   true,
						modTime: time.Now(),
					},
					time:     time.Now(),
					expected: false,
				},
			),
			Entry("When the file is an operator log file",
				testCase{
					fileInfo: dummyFileInfo{
						name:    "operator.json",
						isDir:   true,
						modTime: time.Now(),
					},
					time:     time.Now(),
					expected: false,
				},
			),
			Entry("When the file is not older than 30 minutes",
				testCase{
					fileInfo: dummyFileInfo{
						name:    "trace.10.1.14.36.1.1625057172.rmuWOn.0.1.xml",
						isDir:   true,
						modTime: time.Now(),
					},
					time:     time.Now(),
					expected: false,
				},
			),
			Entry("When the file is a library file",
				testCase{
					fileInfo: dummyFileInfo{
						name:    "trace.10.1.14.36.1.1625057172.rmuWOn.0.1.xml",
						isDir:   true,
						modTime: time.Now().Add(-45 * time.Minute),
					},
					time:     time.Now(),
					expected: false,
				},
			),
			Entry("When the file is a library file",
				testCase{
					fileInfo: dummyFileInfo{
						name:    "trace.10.1.14.36.1337.1625057172.rmuWOn.0.1.xml",
						isDir:   true,
						modTime: time.Now().Add(-45 * time.Minute),
					},
					time:     time.Now(),
					expected: true,
				},
			),
		)
	})
})
