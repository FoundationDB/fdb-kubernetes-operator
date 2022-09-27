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
	"context"
	"strings"
	"testing"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/helper"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
)

func TestCreateSingleFDBCluster(t *testing.T) {
	g := NewGomegaWithT(t)

	testVersions := helper.GetTestFDBVersions()
	createSingleClusterFeatures := make([]features.Feature, 0, len(testVersions))

	for _, version := range testVersions {
		clusterName := "test" + strings.ReplaceAll(version.Compact(), ".", "-")
		te := features.
			New("create single cluster for "+version.Compact()).
			WithLabel("", ""). // TODO (johscheuer): add actual labels
			Setup(func(ctx context.Context, t *testing.T, config *envconf.Config) context.Context {
				testCluster := &fdbv1beta2.FoundationDBCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterName,
						Namespace: config.Namespace(),
					},
					Spec: fdbv1beta2.FoundationDBClusterSpec{
						Version: version.String(),
					},
				}

				g.Expect(config.Client().Resources().Create(ctx, testCluster)).NotTo(HaveOccurred())

				// TODO (johscheuer): Add here a wait function until cluster is reconciled.
				return ctx
			}).
			Assess("it should create an FDB cluster in "+version.Compact(), func(ctx context.Context, t *testing.T, config *envconf.Config) context.Context {
				testCluster := &fdbv1beta2.FoundationDBCluster{}

				g.Expect(config.Client().Resources().Get(ctx, clusterName, config.Namespace(), testCluster)).NotTo(HaveOccurred())
				g.Expect(testCluster.Spec.Version).To(Equal(version.String()))

				// TODO (johscheuer): Check here if Pods are created.

				// TODO (johscheuer): Check here if the FDB cluster is available.

				return ctx
			}).
			Assess("", func(ctx context.Context, t *testing.T, config *envconf.Config) context.Context {

				return ctx
			}).
			Teardown(func(ctx context.Context, t *testing.T, config *envconf.Config) context.Context {
				testCluster := &fdbv1beta2.FoundationDBCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      clusterName,
						Namespace: config.Namespace(),
					},
				}

				g.Expect(config.Client().Resources().Delete(ctx, testCluster)).NotTo(HaveOccurred())

				return ctx
			}).Feature()

		createSingleClusterFeatures = append(createSingleClusterFeatures, te)
	}

	testenv.Test(t, createSingleClusterFeatures...)
}
