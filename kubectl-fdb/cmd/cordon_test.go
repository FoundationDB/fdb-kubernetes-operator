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
	"bytes"
	"context"
	"fmt"
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var secondCluster *fdbv1beta2.FoundationDBCluster
var secondClusterName = "test2"

var _ = Describe("[plugin] cordon command", func() {
	When("running cordon command", func() {
		type testCase struct {
			nodes                                     []string
			WithExclusion                             bool
			ExpectedInstancesToRemove                 []string
			ExpectedInstancesToRemoveWithoutExclusion []string
			clusterName                               string
			clusterLabel                              string
		}

		BeforeEach(func() {
			// creating Pods for first cluster.
			Expect(createPods(clusterName, namespace)).NotTo(HaveOccurred())

			// creating a second cluster
			secondCluster = generateClusterStruct(secondClusterName, namespace)
			Expect(k8sClient.Create(context.TODO(), secondCluster)).NotTo(HaveOccurred())
			Expect(createPods(secondClusterName, namespace)).NotTo(HaveOccurred())
		})

		DescribeTable("should cordon all targeted processes",
			func(input testCase) {
				// We use these buffers to check the input/output
				outBuffer := bytes.Buffer{}
				errBuffer := bytes.Buffer{}
				inBuffer := bytes.Buffer{}

				cmd := newAnalyzeCmd(genericclioptions.IOStreams{In: &inBuffer, Out: &outBuffer, ErrOut: &errBuffer})
				err := cordonNode(cmd, k8sClient, input.clusterName, input.nodes, namespace, input.WithExclusion, false, 0, input.clusterLabel)
				Expect(err).NotTo(HaveOccurred())

				clusterNames := []string{clusterName, secondClusterName}
				var instancesToRemove []string
				var instancesToRemoveWithoutExclusion []string
				for _, clusterName := range clusterNames {
					var resCluster fdbv1beta2.FoundationDBCluster
					err = k8sClient.Get(context.Background(), client.ObjectKey{
						Namespace: namespace,
						Name:      clusterName,
					}, &resCluster)
					Expect(err).NotTo(HaveOccurred())
					instancesToRemove = append(instancesToRemove, resCluster.Spec.ProcessGroupsToRemove...)
					instancesToRemoveWithoutExclusion = append(instancesToRemoveWithoutExclusion, resCluster.Spec.ProcessGroupsToRemoveWithoutExclusion...)
				}

				Expect(input.ExpectedInstancesToRemove).To(ConsistOf(instancesToRemove))
				Expect(input.ExpectedInstancesToRemoveWithoutExclusion).To(ConsistOf(instancesToRemoveWithoutExclusion))
			},
			Entry("Cordon node with exclusion",
				testCase{
					nodes:                     []string{"node-1"},
					WithExclusion:             true,
					ExpectedInstancesToRemove: []string{fmt.Sprintf("%s-instance-1", clusterName)},
					ExpectedInstancesToRemoveWithoutExclusion: []string{},
					clusterName:  clusterName,
					clusterLabel: "",
				}),
			Entry("Cordon node without exclusion",
				testCase{
					nodes:                     []string{"node-1"},
					WithExclusion:             false,
					ExpectedInstancesToRemove: []string{},
					ExpectedInstancesToRemoveWithoutExclusion: []string{fmt.Sprintf("%s-instance-1", clusterName)},
					clusterName:  clusterName,
					clusterLabel: "",
				}),
			Entry("Cordon no nodes with exclusion",
				testCase{
					nodes:                     []string{""},
					WithExclusion:             true,
					ExpectedInstancesToRemove: []string{},
					ExpectedInstancesToRemoveWithoutExclusion: []string{},
					clusterName:  clusterName,
					clusterLabel: "",
				}),
			Entry("Cordon no node nodes without exclusion",
				testCase{
					nodes:                     []string{""},
					WithExclusion:             false,
					ExpectedInstancesToRemove: []string{},
					ExpectedInstancesToRemoveWithoutExclusion: []string{},
					clusterName:  clusterName,
					clusterLabel: "",
				}),
			Entry("Cordon all nodes with exclusion",
				testCase{
					nodes:         []string{"node-1", "node-2"},
					WithExclusion: true,
					ExpectedInstancesToRemove: []string{
						fmt.Sprintf("%s-instance-1", clusterName),
						fmt.Sprintf("%s-instance-2", clusterName),
					},
					ExpectedInstancesToRemoveWithoutExclusion: []string{},
					clusterName:  clusterName,
					clusterLabel: "",
				}),
			Entry("Cordon all nodes without exclusion",
				testCase{
					nodes:                     []string{"node-1", "node-2"},
					WithExclusion:             false,
					ExpectedInstancesToRemove: []string{},
					ExpectedInstancesToRemoveWithoutExclusion: []string{
						fmt.Sprintf("%s-instance-1", clusterName),
						fmt.Sprintf("%s-instance-2", clusterName),
					},
					clusterName:  clusterName,
					clusterLabel: "",
				}),
			Entry("Cordon node from second cluster without exclusion",
				testCase{
					nodes:                     []string{"node-1"},
					WithExclusion:             true,
					ExpectedInstancesToRemove: []string{fmt.Sprintf("%s-instance-1", secondClusterName)},
					ExpectedInstancesToRemoveWithoutExclusion: []string{},
					clusterName:  secondClusterName,
					clusterLabel: "",
				}),
			Entry("Cordon node from second cluster without exclusion with cluster label",
				testCase{
					nodes:                     []string{"node-1"},
					WithExclusion:             true,
					ExpectedInstancesToRemove: []string{fmt.Sprintf("%s-instance-1", secondClusterName)},
					ExpectedInstancesToRemoveWithoutExclusion: []string{},
					clusterName:  secondClusterName,
					clusterLabel: fdbv1beta2.FDBClusterLabel,
				}),
			Entry("Cordon node from all clusters with exclusion",
				testCase{
					nodes:         []string{"node-1"},
					WithExclusion: true,
					ExpectedInstancesToRemove: []string{
						fmt.Sprintf("%s-instance-1", clusterName),
						fmt.Sprintf("%s-instance-1", secondClusterName),
					},
					ExpectedInstancesToRemoveWithoutExclusion: []string{},
					clusterName:  "",
					clusterLabel: fdbv1beta2.FDBClusterLabel,
				}),
			Entry("Cordon all node from all clusters without exclusion",
				testCase{
					nodes:                     []string{"node-1", "node-2"},
					WithExclusion:             false,
					ExpectedInstancesToRemove: []string{},
					ExpectedInstancesToRemoveWithoutExclusion: []string{
						fmt.Sprintf("%s-instance-1", clusterName),
						fmt.Sprintf("%s-instance-2", clusterName),
						fmt.Sprintf("%s-instance-1", secondClusterName),
						fmt.Sprintf("%s-instance-2", secondClusterName),
					},
					clusterName:  "",
					clusterLabel: fdbv1beta2.FDBClusterLabel,
				}),
			Entry("Cordon no node nodes without exclusion with custom label",
				testCase{
					nodes:                     []string{""},
					WithExclusion:             false,
					ExpectedInstancesToRemove: []string{},
					ExpectedInstancesToRemoveWithoutExclusion: []string{},
					clusterName:  "",
					clusterLabel: fdbv1beta2.FDBClusterLabel,
				}),
		)
	})
})

func createPods(clusterName string, namespace string) error {
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-instance-1", clusterName),
				Namespace: namespace,
				Labels: map[string]string{
					fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
					fdbv1beta2.FDBClusterLabel:        clusterName,
					fdbv1beta2.FDBProcessGroupIDLabel: fmt.Sprintf("%s-instance-1", clusterName),
				},
			},
			Spec: corev1.PodSpec{
				NodeName: "node-1",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-instance-2", clusterName),
				Namespace: namespace,
				Labels: map[string]string{
					fdbv1beta2.FDBProcessClassLabel:   string(fdbv1beta2.ProcessClassStorage),
					fdbv1beta2.FDBClusterLabel:        clusterName,
					fdbv1beta2.FDBProcessGroupIDLabel: fmt.Sprintf("%s-instance-2", clusterName),
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
