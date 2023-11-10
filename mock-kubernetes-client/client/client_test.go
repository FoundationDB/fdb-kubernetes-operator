/*
 * client.go
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

package client

import (
	"context"
	"fmt"
	"sort"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/pointer"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"
)

func createDummyPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "pod1",
			Labels: map[string]string{
				"app": "app1",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: "test-container"},
			},
		},
	}
}

var _ = Describe("[mock client]", func() {
	var mockClient *MockClient

	BeforeEach(func() {
		Expect(scheme.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
		Expect(fdbv1beta2.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
		mockClient = NewMockClient(scheme.Scheme)
	})

	When("creating and getting an object", func() {
		It("should create and get the object", func() {
			pod := createDummyPod()
			err := mockClient.Create(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.ObjectMeta.Generation).To(Equal(int64(1)))

			podCopy := &corev1.Pod{}
			err = mockClient.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod1"}, podCopy)
			Expect(err).NotTo(HaveOccurred())
			Expect(podCopy.Name).To(Equal("pod1"))
			Expect(len(podCopy.Spec.Containers)).To(Equal(1))
			Expect(podCopy.Spec.Containers[0].Name).To(Equal("test-container"))
			Expect(podCopy.ObjectMeta.Generation).To(Equal(int64(1)))
		})
	})

	When("creating and getting an object with a custom creationTimestamp", func() {
		It("should create and get the object", func() {
			pod := createDummyPod()
			expectedCreationTime := time.Now().Add(-15 * time.Minute)
			pod.CreationTimestamp.Time = expectedCreationTime
			Expect(mockClient.Create(context.TODO(), pod)).NotTo(HaveOccurred())
			Expect(pod.ObjectMeta.Generation).To(Equal(int64(1)))

			podCopy := &corev1.Pod{}
			Expect(mockClient.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod1"}, podCopy)).NotTo(HaveOccurred())
			Expect(podCopy.Name).To(Equal("pod1"))
			Expect(len(podCopy.Spec.Containers)).To(Equal(1))
			Expect(podCopy.Spec.Containers[0].Name).To(Equal("test-container"))
			Expect(podCopy.ObjectMeta.Generation).To(Equal(int64(1)))
			Expect(podCopy.ObjectMeta.CreationTimestamp.Time.Minute()).To(Equal(expectedCreationTime.Minute()))
		})
	})

	When("creating and getting a Pod", func() {
		var initialPod *corev1.Pod

		BeforeEach(func() {
			initialPod = createDummyPod()
			err := mockClient.Create(context.TODO(), initialPod)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create a mock IP", func() {
			pod := &corev1.Pod{}
			err := mockClient.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod1"}, pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Name).To(Equal("pod1"))
			Expect(len(pod.Spec.Containers)).To(Equal(1))
			Expect(pod.Spec.Containers[0].Name).To(Equal("test-container"))
			Expect(pod.ObjectMeta.Generation).To(Equal(int64(1)))
			Expect(pod.Status.PodIP).To(Equal("1.1.0.1"))
		})

		When("Removing the Pod IP", func() {
			BeforeEach(func() {
				err := mockClient.RemovePodIP(initialPod)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should return an empty Pod IP", func() {
				pod := &corev1.Pod{}
				err := mockClient.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod1"}, pod)
				Expect(err).NotTo(HaveOccurred())
				Expect(pod.Name).To(Equal("pod1"))
				Expect(len(pod.Spec.Containers)).To(Equal(1))
				Expect(pod.Spec.Containers[0].Name).To(Equal("test-container"))
				Expect(pod.ObjectMeta.Generation).To(Equal(int64(1)))
				Expect(pod.Status.PodIP).To(Equal(""))
			})
		})
	})

	When("creating and getting twice object", func() {
		It("should return an error", func() {
			err := mockClient.Create(context.TODO(), createDummyPod())
			Expect(err).NotTo(HaveOccurred())

			err = mockClient.Create(context.TODO(), createDummyPod())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("pods \"pod1\" already exists"))
		})
	})

	When("creating a service", func() {
		It("should create the service", func() {
			service := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "service1",
				},
			}
			err := mockClient.Create(context.TODO(), service)
			Expect(err).NotTo(HaveOccurred())
			Expect(service.ObjectMeta.Generation).To(Equal(int64(1)))
			Expect(service.Spec.ClusterIP).To(Equal("192.168.0.1"))

			service = &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "service2",
				},
				Spec: corev1.ServiceSpec{
					ClusterIP: "None",
				},
			}
			err = mockClient.Create(context.TODO(), service)
			Expect(err).NotTo(HaveOccurred())
			Expect(service.ObjectMeta.Generation).To(Equal(int64(1)))
			Expect(service.Spec.ClusterIP).To(Equal("None"))
		})
	})

	When("getting a missing object", func() {
		It("return an is not found error", func() {
			err := mockClient.Create(context.TODO(), createDummyPod())
			Expect(err).NotTo(HaveOccurred())

			pod := &corev1.Pod{}
			err = mockClient.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod2"}, pod)
			Expect(err).To(HaveOccurred())
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())

			deployment := &appsv1.Deployment{}
			err = mockClient.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod1"}, deployment)
			Expect(err).To(HaveOccurred())
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		})
	})

	When("deleting an object", func() {
		It("it should be able to delete the object or return an error", func() {
			pod := createDummyPod()
			err := mockClient.Create(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred())

			objectKey := types.NamespacedName{Namespace: "default", Name: "pod1"}
			err = mockClient.Delete(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred())

			podCopy := &corev1.Pod{}
			err = mockClient.Get(context.TODO(), objectKey, podCopy)
			Expect(err).To(HaveOccurred())
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())

			err = mockClient.Delete(context.TODO(), pod)
			Expect(err).To(HaveOccurred())
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		})
	})

	When("deleting a Pod in terminating state", func() {
		It("the stuck Pod will stay in the registry", func() {
			pod1 := createDummyPod()
			pod2 := createDummyPod()
			pod2.Name = "pod2"

			err := mockClient.Create(context.TODO(), pod1)
			Expect(err).NotTo(HaveOccurred())
			err = mockClient.Create(context.TODO(), pod2)
			Expect(err).NotTo(HaveOccurred())

			err = mockClient.MockStuckTermination(pod1, true)
			Expect(err).NotTo(HaveOccurred())

			err = mockClient.Delete(context.TODO(), pod1)
			Expect(err).NotTo(HaveOccurred())

			podCopy := &corev1.Pod{}
			err = mockClient.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod1"}, podCopy)
			Expect(err).NotTo(HaveOccurred())

			err = mockClient.Delete(context.TODO(), pod2)
			Expect(err).NotTo(HaveOccurred())

			err = mockClient.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod2"}, podCopy)
			Expect(err).To(HaveOccurred())
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		})
	})

	When("updating an object", func() {
		It("the object should be updated", func() {
			pod := createDummyPod()
			err := mockClient.Create(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred())

			container := &pod.Spec.Containers[0]
			container.Env = append(container.Env, corev1.EnvVar{Name: "test-env"})

			err = mockClient.Update(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.ObjectMeta.Generation).To(Equal(int64(2)))

			podCopy := &corev1.Pod{}
			err = mockClient.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod1"}, podCopy)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(podCopy.Spec.Containers)).To(Equal(1))
			Expect(len(podCopy.Spec.Containers[0].Env)).To(Equal(1))
			Expect(podCopy.Spec.Containers[0].Env[0].Name).To(Equal("test-env"))
			Expect(podCopy.ObjectMeta.Generation).To(Equal(int64(2)))

			err = mockClient.Update(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.ObjectMeta.Generation).To(Equal(int64(2)))
		})
	})

	When("updating the object status", func() {
		It("should update the object status", func() {
			pod := createDummyPod()
			err := mockClient.Create(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred())

			pod.Status.HostIP = "foo"

			err = mockClient.Status().Update(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.ObjectMeta.Generation).To(Equal(int64(1)))

			podCopy := &corev1.Pod{}
			err = mockClient.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod1"}, podCopy)
			Expect(err).NotTo(HaveOccurred())
			Expect(podCopy.Status.HostIP).To(Equal("foo"))
			Expect(podCopy.ObjectMeta.Generation).To(Equal(int64(1)))
		})
	})

	When("listing objects", func() {
		It("should list the objects", func() {
			pod1 := createDummyPod()
			pod2 := createDummyPod()
			pod2.Name = "pod2"

			err := mockClient.Create(context.TODO(), pod1)
			Expect(err).NotTo(HaveOccurred())
			err = mockClient.Create(context.TODO(), pod2)
			Expect(err).NotTo(HaveOccurred())

			pods := &corev1.PodList{}
			err = mockClient.List(context.TODO(), pods)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pods.Items)).To(Equal(2))

			sortPodsByName(pods)
			Expect(pods.Items[0].Name).To(Equal("pod1"))
			Expect(pods.Items[1].Name).To(Equal("pod2"))
		})
	})

	When("listing objects by namespace", func() {
		It("should list the objects in the namespace", func() {
			pod1 := createDummyPod()
			pod2 := createDummyPod()
			pod2.Name = "pod2"

			err := mockClient.Create(context.TODO(), pod1)
			Expect(err).NotTo(HaveOccurred())
			err = mockClient.Create(context.TODO(), pod2)
			Expect(err).NotTo(HaveOccurred())

			pods := &corev1.PodList{}
			err = mockClient.List(context.TODO(), pods, ctrlClient.InNamespace("default"))
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pods.Items)).To(Equal(2))

			err = mockClient.List(context.TODO(), pods, ctrlClient.InNamespace("not-default"))
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pods.Items)).To(Equal(0))
		})
	})

	When("listing objects by label", func() {
		It("should list the objects with the label", func() {
			pod1 := createDummyPod()
			pod2 := createDummyPod()
			pod2.Name = "pod2"
			pod2.ObjectMeta.Labels["app"] = "app2"
			pod3 := createDummyPod()
			pod3.Name = "pod3"
			pod3.Labels = nil

			err := mockClient.Create(context.TODO(), pod1)
			Expect(err).NotTo(HaveOccurred())
			err = mockClient.Create(context.TODO(), pod2)
			Expect(err).NotTo(HaveOccurred())
			err = mockClient.Create(context.TODO(), pod3)
			Expect(err).NotTo(HaveOccurred())

			pods := &corev1.PodList{}
			err = mockClient.List(context.TODO(), pods, ctrlClient.MatchingLabels(map[string]string{"app": "app2"}))
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pods.Items)).To(Equal(1))
			Expect(pods.Items[0].Name).To(Equal("pod2"))

			appRequirement, err := labels.NewRequirement("app", selection.Exists, nil)
			Expect(err).NotTo(HaveOccurred())
			pods = &corev1.PodList{}
			err = mockClient.List(context.TODO(), pods, ctrlClient.MatchingLabelsSelector{Selector: labels.NewSelector().Add(*appRequirement)})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pods.Items)).To(Equal(2))
			sortPodsByName(pods)
			Expect(pods.Items[0].Name).To(Equal("pod1"))
			Expect(pods.Items[1].Name).To(Equal("pod2"))
		})
	})

	// TODO (johscheuer): Once https://github.com/kubernetes-sigs/controller-runtime/pull/2025 is merged we can enable
	// this test again.
	//
	//When("listing objects by field", func() {
	//	It("should list the objects with the field set", func() {
	//		pod1 := createDummyPod()
	//		pod2 := createDummyPod()
	//		pod2.Name = "pod2"
	//
	//		err := mockClient.Create(context.TODO(), pod1)
	//		Expect(err).NotTo(HaveOccurred())
	//		err = mockClient.Create(context.TODO(), pod2)
	//		Expect(err).NotTo(HaveOccurred())
	//
	//		pods := &corev1.PodList{}
	//		err = mockClient.List(context.TODO(), pods, ctrlClient.MatchingFields{"metadata.name": "pod1"})
	//		Expect(err).NotTo(HaveOccurred())
	//		Expect(len(pods.Items)).To(Equal(1))
	//		Expect(pods.Items[0].Name).To(Equal("pod1"))
	//	})
	//})

	When("creating an event", func() {
		It("should create the event", func() {
			pod := createDummyPod()
			pod.ObjectMeta.UID = uuid.NewUUID()
			mockClient.Event(pod, "Testing", "This is a test", "Test message")
			events := &corev1.EventList{}
			err := mockClient.List(context.TODO(), events)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(events.Items)).To(Equal(1))
			Expect(events.Items[0].Namespace).To(Equal("default"))
			Expect(events.Items[0].Name).To(HavePrefix("Testing-"))
			Expect(events.Items[0].InvolvedObject.Namespace).To(Equal("default"))
			Expect(events.Items[0].InvolvedObject.Name).To(Equal("pod1"))
			Expect(events.Items[0].InvolvedObject.UID).To(Equal(pod.ObjectMeta.UID))
			Expect(events.Items[0].Type).To(Equal("Testing"))
			Expect(events.Items[0].Reason).To(Equal("This is a test"))
			Expect(events.Items[0].Message).To(Equal("Test message"))
			Expect(events.Items[0].EventTime.UnixNano()).NotTo(Equal(0))
		})
	})

	When("creating an event with a message format", func() {
		It("should create the event", func() {
			mockClient.Eventf(createDummyPod(), "Testing", "This is a test", "Test message: %d", 5)
			events := &corev1.EventList{}
			err := mockClient.List(context.TODO(), events)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(events.Items)).To(Equal(1))
			Expect(events.Items[0].Namespace).To(Equal("default"))
			Expect(events.Items[0].Name).To(HavePrefix("Testing-"))
			Expect(events.Items[0].InvolvedObject.Namespace).To(Equal("default"))
			Expect(events.Items[0].InvolvedObject.Name).To(Equal("pod1"))
			Expect(events.Items[0].Type).To(Equal("Testing"))
			Expect(events.Items[0].Reason).To(Equal("This is a test"))
			Expect(events.Items[0].Message).To(Equal("Test message: 5"))
		})
	})

	When("creating an event with in the past", func() {
		It("should create the event", func() {
			timestamp := metav1.Time{Time: time.Now().Add(-1 * time.Minute)}
			mockClient.PastEventf(createDummyPod(), timestamp, "Testing", "This is a test", "Test message: %d", 5)
			events := &corev1.EventList{}
			err := mockClient.List(context.TODO(), events)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(events.Items)).To(Equal(1))
			Expect(events.Items[0].Namespace).To(Equal("default"))
			Expect(events.Items[0].Name).To(HavePrefix("Testing-"))
			Expect(events.Items[0].InvolvedObject.Namespace).To(Equal("default"))
			Expect(events.Items[0].InvolvedObject.Name).To(Equal("pod1"))
			Expect(events.Items[0].Type).To(Equal("Testing"))
			Expect(events.Items[0].Reason).To(Equal("This is a test"))
			Expect(events.Items[0].Message).To(Equal("Test message: 5"))
			Expect(events.Items[0].EventTime.Unix()).To(Equal(timestamp.Unix()))
		})
	})

	When("creating an event with annotations", func() {
		It("should create the event", func() {
			mockClient.AnnotatedEventf(createDummyPod(), map[string]string{"anno": "value"}, "Testing", "This is a test", "Test message: %d", 5)
			events := &corev1.EventList{}
			err := mockClient.List(context.TODO(), events)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(events.Items)).To(Equal(1))
			Expect(events.Items[0].Namespace).To(Equal("default"))
			Expect(events.Items[0].Name).To(HavePrefix("Testing-"))
			Expect(events.Items[0].InvolvedObject.Namespace).To(Equal("default"))
			Expect(events.Items[0].InvolvedObject.Name).To(Equal("pod1"))
			Expect(events.Items[0].Type).To(Equal("Testing"))
			Expect(events.Items[0].Reason).To(Equal("This is a test"))
			Expect(events.Items[0].Message).To(Equal("Test message: 5"))
			Expect(events.Items[0].ObjectMeta.Annotations).To(Equal(map[string]string{"anno": "value"}))
		})
	})

	When("adding a custom create hook", func() {
		var expectedPods = int32(4)

		BeforeEach(func() {
			mockClient = NewMockClient(scheme.Scheme, func(ctx context.Context, client *MockClient, object ctrlClient.Object) error {
				replicaSet, isReplicaSet := object.(*appsv1.ReplicaSet)
				if !isReplicaSet {
					return nil
				}

				expectedReplicas := pointer.Int32Deref(replicaSet.Spec.Replicas, 1)

				for i := int32(0); i < expectedReplicas; i++ {
					err := client.Create(ctx, &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: replicaSet.Namespace,
							Name:      fmt.Sprintf("%s-%d", replicaSet.Name, i),
						},
					})
					_ = err
				}

				replicaSet.Labels = map[string]string{
					"test": "success",
				}

				return nil
			})

			Expect(mockClient.Create(context.TODO(), &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "unicorn",
				},
				Spec: appsv1.ReplicaSetSpec{
					Replicas: pointer.Int32(expectedPods),
				},
			})).NotTo(HaveOccurred())
		})

		It("should run the webhook and create 4 Pods", func() {
			podList := &corev1.PodList{}
			Expect(mockClient.List(context.TODO(), podList)).NotTo(HaveOccurred())
			Expect(podList.Items).To(HaveLen(int(expectedPods)))

			// Ensure the label is update
			replicaSet := &appsv1.ReplicaSet{}
			Expect(mockClient.Get(context.TODO(), ctrlClient.ObjectKey{Name: "unicorn"}, replicaSet)).NotTo(HaveOccurred())
			Expect(pointer.Int32Deref(replicaSet.Spec.Replicas, -1)).To(BeNumerically("==", expectedPods))
			Expect(replicaSet.Labels).To(HaveKeyWithValue("test", "success"))
		})
	})

	When("adding a custom create and update hook", func() {
		var expectedPods = int32(4)
		var additionalPods = int32(12)

		BeforeEach(func() {
			createHook := func(ctx context.Context, client *MockClient, object ctrlClient.Object) error {
				replicaSet, isReplicaSet := object.(*appsv1.ReplicaSet)
				if !isReplicaSet {
					return nil
				}

				expectedReplicas := pointer.Int32Deref(replicaSet.Spec.Replicas, 1)

				for i := int32(0); i < expectedReplicas; i++ {
					err := client.Create(ctx, &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: replicaSet.Namespace,
							Name:      fmt.Sprintf("%s-%d", replicaSet.Name, i),
						},
					})
					_ = err
				}

				replicaSet.Labels = map[string]string{
					"test": "success",
				}

				return nil
			}

			updateHook := func(ctx context.Context, client *MockClient, object ctrlClient.Object) error {
				replicaSet, isReplicaSet := object.(*appsv1.ReplicaSet)
				if !isReplicaSet {
					return nil
				}

				podList := &corev1.PodList{}
				Expect(client.List(context.TODO(), podList)).NotTo(HaveOccurred())

				expectedReplicas := pointer.Int32Deref(replicaSet.Spec.Replicas, 1)
				for i := int32(len(podList.Items)); i < expectedReplicas; i++ {
					err := client.Create(ctx, &corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: replicaSet.Namespace,
							Name:      fmt.Sprintf("%s-%d", replicaSet.Name, i),
						},
					})
					_ = err
				}

				replicaSet.Labels = map[string]string{
					"test": "success",
				}

				return nil
			}

			mockClient = NewMockClientWithHooks(scheme.Scheme, []func(ctx context.Context, client *MockClient, object ctrlClient.Object) error{
				createHook,
			}, []func(ctx context.Context, client *MockClient, object ctrlClient.Object) error{
				updateHook,
			})

			replicaSet := &appsv1.ReplicaSet{
				ObjectMeta: metav1.ObjectMeta{
					Name: "unicorn",
				},
				Spec: appsv1.ReplicaSetSpec{
					Replicas: pointer.Int32(expectedPods),
				},
			}
			Expect(mockClient.Create(context.TODO(), replicaSet)).NotTo(HaveOccurred())

			replicaSet.Spec.Replicas = pointer.Int32(expectedPods + additionalPods)
			Expect(mockClient.Update(context.TODO(), replicaSet)).NotTo(HaveOccurred())
		})

		It("should run the webhook and create 16 Pods", func() {
			podList := &corev1.PodList{}
			Expect(mockClient.List(context.TODO(), podList)).NotTo(HaveOccurred())
			Expect(podList.Items).To(HaveLen(int(expectedPods + additionalPods)))

			// Ensure the label is update
			replicaSet := &appsv1.ReplicaSet{}
			Expect(mockClient.Get(context.TODO(), ctrlClient.ObjectKey{Name: "unicorn"}, replicaSet)).NotTo(HaveOccurred())
			Expect(pointer.Int32Deref(replicaSet.Spec.Replicas, -1)).To(BeNumerically("==", expectedPods+additionalPods))
			Expect(replicaSet.Labels).To(HaveKeyWithValue("test", "success"))
		})
	})
})

func sortPodsByName(pods *corev1.PodList) {
	sort.Slice(pods.Items, func(i, j int) bool {
		return pods.Items[i].ObjectMeta.Name < pods.Items[j].ObjectMeta.Name
	})
}
