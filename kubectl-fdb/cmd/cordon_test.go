/*
 * cordon_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021 Apple Inc. and the FoundationDB project authors
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

package cmd

import (
	"context"
	"fmt"
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("[plugin] cordon command", func() {
	When("running cordon command", func() {
		type testCase struct {
			nodes                                     []string
			WithExclusion                             bool
			ExpectedInstancesToRemove                 []string
			ExpectedInstancesToRemoveWithoutExclusion []string
			clusterName                               string
			customLabels                              []string
		}

		BeforeEach(func() {
			// creating pods for first cluster.
			Expect(createPods(clusterName, namespace, 1)).NotTo(HaveOccurred())

			// creating a second cluster
			cluster2 := createCluster("test2", namespace)
			Expect(k8sClient.Create(context.TODO(), cluster2)).NotTo(HaveOccurred())
			Expect(createPods(cluster2.Name, namespace, 3)).NotTo(HaveOccurred())
		})

		DescribeTable("should cordon all targeted processes",
			func(input testCase) {
				err := cordonNode(k8sClient, input.clusterName, input.nodes, namespace, input.WithExclusion, false, 0, input.customLabels)
				Expect(err).NotTo(HaveOccurred())

				var clusterNames []string
				if len(input.clusterName) == 0 {
					clusterNames = []string{clusterName, "test2"}
				} else {
					clusterNames = []string{input.clusterName}
				}

				var currentInstancesToRemove []string
				var currentInstancesToRemoveWithoutExclusion []string
				for _, currentClusterName := range clusterNames {
					var resCluster fdbv1beta2.FoundationDBCluster
					err = k8sClient.Get(context.Background(), client.ObjectKey{
						Namespace: namespace,
						Name:      currentClusterName,
					}, &resCluster)
					currentInstancesToRemove = append(currentInstancesToRemove, resCluster.Spec.ProcessGroupsToRemove...)
					currentInstancesToRemoveWithoutExclusion = append(currentInstancesToRemoveWithoutExclusion, resCluster.Spec.ProcessGroupsToRemoveWithoutExclusion...)
				}

				Expect(err).NotTo(HaveOccurred())
				// Use equality.Semantic.DeepEqual here since the Equal check of gomega is to strict
				Expect(equality.Semantic.DeepEqual(input.ExpectedInstancesToRemove, currentInstancesToRemove)).To(BeTrue())
				Expect(equality.Semantic.DeepEqual(input.ExpectedInstancesToRemoveWithoutExclusion, currentInstancesToRemoveWithoutExclusion)).To(BeTrue())
			},
			Entry("Cordon node with exclusion",
				testCase{
					nodes:                     []string{"node-1"},
					WithExclusion:             true,
					ExpectedInstancesToRemove: []string{"instance-1"},
					ExpectedInstancesToRemoveWithoutExclusion: []string{},
					clusterName:  cluster.Name,
					customLabels: nil,
				}),
			Entry("Cordon node without exclusion",
				testCase{
					nodes:                     []string{"node-1"},
					WithExclusion:             false,
					ExpectedInstancesToRemove: []string{},
					ExpectedInstancesToRemoveWithoutExclusion: []string{"instance-1"},
					clusterName:  cluster.Name,
					customLabels: nil,
				}),
			Entry("Cordon no nodes with exclusion",
				testCase{
					nodes:                     []string{""},
					WithExclusion:             true,
					ExpectedInstancesToRemove: []string{},
					ExpectedInstancesToRemoveWithoutExclusion: []string{},
					clusterName:  cluster.Name,
					customLabels: nil,
				}),
			Entry("Cordon no node nodes without exclusion",
				testCase{
					nodes:                     []string{""},
					WithExclusion:             false,
					ExpectedInstancesToRemove: []string{},
					ExpectedInstancesToRemoveWithoutExclusion: []string{},
					clusterName:  cluster.Name,
					customLabels: nil,
				}),
			Entry("Cordon all nodes with exclusion",
				testCase{
					nodes:                     []string{"node-1", "node-2"},
					WithExclusion:             true,
					ExpectedInstancesToRemove: []string{"instance-1", "instance-2"},
					ExpectedInstancesToRemoveWithoutExclusion: []string{},
					clusterName:  cluster.Name,
					customLabels: []string{fdbv1beta2.FDBClusterLabel},
				}),
			Entry("Cordon all nodes without exclusion",
				testCase{
					nodes:                     []string{"node-1", "node-2"},
					WithExclusion:             false,
					ExpectedInstancesToRemove: []string{},
					ExpectedInstancesToRemoveWithoutExclusion: []string{"instance-1", "instance-2"},
					clusterName:  cluster.Name,
					customLabels: []string{fdbv1beta2.FDBClusterLabel},
				}),
			Entry("Cordon node from second cluster without exclusion",
				testCase{
					nodes:                     []string{"node-1"},
					WithExclusion:             true,
					ExpectedInstancesToRemove: []string{"instance-3"},
					ExpectedInstancesToRemoveWithoutExclusion: []string{},
					clusterName:  "test2",
					customLabels: []string{fdbv1beta2.FDBClusterLabel},
				}),
			Entry("Cordon node from all clusters without exclusion",
				testCase{
					nodes:                     []string{"node-1"},
					WithExclusion:             true,
					ExpectedInstancesToRemove: []string{"instance-1", "instance-3"},
					ExpectedInstancesToRemoveWithoutExclusion: []string{},
					clusterName:  "",
					customLabels: []string{fdbv1beta2.FDBClusterLabel},
				}),
			Entry("Cordon all node from all clusters with exclusion",
				testCase{
					nodes:                     []string{"node-1", "node-2"},
					WithExclusion:             true,
					ExpectedInstancesToRemove: []string{},
					ExpectedInstancesToRemoveWithoutExclusion: []string{"instance-1", "instance-2", "instance-3", "instance-4"},
					clusterName:  "",
					customLabels: []string{fdbv1beta2.FDBClusterLabel},
				}),
			Entry("Cordon no node nodes without exclusion with custom label",
				testCase{
					nodes:                     []string{""},
					WithExclusion:             false,
					ExpectedInstancesToRemove: []string{},
					ExpectedInstancesToRemoveWithoutExclusion: []string{},
					clusterName:  "",
					customLabels: []string{fdbv1beta2.FDBClusterLabel},
				}),
		)
	})
})

func createPods(inputClusterName string, inputNamespace string, id int) error {
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("instance-%d", id),
				Namespace: inputNamespace,
				Labels: map[string]string{
					fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
					fdbv1beta2.FDBClusterLabel:        inputClusterName,
					fdbv1beta2.FDBProcessGroupIDLabel: fmt.Sprintf("instance-%d", id),
				},
			},
			Spec: corev1.PodSpec{
				NodeName: "node-1",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("instance-%d", id+1),
				Namespace: inputNamespace,
				Labels: map[string]string{
					fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
					fdbv1beta2.FDBClusterLabel:        inputClusterName,
					fdbv1beta2.FDBProcessGroupIDLabel: fmt.Sprintf("instance-%d", id+1),
				},
			},
			Spec: corev1.PodSpec{
				NodeName: "node-2",
			},
		},
	}

	for _, pod := range pods {
		err := k8sClient.Create(context.TODO(), &pod)
		if err != nil {
			return err
		}
	}
	return nil
}
