/*
 * operator_test.go
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

package operator

/*
This test suite includes functional tests to ensure normal operational tasks are working fine with multple storage server
per Pod. Those tests include replacements of healthy or fault Pods and setting different configurations.

The assumption is that every test case reverts the changes that were done on the cluster.
In order to improve the test speed we only create one FoundationDB cluster initially.
This cluster will be used for all tests.
*/

import (
	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/fixtures"
)

func init() {
	// Create the factory in the init method and create the cluster spec
	factory := fixtures.CreateFactory(fixtures.InitFlags())
	config := fixtures.DefaultClusterConfig(false)
	config.StorageServerPerPod = 2
	spec := factory.GenerateFDBClusterSpec(config)

	fixtures.OperatorTestSuite(factory, config, spec)
}
