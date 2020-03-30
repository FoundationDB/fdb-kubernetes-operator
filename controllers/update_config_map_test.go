/*
 * update_config_map_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020 Apple Inc. and the FoundationDB project authors
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

package controllers

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"context"
	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("UpdateConfigMap", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var shouldContinue bool
	var err error
	var originalGeneration int64
	var configMapName types.NamespacedName
	var configMap *corev1.ConfigMap
	var originalData map[string]string

	BeforeEach(func() {
		ClearMockAdminClients()
		cluster = createReconciledCluster()
		Expect(err).NotTo(HaveOccurred())
		shouldContinue = true
		originalGeneration = cluster.ObjectMeta.Generation

		configMap = &corev1.ConfigMap{}
		configMapName = types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name + "-config"}
		err = k8sClient.Get(context.TODO(), configMapName, configMap)
		Expect(err).NotTo(HaveOccurred())
		originalData = configMap.Data
	})

	AfterEach(func() {
		if cluster.ObjectMeta.Generation > originalGeneration {
			Eventually(func() (int64, error) { return reloadCluster(k8sClient, cluster) }, 5).Should(BeNumerically(">", originalGeneration))
		}
		cleanupCluster(cluster)
	})

	JustBeforeEach(func() {
		err = runClusterReconciler(UpdateConfigMap{}, cluster, shouldContinue)
	})

	Context("with a reconciled cluster", func() {
		It("should not return an error", func() {
			Expect(err).To(BeNil())
		})
	})

	Context("with a change to the config map annotations", func() {
		BeforeEach(func() {
			cluster.Spec.ConfigMap = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"test-annotation": "test-value",
					},
				},
			}
		})

		It("should not change the data", func() {
			err = k8sClient.Get(context.TODO(), configMapName, configMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(configMap.Data).To(Equal(originalData))
		})

		It("should update the annotations", func() {
			err = k8sClient.Get(context.TODO(), configMapName, configMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(configMap.Annotations["test-annotation"]).To(Equal("test-value"))
		})
	})

	Context("with a change to the config map labels", func() {
		BeforeEach(func() {
			cluster.Spec.ConfigMap = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"test-label": "test-value",
					},
				},
			}
		})

		It("should not change the data", func() {
			err = k8sClient.Get(context.TODO(), configMapName, configMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(configMap.Data).To(Equal(originalData))
		})

		It("should update the labels", func() {
			err = k8sClient.Get(context.TODO(), configMapName, configMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(configMap.Labels["test-label"]).To(Equal("test-value"))
		})
	})

	Context("with a change to the data in the config map", func() {
		BeforeEach(func() {
			cluster.Spec.CustomParameters = []string{"locality_test=test1"}
		})

		It("should update the data", func() {
			err = k8sClient.Get(context.TODO(), configMapName, configMap)
			Expect(err).NotTo(HaveOccurred())
			Expect(configMap.Data).NotTo(Equal(originalData))

			desiredConfigMap, err := GetConfigMap(context.TODO(), cluster, clusterReconciler)
			Expect(err).NotTo(HaveOccurred())
			Expect(configMap.Data).To(Equal(desiredConfigMap.Data))
		})
	})
})
