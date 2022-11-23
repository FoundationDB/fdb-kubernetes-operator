/*
 * error_helper_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021-2022 Apple Inc. and the FoundationDB project authors
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
	"fmt"
	"net"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Internal error helper", func() {
	When("checking if an error is a network error", func() {
		type testCase struct {
			err      error
			expected bool
		}

		DescribeTable("parse the status",
			func(tc testCase) {
				Expect(IsNetworkError(tc.err)).To(Equal(tc.expected))
			},
			Entry("simple error",
				testCase{
					err:      fmt.Errorf("test"),
					expected: false,
				}),
			Entry("simple network error",
				testCase{
					err:      &net.OpError{Op: "mock", Err: fmt.Errorf("not reachable")},
					expected: true,
				}),
			Entry("wrapped simple error",
				testCase{
					err:      fmt.Errorf("test : %w", fmt.Errorf("test")),
					expected: false,
				}),
			Entry("wrapped network error",
				testCase{
					err:      fmt.Errorf("test : %w", &net.OpError{Op: "mock", Err: fmt.Errorf("not reachable")}),
					expected: true,
				}),
		)
	})

	When("checking if an error is a quota exceeded error", func() {
		type testCase struct {
			err      error
			expected bool
		}

		DescribeTable("parse the status",
			func(tc testCase) {
				Expect(IsQuotaExceeded(tc.err)).To(Equal(tc.expected))
			},
			Entry("Admission error",
				testCase{
					err:      apierrors.NewForbidden(schema.GroupResource{}, "test", fmt.Errorf("exceeded quota: todo")),
					expected: true,
				}),
			Entry("Different forbidden error",
				testCase{
					err:      apierrors.NewForbidden(schema.GroupResource{}, "test", fmt.Errorf("not allowed")),
					expected: false,
				}),
			Entry("simple error",
				testCase{
					err:      fmt.Errorf("error"),
					expected: false,
				}),
		)
	})

	When("checking if an error is a timeout error", func() {
		type testCase struct {
			err      error
			expected bool
		}

		DescribeTable("it should detect the timeout error",
			func(tc testCase) {
				Expect(IsTimeoutError(tc.err)).To(Equal(tc.expected))
			},
			Entry("simple error",
				testCase{
					err:      fmt.Errorf("test"),
					expected: false,
				}),
			Entry("simple timeout error",
				testCase{
					err:      fdbv1beta2.TimeoutError{Err: fmt.Errorf("not reachable")},
					expected: true,
				}),
			Entry("wrapped timeout error",
				testCase{
					err:      fmt.Errorf("test : %w", fdbv1beta2.TimeoutError{Err: fmt.Errorf("not reachable")}),
					expected: true,
				}),
		)
	})
})
