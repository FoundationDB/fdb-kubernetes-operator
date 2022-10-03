//go:build e2e_test

/*
 * create_single_fdb_cluster_test.go
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

package e2e

import (
	"testing"

	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/helper"
	"sigs.k8s.io/e2e-framework/pkg/features"
)

func TestCreateSingleFDBCluster(t *testing.T) {
	testVersions := helper.GetTestFDBVersions()
	createSingleClusterFeatures := make([]features.Feature, 0, len(testVersions))

	for _, version := range testVersions {
		createSingleClusterFeatures = append(createSingleClusterFeatures, helper.CreateSingleClusterTest(version, t))
	}

	testenv.Test(t, createSingleClusterFeatures...)
}
