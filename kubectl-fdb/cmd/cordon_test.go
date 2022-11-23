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
		}

		BeforeEach(func() {
			pods := []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "instance-1",
						Namespace: namespace,
						Labels: map[string]string{
							fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
							fdbv1beta2.FDBClusterLabel:        clusterName,
							fdbv1beta2.FDBProcessGroupIDLabel: "instance-1",
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "node-1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "instance-2",
						Namespace: namespace,
						Labels: map[string]string{
							fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
							fdbv1beta2.FDBClusterLabel:        clusterName,
							fdbv1beta2.FDBProcessGroupIDLabel: "instance-2",
						},
					},
					Spec: corev1.PodSpec{
						NodeName: "node-2",
					},
				},
			}

			for _, pod := range pods {
				Expect(k8sClient.Create(context.TODO(), &pod)).NotTo(HaveOccurred())
			}
		})

		DescribeTable("should cordon all targeted processes",
			func(input testCase) {
				err := cordonNode(k8sClient, cluster, input.nodes, namespace, input.WithExclusion, false, 0)
				Expect(err).NotTo(HaveOccurred())

				var resCluster fdbv1beta2.FoundationDBCluster
				err = k8sClient.Get(context.Background(), client.ObjectKey{
					Namespace: namespace,
					Name:      clusterName,
				}, &resCluster)

				Expect(err).NotTo(HaveOccurred())
				// Use equality.Semantic.DeepEqual here since the Equal check of gomega is to strict
				Expect(equality.Semantic.DeepEqual(input.ExpectedInstancesToRemove, resCluster.Spec.ProcessGroupsToRemove)).To(BeTrue())
				Expect(equality.Semantic.DeepEqual(input.ExpectedInstancesToRemoveWithoutExclusion, resCluster.Spec.ProcessGroupsToRemoveWithoutExclusion)).To(BeTrue())
			},
			Entry("Cordon node with exclusion",
				testCase{
					nodes:                     []string{"node-1"},
					WithExclusion:             true,
					ExpectedInstancesToRemove: []string{"instance-1"},
					ExpectedInstancesToRemoveWithoutExclusion: []string{},
				}),
			Entry("Cordon node without exclusion",
				testCase{
					nodes:                     []string{"node-1"},
					WithExclusion:             false,
					ExpectedInstancesToRemove: []string{},
					ExpectedInstancesToRemoveWithoutExclusion: []string{"instance-1"},
				}),
			Entry("Cordon no nodes with exclusion",
				testCase{
					nodes:                     []string{""},
					WithExclusion:             true,
					ExpectedInstancesToRemove: []string{},
					ExpectedInstancesToRemoveWithoutExclusion: []string{},
				}),
			Entry("Cordon no node nodes without exclusion",
				testCase{
					nodes:                     []string{""},
					WithExclusion:             false,
					ExpectedInstancesToRemove: []string{},
					ExpectedInstancesToRemoveWithoutExclusion: []string{},
				}),
			Entry("Cordon all nodes with exclusion",
				testCase{
					nodes:                     []string{"node-1", "node-2"},
					WithExclusion:             true,
					ExpectedInstancesToRemove: []string{"instance-1", "instance-2"},
					ExpectedInstancesToRemoveWithoutExclusion: []string{},
				}),
			Entry("Cordon all nodes without exclusion",
				testCase{
					nodes:                     []string{"node-1", "node-2"},
					WithExclusion:             false,
					ExpectedInstancesToRemove: []string{},
					ExpectedInstancesToRemoveWithoutExclusion: []string{"instance-1", "instance-2"},
				}),
		)
	})
})
