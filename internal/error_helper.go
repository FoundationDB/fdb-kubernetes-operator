/*
 * error_helper.go
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
	"errors"
	"net"
	"strings"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
)

// IsNetworkError returns true if the network is a network error net.Error
func IsNetworkError(err error) bool {
	for err != nil {
		if _, ok := err.(net.Error); ok {
			return true
		}

		err = errors.Unwrap(err)
	}

	return false
}

// IsTimeoutError returns true if the observed error was a timeout error
func IsTimeoutError(err error) bool {
	for err != nil {
		if strings.Contains(err.Error(), "Specified timeout reached") {
			return true
		}

		err = errors.Unwrap(err)
	}

	return false
}

// IsQuotaExceeded returns tru if the error returned by the Kubernetes API is a forbidden error with the error message
// that the quota was exceeded
func IsQuotaExceeded(err error) bool {
	if k8serrors.IsForbidden(err) {
		if strings.Contains(err.Error(), "exceeded quota") {
			return true
		}
	}

	return false
}
