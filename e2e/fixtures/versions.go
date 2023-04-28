/*
 * versions.go
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

package fixtures

import (
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"

	"github.com/onsi/gomega"
)

// VersionsAreProtocolCompatible returns true if versionA and versionB are protocol compatible e.g. a patch upgrade.
func VersionsAreProtocolCompatible(versionA string, versionB string) bool {
	aFdbVersion, err := fdbv1beta2.ParseFdbVersion(versionA)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	bFdbVersion, err := fdbv1beta2.ParseFdbVersion(versionB)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	return aFdbVersion.IsProtocolCompatible(bFdbVersion)
}
