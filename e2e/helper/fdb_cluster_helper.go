/*
 * fdb_cluster_helper.go
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

package helper

import (
	"context"
	"testing"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
)

type contextKey int

const (
	keyNamespaceID contextKey = iota
	keyClusterNameID
)

// GetTestFDBVersions returns a slice that contains all FDB versions we use to do e2e tests.
func GetTestFDBVersions() []fdbv1beta2.Version {
	return []fdbv1beta2.Version{
		{Major: 6, Minor: 2, Patch: 20},
		{Major: 6, Minor: 3, Patch: 23},
		{Major: 7, Minor: 1, Patch: 23},
	}
}

// CreateSingleClusterTest returns an e2e test that creates a cluster and assess that the cluster was correctly created.
func CreateSingleClusterTest(version fdbv1beta2.Version, t *testing.T) features.Feature {
	return features.
		New("create single cluster for "+version.Compact()).
		WithLabel("type", "create-cluster").
		Setup(func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			clusterName := envconf.RandomName("fdb-create", 32)
			namespace := ctx.Value(keyNamespaceID).(string)

			testCluster := createFoundationDBCluster(clusterName, namespace, version.String())

			err := cfg.Client().Resources(namespace).Create(ctx, testCluster)
			if err != nil {
				t.Error(err)
			}

			return context.WithValue(ctx, keyClusterNameID, clusterName)
		}).
		Assess("it should create an FDB cluster in "+version.Compact(), func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			namespace := ctx.Value(keyNamespaceID).(string)
			clusterName := ctx.Value(keyClusterNameID).(string)

			testCluster := &fdbv1beta2.FoundationDBCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
			}

			err := wait.For(conditions.New(cfg.Client().Resources(namespace)).ResourceMatch(testCluster, func(object k8s.Object) bool {
				cluster := object.(*fdbv1beta2.FoundationDBCluster)
				return cluster.Status.Generations.Reconciled >= 1
			}), wait.WithTimeout(time.Minute*3))

			if err != nil {
				t.Error(err)
			}

			return ctx
		}).
		Assess("it should create all desired Pods", func(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
			namespace := ctx.Value(keyNamespaceID).(string)
			clusterName := ctx.Value(keyClusterNameID).(string)

			testCluster := &fdbv1beta2.FoundationDBCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
			}

			err := cfg.Client().Resources(namespace).Get(ctx, testCluster.Name, testCluster.Namespace, testCluster)
			if err != nil {
				t.Error(err)
				t.Fail()
			}

			podList := &corev1.PodList{}

			err = cfg.Client().Resources(namespace).List(ctx, podList, resources.WithLabelSelector(labels.SelectorFromSet(testCluster.GetMatchLabels()).String()))
			if err != nil {
				t.Error(err)
				t.Fail()
			}

			processCounts, err := testCluster.GetProcessCountsWithDefaults()
			if err != nil {
				t.Error(err)
			}

			var storageCount, statelessCount, logCount int
			for _, pod := range podList.Items {
				pClass, ok := pod.GetLabels()[testCluster.GetProcessClassLabel()]
				if !ok {
					continue
				}

				if pClass == string(fdbv1beta2.ProcessClassStorage) {
					storageCount++
					continue
				} else if pClass == string(fdbv1beta2.ProcessClassLog) {
					logCount++
					continue
				} else if pClass == string(fdbv1beta2.ProcessClassStateless) {
					statelessCount++
					continue
				}
			}

			if storageCount != processCounts.Storage || statelessCount != processCounts.Stateless || logCount != processCounts.Log {
				t.Errorf(`Expected to have matching process counts
Expected Storage: %d, got: %d
Expected Stateless: %d, got: %d
Expected Log: %d, got: %d
`,
					processCounts.Storage, storageCount,
					processCounts.Stateless, statelessCount,
					processCounts.Log, logCount,
				)
			}

			return ctx
		}).
		Teardown(func(ctx context.Context, t *testing.T, config *envconf.Config) context.Context {
			namespace := ctx.Value(keyNamespaceID).(string)
			clusterName := ctx.Value(keyClusterNameID).(string)

			testCluster := &fdbv1beta2.FoundationDBCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
			}

			err := config.Client().Resources(namespace).Delete(ctx, testCluster)
			if err != nil {
				t.Error(err)
			}

			return ctx
		}).Feature()
}

func createFoundationDBCluster(clusterName string, namespace string, version string) *fdbv1beta2.FoundationDBCluster {
	return &fdbv1beta2.FoundationDBCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: fdbv1beta2.FoundationDBClusterSpec{
			FaultDomain: fdbv1beta2.FoundationDBClusterFaultDomain{
				Key: fdbv1beta2.NoneFaultDomainKey,
			},
			MinimumUptimeSecondsForBounce: 60,
			Version:                       version,
			Processes: map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{
				fdbv1beta2.ProcessClassGeneral: {
					CustomParameters: fdbv1beta2.FoundationDBCustomParameters{
						"knob_disable_posix_kernel_aio=1",
					},
					PodTemplate: &corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: fdbv1beta2.MainContainerName,
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											"foundationdb.org/empty": resource.MustParse("0"),
										},
									},
								},
								{
									Name: fdbv1beta2.SidecarContainerName,
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											"foundationdb.org/empty": resource.MustParse("0"),
										},
									},
								},
							},
							InitContainers: []corev1.Container{
								{
									Name: fdbv1beta2.InitContainerName,
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											"foundationdb.org/empty": resource.MustParse("0"),
										},
									},
								},
							},
						},
					},
					VolumeClaimTemplate: &corev1.PersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceStorage: resource.MustParse("2G"),
								},
							},
						},
					},
				},
			},
		},
	}
}

// RegisterFDBScheme registers the fdbv1beta2 scheme to the rest config
func RegisterFDBScheme(ctx context.Context, cfg *envconf.Config) (context.Context, error) {
	r, err := resources.New(cfg.Client().RESTConfig())
	if err != nil {
		return ctx, err
	}

	err = fdbv1beta2.AddToScheme(r.GetScheme())

	return ctx, err
}

// WaitUntilCRDAvailable waits until the CRD is available
func WaitUntilCRDAvailable(ctx context.Context, _ *envconf.Config) (context.Context, error) {
	// TODO (johscheuer): Implement this logic instead of sleeping 1 second.
	time.Sleep(1 * time.Second)

	return ctx, nil
}
