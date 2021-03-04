/*
 * remove_instances_test.go
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

func TestRemoveInstances(t *testing.T) {
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

	podList := corev1.PodList{
		Items: []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "instance-1",
					Namespace: namespace,
					Labels: map[string]string{
						controllers.FDBProcessClassLabel: string(fdbtypes.ProcessClassStorage),
						controllers.FDBClusterLabel:      clusterName,
					},
				},
			},
		},
	}

	tt := []struct {
		Name                                      string
		Instances                                 []string
		WithExclusion                             bool
		WithShrink                                bool
		ExpectedInstancesToRemove                 []string
		ExpectedInstancesToRemoveWithoutExclusion []string
		ExpectedProcessCounts                     fdbtypes.ProcessCounts
	}{
		{
			Name:                      "Remove instance with exclusion",
			Instances:                 []string{"instance-1"},
			WithExclusion:             true,
			WithShrink:                false,
			ExpectedInstancesToRemove: []string{"instance-1"},
			ExpectedInstancesToRemoveWithoutExclusion: []string{},
			ExpectedProcessCounts: fdbtypes.ProcessCounts{
				Storage: 1,
			},
		},
		{
			Name:                      "Remove instance without exclusion",
			Instances:                 []string{"instance-1"},
			WithExclusion:             false,
			WithShrink:                false,
			ExpectedInstancesToRemove: []string{},
			ExpectedInstancesToRemoveWithoutExclusion: []string{"instance-1"},
			ExpectedProcessCounts: fdbtypes.ProcessCounts{
				Storage: 1,
			},
		},
		{
			Name:                      "Remove instance with exclusion and shrink",
			Instances:                 []string{"instance-1"},
			WithExclusion:             true,
			WithShrink:                true,
			ExpectedInstancesToRemove: []string{"instance-1"},
			ExpectedInstancesToRemoveWithoutExclusion: []string{},
			ExpectedProcessCounts: fdbtypes.ProcessCounts{
				Storage: 0,
			},
		},
		{
			Name:                      "Remove instance without exclusion and shrink",
			Instances:                 []string{"instance-1"},
			WithExclusion:             false,
			WithShrink:                true,
			ExpectedInstancesToRemove: []string{},
			ExpectedInstancesToRemoveWithoutExclusion: []string{"instance-1"},
			ExpectedProcessCounts: fdbtypes.ProcessCounts{
				Storage: 0,
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.Name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = clientgoscheme.AddToScheme(scheme)
			_ = fdbtypes.AddToScheme(scheme)
			kubeClient := fake.NewFakeClientWithScheme(scheme, &cluster, &podList)

			err := removeInstances(kubeClient, clusterName, tc.Instances, namespace, tc.WithExclusion, tc.WithShrink, true)
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

			if tc.ExpectedProcessCounts.Storage != resCluster.Spec.ProcessCounts.Storage {
				t.Errorf("ProcessCounts expected: %d - got: %d\n", tc.ExpectedProcessCounts.Storage, cluster.Spec.ProcessCounts.Storage)
			}
		})
	}
}

func TestGetInstanceIDsFromPod(t *testing.T) {
	clusterName := "test"
	namespace := "test"

	podList := corev1.PodList{
		Items: []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "instance-1",
					Namespace: namespace,
					Labels: map[string]string{
						controllers.FDBProcessClassLabel: string(fdbtypes.ProcessClassStorage),
						controllers.FDBClusterLabel:      clusterName,
						controllers.FDBInstanceIDLabel:   "storage-1",
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "instance-2",
					Namespace: namespace,
					Labels: map[string]string{
						controllers.FDBProcessClassLabel: string(fdbtypes.ProcessClassStorage),
						controllers.FDBClusterLabel:      clusterName,
						controllers.FDBInstanceIDLabel:   "storage-2",
					},
				},
			},
		},
	}

	tt := []struct {
		Name              string
		Instances         []string
		ExpectedInstances []string
	}{
		{
			Name:              "Filter one instance",
			Instances:         []string{"instance-1"},
			ExpectedInstances: []string{"storage-1"},
		},
		{
			Name:              "Filter two instances",
			Instances:         []string{"instance-1", "instance-2"},
			ExpectedInstances: []string{"storage-1", "storage-2"},
		},
		{
			Name:              "Filter no instance",
			Instances:         []string{""},
			ExpectedInstances: []string{},
		},
	}

	for _, tc := range tt {
		t.Run(tc.Name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = clientgoscheme.AddToScheme(scheme)
			_ = fdbtypes.AddToScheme(scheme)
			kubeClient := fake.NewFakeClientWithScheme(scheme, &podList)

			instances, err := getInstanceIDsFromPod(kubeClient, clusterName, tc.Instances, namespace)
			if err != nil {
				t.Error(err)
				return
			}

			if !equality.Semantic.DeepEqual(tc.ExpectedInstances, instances) {
				t.Errorf("expected: %s - got: %s\n", tc.ExpectedInstances, instances)
			}
		})
	}
}
