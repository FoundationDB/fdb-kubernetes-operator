/*
 * delete_pods_for_buggification_test.go
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

package controllers

import (
	"context"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("delete_pods_for_buggification", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var err error
	var requeue *requeue
	var originalPods *corev1.PodList

	BeforeEach(func() {
		cluster = internal.CreateDefaultCluster()
		err = k8sClient.Create(context.TODO(), cluster)
		Expect(err).NotTo(HaveOccurred())

		err = internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})
		Expect(err).NotTo(HaveOccurred())

		result, err := reconcileCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Requeue).To(BeFalse())

		generation, err := reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
		Expect(generation).To(Equal(int64(1)))

		originalPods = &corev1.PodList{}
		err = k8sClient.List(context.TODO(), originalPods, getListOptions(cluster)...)
		Expect(err).NotTo(HaveOccurred())
	})

	JustBeforeEach(func() {
		err = internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})
		Expect(err).NotTo(HaveOccurred())

		requeue = deletePodsForBuggification{}.reconcile(context.TODO(), clusterReconciler, cluster, nil, globalControllerLogger)
		if requeue != nil {
			Expect(requeue.curError).NotTo(HaveOccurred())
		}
		_, err = reloadCluster(cluster)
		Expect(err).NotTo(HaveOccurred())
	})

	Context("with a reconciled cluster", func() {
		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should not delete any pods", func() {
			pods := &corev1.PodList{}
			err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pods.Items)).To(Equal(len(originalPods.Items)))

			pod := &corev1.Pod{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-storage-1"}, pod)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("with a buggification that needs to be enabled", func() {
		When("using crashLoop", func() {
			BeforeEach(func() {
				cluster.Spec.Buggify.CrashLoop = []fdbv1beta2.ProcessGroupID{"storage-1"}
			})

			It("should requeue", func() {
				Expect(requeue).NotTo(BeNil())
				Expect(requeue.message).To(Equal("Pods need to be recreated"))
			})

			It("should delete the pod", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(len(originalPods.Items) - 1))

				pod := &corev1.Pod{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-storage-1"}, pod)
				Expect(err).To(HaveOccurred())
				Expect(k8serrors.IsNotFound(err)).To(BeTrue())
			})
		})

		When("using noSchedule", func() {
			BeforeEach(func() {
				cluster.Spec.Buggify.NoSchedule = []fdbv1beta2.ProcessGroupID{"storage-1"}
			})

			It("should requeue", func() {
				Expect(requeue).NotTo(BeNil())
				Expect(requeue.message).To(Equal("Pods need to be recreated"))
			})

			It("should delete the pod", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(len(originalPods.Items) - 1))

				pod := &corev1.Pod{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-storage-1"}, pod)
				Expect(err).To(HaveOccurred())
				Expect(k8serrors.IsNotFound(err)).To(BeTrue())
			})
		})
	})

	Context("with a wildcard buggification", func() {
		BeforeEach(func() {
			cluster.Spec.Buggify.CrashLoop = []fdbv1beta2.ProcessGroupID{"*"}
		})

		It("should requeue", func() {
			Expect(requeue).NotTo(BeNil())
			Expect(requeue.message).To(Equal("Pods need to be recreated"))
		})

		It("should delete the pods", func() {
			pods := &corev1.PodList{}
			err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pods.Items)).To(Equal(0))

			pod := &corev1.Pod{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-storage-1"}, pod)
			Expect(err).To(HaveOccurred())
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		})
	})

	Context("with a buggification that needs to be disabled", func() {
		When("using crashLoop", func() {
			BeforeEach(func() {
				pod := &corev1.Pod{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-storage-1"}, pod)
				Expect(err).NotTo(HaveOccurred())

				err = k8sClient.Delete(context.TODO(), pod)
				Expect(err).NotTo(HaveOccurred())

				pod.ResourceVersion = ""
				pod.Spec.Containers[0].Args = []string{"crash-loop"}
				err = k8sClient.Create(context.TODO(), pod)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should requeue", func() {
				Expect(requeue).NotTo(BeNil())
				Expect(requeue.message).To(Equal("Pods need to be recreated"))
			})

			It("should delete the pod", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(len(originalPods.Items) - 1))

				pod := &corev1.Pod{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-storage-1"}, pod)
				Expect(err).To(HaveOccurred())
				Expect(k8serrors.IsNotFound(err)).To(BeTrue())
			})
		})

		When("using noSchedule", func() {
			BeforeEach(func() {
				pod := &corev1.Pod{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-storage-1"}, pod)
				Expect(err).NotTo(HaveOccurred())

				err = k8sClient.Delete(context.TODO(), pod)
				Expect(err).NotTo(HaveOccurred())

				pod.Spec.Affinity = &corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key: fdbv1beta2.NodeSelectorNoScheduleLabel,
										},
									},
								},
							},
						},
					},
				}

				pod.ResourceVersion = ""
				err = k8sClient.Create(context.TODO(), pod)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should requeue", func() {
				Expect(requeue).NotTo(BeNil())
				Expect(requeue.message).To(Equal("Pods need to be recreated"))
			})

			It("should delete the pod", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(len(originalPods.Items) - 1))

				pod := &corev1.Pod{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-storage-1"}, pod)
				Expect(err).To(HaveOccurred())
				Expect(k8serrors.IsNotFound(err)).To(BeTrue())
			})
		})
	})

	Context("with a buggification that is already on", func() {
		When("using crashLoop", func() {
			BeforeEach(func() {
				pod := &corev1.Pod{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-storage-1"}, pod)
				Expect(err).NotTo(HaveOccurred())

				err = k8sClient.Delete(context.TODO(), pod)
				Expect(err).NotTo(HaveOccurred())

				pod.ResourceVersion = ""
				pod.Spec.Containers[0].Args = []string{"crash-loop"}
				err = k8sClient.Create(context.TODO(), pod)
				Expect(err).NotTo(HaveOccurred())
				cluster.Spec.Buggify.CrashLoop = []fdbv1beta2.ProcessGroupID{"storage-1"}
			})

			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should not delete any pods", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(len(originalPods.Items)))

				pod := &corev1.Pod{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-storage-1"}, pod)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		When("using noSchedule", func() {
			BeforeEach(func() {
				pod := &corev1.Pod{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-storage-1"}, pod)
				Expect(err).NotTo(HaveOccurred())

				err = k8sClient.Delete(context.TODO(), pod)
				Expect(err).NotTo(HaveOccurred())

				pod.Spec.Affinity = &corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key: fdbv1beta2.NodeSelectorNoScheduleLabel,
										},
									},
								},
							},
						},
					},
				}

				pod.ResourceVersion = ""
				err = k8sClient.Create(context.TODO(), pod)
				Expect(err).NotTo(HaveOccurred())
				cluster.Spec.Buggify.NoSchedule = []fdbv1beta2.ProcessGroupID{"storage-1"}
			})

			It("should not requeue", func() {
				Expect(requeue).To(BeNil())
			})

			It("should not delete any pods", func() {
				pods := &corev1.PodList{}
				err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(pods.Items)).To(Equal(len(originalPods.Items)))

				pod := &corev1.Pod{}
				err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-storage-1"}, pod)
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})

	Context("with a change to an environment variable", func() {
		BeforeEach(func() {
			cluster.Spec.Processes = map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings{fdbv1beta2.ProcessClassGeneral: {PodTemplate: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: fdbv1beta2.MainContainerName,
							Env: []corev1.EnvVar{
								{
									Name:  "TEST_CHANGE",
									Value: "1",
								},
							},
						},
					},
				},
			}}}
			err = internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should not requeue", func() {
			Expect(requeue).To(BeNil())
		})

		It("should not delete any pods", func() {
			pods := &corev1.PodList{}
			err = k8sClient.List(context.TODO(), pods, getListOptions(cluster)...)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pods.Items)).To(Equal(len(originalPods.Items)))

			pod := &corev1.Pod{}
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "operator-test-1-storage-1"}, pod)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
