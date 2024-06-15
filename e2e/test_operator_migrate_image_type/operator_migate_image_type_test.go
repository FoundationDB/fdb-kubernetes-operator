/*
 * operator_migrate_image_type_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2023-2024 Apple Inc. and the FoundationDB project authors
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

This test suite includes tests for migrating between the different image types.
*/

import (
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/e2e/fixtures"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/pointer"
	"log"
)

var (
	factory     *fixtures.Factory
	fdbCluster  *fixtures.FdbCluster
	testOptions *fixtures.FactoryOptions
)

func init() {
	testOptions = fixtures.InitFlags()
}

var _ = BeforeSuite(func() {
	factory = fixtures.CreateFactory(testOptions)
})

var _ = AfterSuite(func() {
	if CurrentSpecReport().Failed() {
		log.Printf("failed due to %s", CurrentSpecReport().FailureMessage())
	}
	factory.Shutdown()
})

var _ = PDescribe("Operator Migrate Image Type", Label("e2e"), func() {
	When("migrating from split to unified", func() {
		BeforeEach(func() {
			config := fixtures.DefaultClusterConfig(false)
			config.UseUnifiedImage = pointer.Bool(false)
			fdbCluster = factory.CreateFdbCluster(
				config,
				factory.GetClusterOptions()...,
			)

			// Load some data async into the cluster. We will only block as long as the Job is created.
			factory.CreateDataLoaderIfAbsent(fdbCluster)

			// Update the cluster spec to run with the unified image.
			spec := fdbCluster.GetCluster().Spec.DeepCopy()
			imageType := fdbv1beta2.ImageTypeUnified
			spec.ImageType = &imageType
			// Generate the new config to make use of the unified images.
			overrides := factory.GetMainContainerOverrides(false, true)
			overrides.EnableTLS = spec.MainContainer.EnableTLS
			spec.MainContainer = overrides
			fdbCluster.UpdateClusterSpecWithSpec(spec)
			Expect(fdbCluster.WaitForReconciliation()).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			Expect(fdbCluster.Destroy()).NotTo(HaveOccurred())
		})

		It("should convert the cluster", func() {
			// Make sure we didn't lose data.
			fdbCluster.EnsureTeamTrackersAreHealthy()
			fdbCluster.EnsureTeamTrackersHaveMinReplicas()

			unifiedImage := factory.GetUnifiedFoundationDBImage()
			pods := fdbCluster.GetPods()
			for _, pod := range pods.Items {
				// Ignore Pods that are pending the deletion.
				if !pod.DeletionTimestamp.IsZero() {
					continue
				}

				// With the unified image no init containers are used.
				Expect(pod.Spec.InitContainers).To(HaveLen(0))
				// Make sure they run the unified image.
				Expect(pod.Spec.Containers[0].Image).To(ContainSubstring(unifiedImage))
			}
		})
	})

	When("migrating from unified to split", func() {
		BeforeEach(func() {
			config := fixtures.DefaultClusterConfig(false)
			config.UseUnifiedImage = pointer.Bool(true)
			fdbCluster = factory.CreateFdbCluster(
				config,
				factory.GetClusterOptions()...,
			)

			// Load some data async into the cluster. We will only block as long as the Job is created.
			factory.CreateDataLoaderIfAbsent(fdbCluster)

			// Update the cluster spec to run with the split image.
			spec := fdbCluster.GetCluster().Spec.DeepCopy()
			imageType := fdbv1beta2.ImageTypeSplit
			spec.ImageType = &imageType
			// Generate the new config to make use of the split images.
			overrides := factory.GetMainContainerOverrides(false, false)
			overrides.EnableTLS = spec.MainContainer.EnableTLS
			spec.MainContainer = overrides
			fdbCluster.UpdateClusterSpecWithSpec(spec)
			Expect(fdbCluster.WaitForReconciliation()).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			Expect(fdbCluster.Destroy()).NotTo(HaveOccurred())
		})

		It("should convert the cluster", func() {
			// Make sure we didn't lose data.
			fdbCluster.EnsureTeamTrackersAreHealthy()
			fdbCluster.EnsureTeamTrackersHaveMinReplicas()

			fdbImage := factory.GetFoundationDBImage()
			sidecarImage := factory.GetSidecarImage()
			pods := fdbCluster.GetPods()
			for _, pod := range pods.Items {
				// Ignore Pods that are pending the deletion.
				if !pod.DeletionTimestamp.IsZero() {
					continue
				}
				Expect(pod.Spec.InitContainers).NotTo(HaveLen(0))
				// Make sure they run the split image.
				for _, container := range pod.Spec.Containers {
					if container.Name == fdbv1beta2.MainContainerName {
						Expect(container.Image).To(ContainSubstring(fdbImage))
					} else {
						Expect(container.Image).To(ContainSubstring(sidecarImage))
					}
				}
			}
		})
	})
})
