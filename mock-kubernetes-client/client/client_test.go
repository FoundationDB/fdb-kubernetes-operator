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
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"

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

func TestBasicCreateAndGetOperations(t *testing.T) {
	g := NewWithT(t)
	client := &MockClient{}
	pod := createDummyPod()
	err := client.Create(context.TODO(), pod)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(pod.ObjectMeta.Generation).To(Equal(int64(1)))

	podCopy := &corev1.Pod{}
	err = client.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod1"}, podCopy)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(podCopy.Name).To(Equal("pod1"))
	g.Expect(len(podCopy.Spec.Containers)).To(Equal(1))
	g.Expect(podCopy.Spec.Containers[0].Name).To(Equal("test-container"))
	g.Expect(podCopy.ObjectMeta.Generation).To(Equal(int64(1)))
}

func TestCreatingObjectTwice(t *testing.T) {
	g := NewWithT(t)
	client := &MockClient{}
	err := client.Create(context.TODO(), createDummyPod())
	g.Expect(err).NotTo(HaveOccurred())

	err = client.Create(context.TODO(), createDummyPod())
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(Equal("Conflict"))
}

func TestCreatingService(t *testing.T) {
	g := NewWithT(t)
	client := &MockClient{}
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "service1",
		},
	}
	err := client.Create(context.TODO(), service)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(service.ObjectMeta.Generation).To(Equal(int64(1)))
	g.Expect(service.Spec.ClusterIP).To(Equal("192.168.0.1"))

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
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(service.ObjectMeta.Generation).To(Equal(int64(1)))
	g.Expect(service.Spec.ClusterIP).To(Equal("None"))
}

func TestGettingMissingObject(t *testing.T) {
	g := NewWithT(t)
	client := &MockClient{}

	err := client.Create(context.TODO(), createDummyPod())
	g.Expect(err).NotTo(HaveOccurred())

	pod := &corev1.Pod{}
	err = client.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod2"}, pod)
	g.Expect(err).To(HaveOccurred())
	g.Expect(k8serrors.IsNotFound(err)).To(BeTrue())

	deployment := &appsv1.Deployment{}
	err = client.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod1"}, deployment)
	g.Expect(err).To(HaveOccurred())
	g.Expect(k8serrors.IsNotFound(err)).To(BeTrue())
}

func TestDeletingObject(t *testing.T) {
	g := NewWithT(t)
	client := &MockClient{}

	pod := createDummyPod()
	err := client.Create(context.TODO(), pod)
	g.Expect(err).NotTo(HaveOccurred())

	objectKey := types.NamespacedName{Namespace: "default", Name: "pod1"}
	err = client.Delete(context.TODO(), pod)
	g.Expect(err).NotTo(HaveOccurred())

	podCopy := &corev1.Pod{}
	err = client.Get(context.TODO(), objectKey, podCopy)
	g.Expect(err).To(HaveOccurred())
	g.Expect(k8serrors.IsNotFound(err)).To(BeTrue())

	err = client.Delete(context.TODO(), pod)
	g.Expect(err).To(HaveOccurred())
	g.Expect(k8serrors.IsNotFound(err)).To(BeTrue())
}

func TestUpdatingObject(t *testing.T) {
	g := NewWithT(t)
	client := &MockClient{}

	pod := createDummyPod()
	err := client.Create(context.TODO(), pod)
	g.Expect(err).NotTo(HaveOccurred())

	container := &pod.Spec.Containers[0]
	container.Env = append(container.Env, corev1.EnvVar{Name: "test-env"})

	err = client.Update(context.TODO(), pod)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(pod.ObjectMeta.Generation).To(Equal(int64(2)))

	podCopy := &corev1.Pod{}
	err = client.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod1"}, podCopy)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(podCopy.Spec.Containers)).To(Equal(1))
	g.Expect(len(podCopy.Spec.Containers[0].Env)).To(Equal(1))
	g.Expect(podCopy.Spec.Containers[0].Env[0].Name).To(Equal("test-env"))
	g.Expect(podCopy.ObjectMeta.Generation).To(Equal(int64(2)))

	err = client.Update(context.TODO(), pod)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(pod.ObjectMeta.Generation).To(Equal(int64(2)))
}

func TestUpdatingObjectStatus(t *testing.T) {
	g := NewWithT(t)
	client := &MockClient{}

	pod := createDummyPod()
	err := client.Create(context.TODO(), pod)
	g.Expect(err).NotTo(HaveOccurred())

	container := &pod.Spec.Containers[0]
	container.Env = append(container.Env, corev1.EnvVar{Name: "test-env"})
	pod.Status.HostIP = "foo"

	err = client.Status().Update(context.TODO(), pod)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(pod.ObjectMeta.Generation).To(Equal(int64(1)))

	podCopy := &corev1.Pod{}
	err = client.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "pod1"}, podCopy)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(podCopy.Spec.Containers)).To(Equal(1))
	g.Expect(len(podCopy.Spec.Containers[0].Env)).To(Equal(0))
	g.Expect(podCopy.Status.HostIP).To(Equal("foo"))
	g.Expect(podCopy.ObjectMeta.Generation).To(Equal(int64(1)))
}

func sortPodsByName(pods *corev1.PodList) {
	sort.Slice(pods.Items, func(i, j int) bool {
		return pods.Items[i].ObjectMeta.Name < pods.Items[j].ObjectMeta.Name
	})
}

func TestListingObjects(t *testing.T) {
	g := NewWithT(t)
	client := &MockClient{}

	pod1 := createDummyPod()
	pod2 := createDummyPod()
	pod2.Name = "pod2"

	err := client.Create(context.TODO(), pod1)
	g.Expect(err).NotTo(HaveOccurred())
	err = client.Create(context.TODO(), pod2)
	g.Expect(err).NotTo(HaveOccurred())

	pods := &corev1.PodList{}
	err = client.List(context.TODO(), pods)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(pods.Items)).To(Equal(2))

	sortPodsByName(pods)
	g.Expect(pods.Items[0].Name).To(Equal("pod1"))
	g.Expect(pods.Items[1].Name).To(Equal("pod2"))
}

func TestListingObjectsByNamespace(t *testing.T) {
	g := NewWithT(t)
	client := &MockClient{}

	pod1 := createDummyPod()
	pod2 := createDummyPod()
	pod2.Name = "pod2"

	err := client.Create(context.TODO(), pod1)
	g.Expect(err).NotTo(HaveOccurred())
	err = client.Create(context.TODO(), pod2)
	g.Expect(err).NotTo(HaveOccurred())

	pods := &corev1.PodList{}
	err = client.List(context.TODO(), pods, ctrlClient.InNamespace("default"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(pods.Items)).To(Equal(2))

	err = client.List(context.TODO(), pods, ctrlClient.InNamespace("not-default"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(pods.Items)).To(Equal(0))
}

func TestListingObjectsByLabel(t *testing.T) {
	g := NewWithT(t)
	client := &MockClient{}

	pod1 := createDummyPod()
	pod2 := createDummyPod()
	pod2.Name = "pod2"
	pod2.ObjectMeta.Labels["app"] = "app2"
	pod3 := createDummyPod()
	pod3.Name = "pod3"
	pod3.Labels = nil

	err := client.Create(context.TODO(), pod1)
	g.Expect(err).NotTo(HaveOccurred())
	err = client.Create(context.TODO(), pod2)
	g.Expect(err).NotTo(HaveOccurred())
	err = client.Create(context.TODO(), pod3)
	g.Expect(err).NotTo(HaveOccurred())

	pods := &corev1.PodList{}
	err = client.List(context.TODO(), pods, ctrlClient.MatchingLabels(map[string]string{"app": "app2"}))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(pods.Items)).To(Equal(1))
	g.Expect(pods.Items[0].Name).To(Equal("pod2"))

	appRequirement, err := labels.NewRequirement("app", selection.Exists, nil)
	g.Expect(err).NotTo(HaveOccurred())
	pods = &corev1.PodList{}
	err = client.List(context.TODO(), pods, ctrlClient.MatchingLabelsSelector{Selector: labels.NewSelector().Add(*appRequirement)})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(pods.Items)).To(Equal(2))
	sortPodsByName(pods)
	g.Expect(pods.Items[0].Name).To(Equal("pod1"))
	g.Expect(pods.Items[1].Name).To(Equal("pod2"))
}

func TestListingObjectsByField(t *testing.T) {
	g := NewWithT(t)
	client := &MockClient{}

	pod1 := createDummyPod()
	pod2 := createDummyPod()
	pod2.Name = "pod2"

	err := client.Create(context.TODO(), pod1)
	g.Expect(err).NotTo(HaveOccurred())
	err = client.Create(context.TODO(), pod2)
	g.Expect(err).NotTo(HaveOccurred())

	pods := &corev1.PodList{}
	err = client.List(context.TODO(), pods, ctrlClient.MatchingFields{"metadata.name": "pod1"})
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(pods.Items)).To(Equal(1))
	g.Expect(pods.Items[0].Name).To(Equal("pod1"))
}

func TestCreatingEvent(t *testing.T) {
	g := NewWithT(t)
	client := &MockClient{}
	pod := createDummyPod()
	pod.ObjectMeta.UID = uuid.NewUUID()
	client.Event(pod, "Testing", "This is a test", "Test message")
	events := &corev1.EventList{}
	err := client.List(context.TODO(), events)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(events.Items)).To(Equal(1))
	g.Expect(events.Items[0].Namespace).To(Equal("default"))
	g.Expect(events.Items[0].Name).To(HavePrefix("Testing-"))
	g.Expect(events.Items[0].InvolvedObject.Namespace).To(Equal("default"))
	g.Expect(events.Items[0].InvolvedObject.Name).To(Equal("pod1"))
	g.Expect(events.Items[0].InvolvedObject.UID).To(Equal(pod.ObjectMeta.UID))
	g.Expect(events.Items[0].Type).To(Equal("Testing"))
	g.Expect(events.Items[0].Reason).To(Equal("This is a test"))
	g.Expect(events.Items[0].Message).To(Equal("Test message"))
	g.Expect(events.Items[0].EventTime.UnixNano()).NotTo(Equal(0))
}

func TestCreatingEventWithMessageFormat(t *testing.T) {
	g := NewWithT(t)
	client := &MockClient{}
	client.Eventf(createDummyPod(), "Testing", "This is a test", "Test message: %d", 5)
	events := &corev1.EventList{}
	err := client.List(context.TODO(), events)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(events.Items)).To(Equal(1))
	g.Expect(events.Items[0].Namespace).To(Equal("default"))
	g.Expect(events.Items[0].Name).To(HavePrefix("Testing-"))
	g.Expect(events.Items[0].InvolvedObject.Namespace).To(Equal("default"))
	g.Expect(events.Items[0].InvolvedObject.Name).To(Equal("pod1"))
	g.Expect(events.Items[0].Type).To(Equal("Testing"))
	g.Expect(events.Items[0].Reason).To(Equal("This is a test"))
	g.Expect(events.Items[0].Message).To(Equal("Test message: 5"))
}

func TestCreatingEventInPast(t *testing.T) {
	g := NewWithT(t)
	client := &MockClient{}
	timestamp := metav1.Time{Time: time.Now().Add(-1 * time.Minute)}

	client.PastEventf(createDummyPod(), timestamp, "Testing", "This is a test", "Test message: %d", 5)
	events := &corev1.EventList{}
	err := client.List(context.TODO(), events)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(events.Items)).To(Equal(1))
	g.Expect(events.Items[0].Namespace).To(Equal("default"))
	g.Expect(events.Items[0].Name).To(HavePrefix("Testing-"))
	g.Expect(events.Items[0].InvolvedObject.Namespace).To(Equal("default"))
	g.Expect(events.Items[0].InvolvedObject.Name).To(Equal("pod1"))
	g.Expect(events.Items[0].Type).To(Equal("Testing"))
	g.Expect(events.Items[0].Reason).To(Equal("This is a test"))
	g.Expect(events.Items[0].Message).To(Equal("Test message: 5"))
	g.Expect(events.Items[0].EventTime.Unix()).To(Equal(timestamp.Unix()))
}

func TestCreatingEventWithAnnotations(t *testing.T) {
	g := NewWithT(t)
	client := &MockClient{}

	client.AnnotatedEventf(createDummyPod(), map[string]string{"anno": "value"}, "Testing", "This is a test", "Test message: %d", 5)
	events := &corev1.EventList{}
	err := client.List(context.TODO(), events)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(len(events.Items)).To(Equal(1))
	g.Expect(events.Items[0].Namespace).To(Equal("default"))
	g.Expect(events.Items[0].Name).To(HavePrefix("Testing-"))
	g.Expect(events.Items[0].InvolvedObject.Namespace).To(Equal("default"))
	g.Expect(events.Items[0].InvolvedObject.Name).To(Equal("pod1"))
	g.Expect(events.Items[0].Type).To(Equal("Testing"))
	g.Expect(events.Items[0].Reason).To(Equal("This is a test"))
	g.Expect(events.Items[0].Message).To(Equal("Test message: 5"))
	g.Expect(events.Items[0].ObjectMeta.Annotations).To(Equal(map[string]string{"anno": "value"}))
}
