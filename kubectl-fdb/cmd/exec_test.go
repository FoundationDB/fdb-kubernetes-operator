/*
 * exec_tests.go
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
	"reflect"
	"testing"

	"github.com/FoundationDB/fdb-kubernetes-operator/controllers"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBuildCommand(t *testing.T) {
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
		Name          string
		ClusterName   string
		Context       string
		Command       []string
		ExpectedArgs  []string
		ExpectedError string
	}{
		{
			Name:         "Exec into instance with valid pod",
			ClusterName:  "test",
			ExpectedArgs: []string{"--namespace", "test", "exec", "-it", "instance-1", "--", "bash"},
		},
		{
			Name:         "Exec into instance with explicit context",
			ClusterName:  "test",
			Context:      "remote-kc",
			ExpectedArgs: []string{"--context", "remote-kc", "--namespace", "test", "exec", "-it", "instance-1", "--", "bash"},
		},
		{
			Name:          "Exec into instance with missing pod",
			ClusterName:   "test-2",
			ExpectedError: "No usable pods found for cluster test-2",
		},
	}

	for _, tc := range tt {
		t.Run(tc.Name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = clientgoscheme.AddToScheme(scheme)
			_ = fdbtypes.AddToScheme(scheme)
			kubeClient := fake.NewFakeClientWithScheme(scheme, &cluster, &podList)

			command, err := buildCommand(kubeClient, tc.ClusterName, tc.Context, namespace, tc.Command)

			if tc.ExpectedError != "" {
				if err == nil {
					t.Logf("buildCommand %s: Expected error %s, but got none", tc.Name, tc.ExpectedError)
					t.FailNow()
				}
				if err.Error() != tc.ExpectedError {
					t.Logf("buildCommand %s: Expected error %s, but got %s", tc.Name, tc.ExpectedError, err.Error())
					t.FailNow()
				}
			} else {
				if err != nil {
					t.Log(err)
					t.FailNow()
				}
				if command.Path == "" {
					t.Logf("buildCommand %s: Path is empty", tc.Name)
					t.FailNow()
				}
				expectedArgs := []string{command.Path}
				expectedArgs = append(expectedArgs, tc.ExpectedArgs...)
				if !reflect.DeepEqual(command.Args, expectedArgs) {
					t.Logf("buildCommand %s: Expected args %v, but got %v", tc.Name, expectedArgs, command.Args)
					t.FailNow()
				}
			}
		})
	}
}
