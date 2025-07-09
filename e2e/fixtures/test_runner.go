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
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/onsi/ginkgo/v2/types"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

// testSuiteName will be used to prevent namespace conflicts between concurrent running test suites.
var testSuiteName string

// RunGinkgoTests sets up the current test suite to run Ginkgo tests,
// then invokes the tests.  It should be invoked by each top level Test*
// function.
func RunGinkgoTests(t *testing.T, name string) {
	// Setup logging
	log.SetFlags(log.LstdFlags)
	log.SetOutput(ginkgo.GinkgoWriter)
	// Do not output the logs from the controller-runtime. We only use it as a client.
	// Without this call we get a stack trace.
	ctrl.SetLogger(logr.Discard())
	gomega.RegisterFailHandler(ginkgo.Fail)
	_, inCI := os.LookupEnv("CODEBUILD_SRC_DIR")
	if inCI {
		// We wait up to 300 seconds before executing the code. In CI all tests will run in parallel and adding some
		// random wait time should help in making those tests more stable.
		waitDuration := time.Duration(rand.Intn(300)) * time.Second
		ginkgo.GinkgoLogr.Info("waiting for", waitDuration.String(), "before executing test suite")
		time.Sleep(waitDuration)
	}
	ginkgo.RunSpecs(t, name)
}

// InitFlags sets up the custom flags for this test suite and returns the parsed FactoryOptions struct. This method will
// take care of including the command line flags from Ginkgo and the testing framework.
func InitFlags() *FactoryOptions {
	testing.Init()
	_, err := types.NewAttachedGinkgoFlagSet(
		flag.CommandLine,
		types.GinkgoFlags{},
		nil,
		types.GinkgoFlagSections{},
		types.GinkgoFlagSection{},
	)
	if err != nil {
		log.Fatal(err)
	}
	testOptions := &FactoryOptions{}
	testOptions.BindFlags(flag.CommandLine)
	flag.Parse()

	return testOptions
}

// SetTestSuiteName will set the test suite name for the current test suite. You have to ensure that this test suite
// name is unique across all test suites.
func SetTestSuiteName(name string) {
	testSuiteName = name
}
