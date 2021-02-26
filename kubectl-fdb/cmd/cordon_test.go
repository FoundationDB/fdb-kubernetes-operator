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
	ctx "context"
	"testing"

	"k8s.io/apimachinery/pkg/api/equality"

	"github.com/FoundationDB/fdb-kubernetes-operator/controllers"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestCordonNode(t *testing.T) {
	clusterName := "test"
	namespace := "test"

	cluster := fdbtypes.FoundationDBCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: fdbtypes.FoundationDBClusterSpec{
			ProcessCounts: fdbtypes.ProcessCounts{
				Storage: 1,
			},
		},
	}

	nodeList := corev1.NodeList{
		Items: []corev1.Node{},
	}

	podList := corev1.PodList{
		Items: []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "instance-1",
					Namespace: namespace,
					Labels: map[string]string{
						controllers.FDBProcessClassLabel: string(fdbtypes.ProcessClassStorage),
						controllers.FDBClusterLabel:      clusterName,
						controllers.FDBInstanceIDLabel:   "instance-1",
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
						controllers.FDBProcessClassLabel: string(fdbtypes.ProcessClassStorage),
						controllers.FDBClusterLabel:      clusterName,
						controllers.FDBInstanceIDLabel:   "instance-2",
					},
				},
				Spec: corev1.PodSpec{
					NodeName: "node-2",
				},
			},
		},
	}

	tt := []struct {
		Name                                      string
		nodes                                     []string
		WithExclusion                             bool
		ExpectedInstancesToRemove                 []string
		ExpectedInstancesToRemoveWithoutExclusion []string
	}{
		// todo add test cases for node selector
		{
			Name:                      "Cordon node with exclusion",
			nodes:                     []string{"node-1"},
			WithExclusion:             true,
			ExpectedInstancesToRemove: []string{"instance-1"},
			ExpectedInstancesToRemoveWithoutExclusion: []string{},
		},
		{
			Name:                      "Cordon node without exclusion",
			nodes:                     []string{"node-1"},
			WithExclusion:             false,
			ExpectedInstancesToRemove: []string{},
			ExpectedInstancesToRemoveWithoutExclusion: []string{"instance-1"},
		},
		{
			Name:                      "Cordon no nodes with exclusion",
			nodes:                     []string{""},
			WithExclusion:             true,
			ExpectedInstancesToRemove: []string{},
			ExpectedInstancesToRemoveWithoutExclusion: []string{},
		},
		{
			Name:                      "Cordon no node nodes without exclusion",
			nodes:                     []string{""},
			WithExclusion:             false,
			ExpectedInstancesToRemove: []string{},
			ExpectedInstancesToRemoveWithoutExclusion: []string{},
		},
		{
			Name:                      "Cordon all nodes with exclusion",
			nodes:                     []string{"node-1", "node-2"},
			WithExclusion:             true,
			ExpectedInstancesToRemove: []string{"instance-1", "instance-2"},
			ExpectedInstancesToRemoveWithoutExclusion: []string{},
		},
		{
			Name:                      "Cordon all nodes without exclusion",
			nodes:                     []string{"node-1", "node-2"},
			WithExclusion:             false,
			ExpectedInstancesToRemove: []string{},
			ExpectedInstancesToRemoveWithoutExclusion: []string{"instance-1", "instance-2"},
		},
	}

	for _, tc := range tt {
		t.Run(tc.Name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = clientgoscheme.AddToScheme(scheme)
			_ = fdbtypes.AddToScheme(scheme)
			kubeClient := fake.NewFakeClientWithScheme(scheme, &cluster, &podList, &nodeList)

			err := cordonNode(kubeClient, clusterName, tc.nodes, namespace, tc.WithExclusion, true)
			if err != nil {
				t.Error(err)
				return
			}

			var resCluster fdbtypes.FoundationDBCluster
			err = kubeClient.Get(ctx.Background(), client.ObjectKey{
				Namespace: namespace,
				Name:      clusterName,
			}, &resCluster)

			if err != nil {
				t.Error(err)
			}

			if !equality.Semantic.DeepEqual(tc.ExpectedInstancesToRemove, resCluster.Spec.InstancesToRemove) {
				t.Errorf("InstancesToRemove expected: %s - got: %s\n", tc.ExpectedInstancesToRemove, resCluster.Spec.InstancesToRemove)
			}

			if !equality.Semantic.DeepEqual(tc.ExpectedInstancesToRemoveWithoutExclusion, resCluster.Spec.InstancesToRemoveWithoutExclusion) {
				t.Errorf("InstancesToRemoveWithoutExclusion expected: %s - got: %s\n", tc.ExpectedInstancesToRemoveWithoutExclusion, resCluster.Spec.InstancesToRemoveWithoutExclusion)
			}
		})
	}
}
