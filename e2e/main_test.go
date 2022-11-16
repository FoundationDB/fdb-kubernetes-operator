//go:build e2e_test

/*
 * main_test.go
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
	"context"
	"fmt"

	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/helper"

	// If testing with a cloud vendor managed cluster uncomment one of the below dependencies to properly get authorised.
	//_ "k8s.io/client-go/plugin/pkg/client/auth/azure" // auth for AKS clusters
	//_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"   // auth for GKE clusters
	//_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"  // auth for OIDC
	"os"
	"testing"

	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
)

var testenv env.Environment

func TestMain(m *testing.M) {
	testenv = env.New()

	cfg, err := envconf.NewFromFlags()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "envconf failed: %s\n", err)
		os.Exit(1)
	}

	testenv = env.NewWithConfig(cfg)

	testenv.Setup(
		envfuncs.SetupCRDs("../config/crd/bases", "*"),
		helper.RegisterFDBScheme,
		helper.WaitUntilCRDAvailable,
	)

	testenv.BeforeEachTest(func(ctx context.Context, cfg *envconf.Config, t *testing.T) (context.Context, error) {
		return helper.CreateNamespace(ctx, cfg, t, envconf.RandomName("ns", 4))
	}, helper.InstallOperator)

	testenv.AfterEachTest(helper.DeleteNamespace)

	testenv.Finish(
		envfuncs.TeardownCRDs("../config/crd/bases", "*"),
	)

	os.Exit(testenv.Run(m))
}
