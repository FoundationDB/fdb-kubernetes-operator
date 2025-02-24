/*
 * error_helper.go
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
	"errors"
	"fmt"
	"net"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
)

// ResourceNotDeleted can be returned if the resource is not yet deleted.
type ResourceNotDeleted struct {
	Resource client.Object
}

// Error returns the error message of the internal timeout error.
func (err ResourceNotDeleted) Error() string {
	return fmt.Sprintf("resource: %s is missing the deletion timestamp.", err.Resource.GetName())
}

// IsResourceNotDeleted returns true if provided error is of the type IsResourceNotDeleted.
func IsResourceNotDeleted(err error) bool {
	var targetErr ResourceNotDeleted
	return errors.As(err, &targetErr)
}

// IsNetworkError returns true if the network is a network error net.Error
func IsNetworkError(err error) bool {
	var netError net.Error
	return errors.As(err, &netError)
}

// IsTimeoutError returns true if the observed error was a timeout error
func IsTimeoutError(err error) bool {
	var timeoutError fdbv1beta2.TimeoutError
	return errors.As(err, &timeoutError)
}

// IsQuotaExceeded returns true if the error returned by the Kubernetes API is a forbidden error with the error message
// that the quota was exceeded
func IsQuotaExceeded(err error) bool {
	if k8serrors.IsForbidden(err) {
		if strings.Contains(err.Error(), "exceeded quota") {
			return true
		}
	}

	return false
}
