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
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"

	// If testing with a cloud vendor managed cluster uncomment one of the below dependencies to properly get authorised.
	//_ "k8s.io/client-go/plugin/pkg/client/auth/azure" // auth for AKS clusters
	//_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"   // auth for GKE clusters
	//_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"  // auth for OIDC
	"os"
	"testing"

	"sigs.k8s.io/e2e-framework/klient/conf"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/envfuncs"
)

var testenv env.Environment

func TestMain(m *testing.M) {
	testenv = env.New()
	// TODO (johscheuer): make this a flag
	namespace := envconf.RandomName("sample-ns", 16)

	// TODO (johscheuer): make this a flag
	if os.Getenv("REAL_CLUSTER") == "true" {
		path := conf.ResolveKubeConfigFile()
		cfg := envconf.NewWithKubeConfig(path)
		testenv = env.NewWithConfig(cfg)

		testenv.Setup(
			envfuncs.CreateNamespace(namespace),
			envfuncs.SetupCRDs("../config/crd/bases", "*"),
		)
		testenv.Finish(
			envfuncs.TeardownCRDs("../config/crd/bases", "*"),
			envfuncs.DeleteNamespace(namespace),
		)
	} else {
		kindClusterName := envconf.RandomName("kind-with-config", 16)

		// TODO (johscheuer): install fdb operator with latest version
		testenv.Setup(
			envfuncs.CreateKindCluster(kindClusterName),
			envfuncs.SetupCRDs("../config/crd/bases", "*"),
			// Register the fdbv1beta2 scheme to the rest config
			func(ctx context.Context, config *envconf.Config) (context.Context, error) {
				r, err := resources.New(config.Client().RESTConfig())
				if err != nil {
					return ctx, err
				}

				err = fdbv1beta2.AddToScheme(r.GetScheme())

				return ctx, err
			},
			func(ctx context.Context, config *envconf.Config) (context.Context, error) {
				// TODO (johscheuer): Add a proper test to ensure the CRD is available.
				time.Sleep(1 * time.Second)
				return ctx, nil
			},
			envfuncs.CreateNamespace(namespace),
		)

		testenv.Finish(
			envfuncs.DeleteNamespace(namespace),
			envfuncs.TeardownCRDs("../config/crd/bases", "*"),
			envfuncs.DestroyKindCluster(kindClusterName),
		)
	}

	os.Exit(testenv.Run(m))
}
