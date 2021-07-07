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
	"sort"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
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
	var client *MockClient
	BeforeEach(func() {
		client = &MockClient{}
	})

	When("creating and getting an object", func() {
		It("should create and get the object", func() {
			pod := createDummyPod()
			err := client.Create(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.ObjectMeta.Generation).To(Equal(int64(1)))

			podCopy := &corev1.Pod{}
			err = client.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod1"}, podCopy)
			Expect(err).NotTo(HaveOccurred())
			Expect(podCopy.Name).To(Equal("pod1"))
			Expect(len(podCopy.Spec.Containers)).To(Equal(1))
			Expect(podCopy.Spec.Containers[0].Name).To(Equal("test-container"))
			Expect(podCopy.ObjectMeta.Generation).To(Equal(int64(1)))
		})
	})

	When("creating and getting a Pod", func() {
		var initialPod *corev1.Pod

		BeforeEach(func() {
			initialPod = createDummyPod()
			initialPod.Labels[fdbtypes.FDBInstanceIDLabel] = "storage-1"
			err := client.Create(context.TODO(), initialPod)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create a mock IP", func() {
			pod := &corev1.Pod{}
			err := client.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod1"}, pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.Name).To(Equal("pod1"))
			Expect(len(pod.Spec.Containers)).To(Equal(1))
			Expect(pod.Spec.Containers[0].Name).To(Equal("test-container"))
			Expect(pod.ObjectMeta.Generation).To(Equal(int64(1)))
			Expect(pod.Status.PodIP).To(Equal("1.1.1.1"))
		})

		When("Removing the Pod IP", func() {
			BeforeEach(func() {
				err := client.RemovePodIP(initialPod)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should return an empty Pod IP", func() {
				pod := &corev1.Pod{}
				err := client.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod1"}, pod)
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
			err := client.Create(context.TODO(), createDummyPod())
			Expect(err).NotTo(HaveOccurred())

			err = client.Create(context.TODO(), createDummyPod())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("Conflict"))
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
			err := client.Create(context.TODO(), service)
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
			err = client.Create(context.TODO(), service)
			Expect(err).NotTo(HaveOccurred())
			Expect(service.ObjectMeta.Generation).To(Equal(int64(1)))
			Expect(service.Spec.ClusterIP).To(Equal("None"))
		})
	})

	When("getting a missing object", func() {
		It("return an is not found error", func() {
			err := client.Create(context.TODO(), createDummyPod())
			Expect(err).NotTo(HaveOccurred())

			pod := &corev1.Pod{}
			err = client.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod2"}, pod)
			Expect(err).To(HaveOccurred())
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())

			deployment := &appsv1.Deployment{}
			err = client.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod1"}, deployment)
			Expect(err).To(HaveOccurred())
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		})
	})

	When("deleting an object", func() {
		It("it should be able to delete the object or return an error", func() {
			pod := createDummyPod()
			err := client.Create(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred())

			objectKey := types.NamespacedName{Namespace: "default", Name: "pod1"}
			err = client.Delete(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred())

			podCopy := &corev1.Pod{}
			err = client.Get(context.TODO(), objectKey, podCopy)
			Expect(err).To(HaveOccurred())
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())

			err = client.Delete(context.TODO(), pod)
			Expect(err).To(HaveOccurred())
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		})
	})

	When("deleting a Pod in terminating state", func() {
		It("the stuck Pod will stay in the registry", func() {
			pod1 := createDummyPod()
			pod2 := createDummyPod()
			pod2.Name = "pod2"

			err := client.Create(context.TODO(), pod1)
			Expect(err).NotTo(HaveOccurred())
			err = client.Create(context.TODO(), pod2)
			Expect(err).NotTo(HaveOccurred())

			err = client.MockStuckTermination(pod1, true)
			Expect(err).NotTo(HaveOccurred())

			err = client.Delete(context.TODO(), pod1)
			Expect(err).NotTo(HaveOccurred())

			podCopy := &corev1.Pod{}
			err = client.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod1"}, podCopy)
			Expect(err).NotTo(HaveOccurred())

			err = client.Delete(context.TODO(), pod2)
			Expect(err).NotTo(HaveOccurred())

			err = client.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod2"}, podCopy)
			Expect(err).To(HaveOccurred())
			Expect(k8serrors.IsNotFound(err)).To(BeTrue())
		})
	})

	When("updating an object", func() {
		It("the object should be updated", func() {
			pod := createDummyPod()
			err := client.Create(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred())

			container := &pod.Spec.Containers[0]
			container.Env = append(container.Env, corev1.EnvVar{Name: "test-env"})

			err = client.Update(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.ObjectMeta.Generation).To(Equal(int64(2)))

			podCopy := &corev1.Pod{}
			err = client.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod1"}, podCopy)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(podCopy.Spec.Containers)).To(Equal(1))
			Expect(len(podCopy.Spec.Containers[0].Env)).To(Equal(1))
			Expect(podCopy.Spec.Containers[0].Env[0].Name).To(Equal("test-env"))
			Expect(podCopy.ObjectMeta.Generation).To(Equal(int64(2)))

			err = client.Update(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.ObjectMeta.Generation).To(Equal(int64(2)))
		})
	})

	When("updating the object status", func() {
		It("should update the object status", func() {
			pod := createDummyPod()
			err := client.Create(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred())

			container := &pod.Spec.Containers[0]
			container.Env = append(container.Env, corev1.EnvVar{Name: "test-env"})
			pod.Status.HostIP = "foo"

			err = client.Status().Update(context.TODO(), pod)
			Expect(err).NotTo(HaveOccurred())
			Expect(pod.ObjectMeta.Generation).To(Equal(int64(1)))

			podCopy := &corev1.Pod{}
			err = client.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod1"}, podCopy)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(podCopy.Spec.Containers)).To(Equal(1))
			Expect(len(podCopy.Spec.Containers[0].Env)).To(Equal(0))
			Expect(podCopy.Status.HostIP).To(Equal("foo"))
			Expect(podCopy.ObjectMeta.Generation).To(Equal(int64(1)))
		})
	})

	When("listing objects", func() {
		It("should list the objects", func() {
			pod1 := createDummyPod()
			pod2 := createDummyPod()
			pod2.Name = "pod2"

			err := client.Create(context.TODO(), pod1)
			Expect(err).NotTo(HaveOccurred())
			err = client.Create(context.TODO(), pod2)
			Expect(err).NotTo(HaveOccurred())

			pods := &corev1.PodList{}
			err = client.List(context.TODO(), pods)
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

			err := client.Create(context.TODO(), pod1)
			Expect(err).NotTo(HaveOccurred())
			err = client.Create(context.TODO(), pod2)
			Expect(err).NotTo(HaveOccurred())

			pods := &corev1.PodList{}
			err = client.List(context.TODO(), pods, ctrlClient.InNamespace("default"))
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pods.Items)).To(Equal(2))

			err = client.List(context.TODO(), pods, ctrlClient.InNamespace("not-default"))
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

			err := client.Create(context.TODO(), pod1)
			Expect(err).NotTo(HaveOccurred())
			err = client.Create(context.TODO(), pod2)
			Expect(err).NotTo(HaveOccurred())
			err = client.Create(context.TODO(), pod3)
			Expect(err).NotTo(HaveOccurred())

			pods := &corev1.PodList{}
			err = client.List(context.TODO(), pods, ctrlClient.MatchingLabels(map[string]string{"app": "app2"}))
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pods.Items)).To(Equal(1))
			Expect(pods.Items[0].Name).To(Equal("pod2"))

			appRequirement, err := labels.NewRequirement("app", selection.Exists, nil)
			Expect(err).NotTo(HaveOccurred())
			pods = &corev1.PodList{}
			err = client.List(context.TODO(), pods, ctrlClient.MatchingLabelsSelector{Selector: labels.NewSelector().Add(*appRequirement)})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pods.Items)).To(Equal(2))
			sortPodsByName(pods)
			Expect(pods.Items[0].Name).To(Equal("pod1"))
			Expect(pods.Items[1].Name).To(Equal("pod2"))
		})
	})

	When("listing objects by field", func() {
		It("should list the objects with the field set", func() {
			pod1 := createDummyPod()
			pod2 := createDummyPod()
			pod2.Name = "pod2"

			err := client.Create(context.TODO(), pod1)
			Expect(err).NotTo(HaveOccurred())
			err = client.Create(context.TODO(), pod2)
			Expect(err).NotTo(HaveOccurred())

			pods := &corev1.PodList{}
			err = client.List(context.TODO(), pods, ctrlClient.MatchingFields{"metadata.name": "pod1"})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(pods.Items)).To(Equal(1))
			Expect(pods.Items[0].Name).To(Equal("pod1"))
		})
	})

	When("creating an event", func() {
		It("should create the event", func() {
			pod := createDummyPod()
			pod.ObjectMeta.UID = uuid.NewUUID()
			client.Event(pod, "Testing", "This is a test", "Test message")
			events := &corev1.EventList{}
			err := client.List(context.TODO(), events)
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
			client.Eventf(createDummyPod(), "Testing", "This is a test", "Test message: %d", 5)
			events := &corev1.EventList{}
			err := client.List(context.TODO(), events)
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
			client.PastEventf(createDummyPod(), timestamp, "Testing", "This is a test", "Test message: %d", 5)
			events := &corev1.EventList{}
			err := client.List(context.TODO(), events)
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
			client.AnnotatedEventf(createDummyPod(), map[string]string{"anno": "value"}, "Testing", "This is a test", "Test message: %d", 5)
			events := &corev1.EventList{}
			err := client.List(context.TODO(), events)
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
})

func sortPodsByName(pods *corev1.PodList) {
	sort.Slice(pods.Items, func(i, j int) bool {
		return pods.Items[i].ObjectMeta.Name < pods.Items[j].ObjectMeta.Name
	})
}
