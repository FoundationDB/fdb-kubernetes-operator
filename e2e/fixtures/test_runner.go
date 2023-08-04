/*
 * test_runner.go
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
	"flag"
	"log"
	"testing"

	"github.com/onsi/ginkgo/v2/types"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

// RunGinkgoTests sets up the current test suite to run Ginkgo tests,
// then invokes the tests.  It should be invoked by each top level Test*
// function.
func RunGinkgoTests(t *testing.T, name string) {
	// Setup logging
	log.SetFlags(log.LstdFlags)
	log.SetOutput(ginkgo.GinkgoWriter)
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, name)
}

// InitFlags sets up the custom flags for this test suite and returns the parsed FactoryOptions struct. This method will
// take care of including the command line flags from Ginkgo and the testing framework.
func InitFlags() *FactoryOptions {
	testing.Init()
	_, err := types.NewAttachedGinkgoFlagSet(flag.CommandLine, types.GinkgoFlags{}, nil, types.GinkgoFlagSections{}, types.GinkgoFlagSection{})
	if err != nil {
		log.Fatal(err)
	}
	testOptions := &FactoryOptions{}
	testOptions.BindFlags(flag.CommandLine)
	flag.Parse()

	return testOptions
}
