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
	"encoding/json"
	"fmt"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"k8s.io/apimachinery/pkg/runtime/schema"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apimachinery/pkg/watch"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// Make sure our MockClient implements all methods for the WithWatch interface.
var _ ctrlClient.WithWatch = (*MockClient)(nil)

// MockClient provides a mock Kubernetes client.
type MockClient struct {
	// fakeClient
	fakeClient ctrlClient.WithWatch

	// ipCounter provides monotonically incrementing IP addresses.
	ipCounter int

	// scheme will be used to initialize or reset the new fake client
	scheme *runtime.Scheme

	// createHooks allow to inject custom logic to the creation of objects. See serviceCreateHook and podCreateHook as
	// examples.
	// TODO (johscheuer): Evaluate https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/client/fake#ClientBuilder.WithInterceptorFuncs
	createHooks []func(ctx context.Context, client *MockClient, object ctrlClient.Object) error

	// updateHooks allow to inject custom logic to the update of objects.
	// TODO (johscheuer): Evaluate https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/client/fake#ClientBuilder.WithInterceptorFuncs
	updateHooks []func(ctx context.Context, client *MockClient, object ctrlClient.Object) error

	// createIndexes defines if the MockClient should create a predefined set of Indexer.
	createIndexes bool
}

// NewMockClient creates a new MockClient.
func NewMockClient(
	scheme *runtime.Scheme,
	hooks ...func(_ context.Context, client *MockClient, object ctrlClient.Object) error,
) *MockClient {
	return NewMockClientWithHooks(scheme, hooks, nil)
}

// NewMockClientWithHooks creates a new MockClient with hooks.
func NewMockClientWithHooks(
	scheme *runtime.Scheme,
	createHooks []func(ctx context.Context, client *MockClient, object ctrlClient.Object) error,
	updateHooks []func(ctx context.Context, client *MockClient, object ctrlClient.Object) error,
) *MockClient {
	return NewMockClientWithHooksAndIndexes(scheme, createHooks, updateHooks, false)
}

// NewMockClientWithHooksAndIndexes creates a new MockClient with hooks and indexes.
func NewMockClientWithHooksAndIndexes(
	scheme *runtime.Scheme,
	createHooks []func(ctx context.Context, client *MockClient, object ctrlClient.Object) error,
	updateHooks []func(ctx context.Context, client *MockClient, object ctrlClient.Object) error,
	createIndexes bool,
) *MockClient {
	serviceCreateHook := func(_ context.Context, client *MockClient, object ctrlClient.Object) error {
		svc, isSvc := object.(*corev1.Service)
		if !isSvc {
			return nil
		}

		if svc.Spec.ClusterIP == "" {
			svc.Spec.ClusterIP = client.generateIP()
		}

		return nil
	}

	podCreateHook := func(ctx context.Context, client *MockClient, object ctrlClient.Object) error {
		pod, isPod := object.(*corev1.Pod)
		if !isPod {
			return nil
		}

		if pod.Status.PodIP == "" {
			v4Address := client.generatePodIPv4()
			pod.Status.PodIP = v4Address
		}

		if len(pod.Status.PodIPs) == 0 {
			// TODO (johscheuer) We should check here if the added address was IPv4 or not.
			pod.Status.PodIPs = []corev1.PodIP{
				{IP: pod.Status.PodIP},
				{IP: client.generatePodIPv6()},
			}
		}

		if pod.Status.Phase == "" {
			pod.Status.Phase = corev1.PodRunning
		}

		nodeName := fmt.Sprintf("%s-node", pod.Name)
		node := corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName},
		}
		err := client.Get(ctx, ctrlClient.ObjectKey{Name: nodeName}, &node)
		if pod.Spec.NodeName == "" { // Do not override user-set NodeName so that we can simulate multiple pods on a single node
			pod.Spec.NodeName = node.Name
		}
		if errors.IsNotFound(err) {
			return client.Create(context.Background(), &node)
		}

		return err
	}

	mockClient := &MockClient{
		scheme:        scheme,
		createHooks:   append(createHooks, serviceCreateHook, podCreateHook),
		updateHooks:   updateHooks,
		createIndexes: createIndexes,
	}

	mockClient.setNewFakeClient()
	return mockClient
}

// Clear erases any mock data.
func (client *MockClient) setNewFakeClient() {
	builder := fake.NewClientBuilder()

	// We have to create those indexes, otherwise the fake client is complaining. This is only the case for the
	// kubectl-fdb plugin tests.
	if client.createIndexes {
		builder = builder.WithIndex(&corev1.Pod{}, "spec.nodeName", func(o ctrlClient.Object) []string {
			return []string{o.(*corev1.Pod).Spec.NodeName}
		}).
			WithIndex(&corev1.Pod{}, "status.phase", func(o ctrlClient.Object) []string {
				return []string{string(o.(*corev1.Pod).Status.Phase)}
			})
	}

	builder.WithScheme(client.scheme)
	// See: https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/client/fake#ClientBuilder.WithStatusSubresource
	// otherwise the status update/patch calls will fail.
	builder.WithStatusSubresource(&fdbv1beta2.FoundationDBCluster{})
	builder.WithStatusSubresource(&fdbv1beta2.FoundationDBBackup{})
	builder.WithStatusSubresource(&fdbv1beta2.FoundationDBRestore{})
	client.fakeClient = builder.Build()
}

// Clear erases any mock data.
func (client *MockClient) Clear() {
	client.setNewFakeClient()
}

// Scheme returns the runtime Scheme
func (client *MockClient) Scheme() *runtime.Scheme {
	return runtime.NewScheme()
}

// RESTMapper returns the RESTMapper
func (client *MockClient) RESTMapper() meta.RESTMapper {
	return client.fakeClient.RESTMapper()
}

// generateIP generates a unique IP address.
func (client *MockClient) generateIP() string {
	client.ipCounter++
	return fmt.Sprintf("192.168.%d.%d", client.ipCounter/256, client.ipCounter%256)
}

// Create creates a new object
func (client *MockClient) Create(
	ctx context.Context,
	object ctrlClient.Object,
	options ...ctrlClient.CreateOption,
) error {
	// Ensure the default values are set if not present.
	if object.GetCreationTimestamp().Time.IsZero() {
		object.SetCreationTimestamp(metav1.Time{Time: time.Now()})
	}

	object.SetGeneration(object.GetGeneration() + 1)
	object.SetUID(uuid.NewUUID())

	for _, hook := range client.createHooks {
		err := hook(ctx, client, object)
		if err != nil {
			return err
		}
	}

	return client.fakeClient.Create(ctx, object, options...)
}

// Get retrieves an object.
func (client *MockClient) Get(
	ctx context.Context,
	key ctrlClient.ObjectKey,
	object ctrlClient.Object,
	options ...ctrlClient.GetOption,
) error {
	return client.fakeClient.Get(ctx, key, object, options...)
}

// List lists objects.
func (client *MockClient) List(
	ctx context.Context,
	list ctrlClient.ObjectList,
	options ...ctrlClient.ListOption,
) error {
	// TODO (johscheuer): Once https://github.com/kubernetes-sigs/controller-runtime/pull/2025 is merged and we update the
	// controller-runtime we will support field selectors.
	return client.fakeClient.List(ctx, list, options...)
}

// Delete deletes an object.
func (client *MockClient) Delete(
	ctx context.Context,
	object ctrlClient.Object,
	options ...ctrlClient.DeleteOption,
) error {
	return client.fakeClient.Delete(ctx, object, options...)
}

func getMapFromObject(object ctrlClient.Object) (map[string]interface{}, error) {
	jsonData, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}

	newMap := make(map[string]interface{})
	err = json.Unmarshal(jsonData, &newMap)
	if err != nil {
		return nil, err
	}

	return newMap, nil
}

func (client *MockClient) hasSpecChanges(
	existingObject ctrlClient.Object,
	newObject ctrlClient.Object,
) (bool, error) {
	newObjectMap, err := getMapFromObject(newObject)
	if err != nil {
		return false, err
	}

	existingObjectMap, err := getMapFromObject(existingObject)
	if err != nil {
		return false, err
	}

	return !equality.Semantic.DeepEqual(existingObjectMap["spec"], newObjectMap["spec"]), nil
}

// Update updates an object.
func (client *MockClient) Update(
	ctx context.Context,
	object ctrlClient.Object,
	options ...ctrlClient.UpdateOption,
) error {
	existingObject := object.DeepCopyObject().(ctrlClient.Object)
	err := client.fakeClient.Get(ctx, ctrlClient.ObjectKeyFromObject(object), existingObject)
	if err != nil {
		return err
	}

	for _, hook := range client.updateHooks {
		err = hook(ctx, client, object)
		if err != nil {
			return err
		}
	}

	hasSpecChanges, err := client.hasSpecChanges(existingObject, object)
	if err != nil {
		return err
	}

	if hasSpecChanges {
		object.SetGeneration(existingObject.GetGeneration() + 1)
	}

	return client.fakeClient.Update(ctx, object, options...)
}

// Patch patches an object.
func (client *MockClient) Patch(
	ctx context.Context,
	obj ctrlClient.Object,
	patch ctrlClient.Patch,
	options ...ctrlClient.PatchOption,
) error {
	// Currently the SSA patch type is not supported in the fake client: https://github.com/kubernetes/client-go/issues/992
	return client.fakeClient.Patch(ctx, obj, patch, options...)
}

// DeleteAllOf deletes all objects of the given type matching the given options.
func (client *MockClient) DeleteAllOf(
	ctx context.Context,
	object ctrlClient.Object,
	options ...ctrlClient.DeleteAllOfOption,
) error {
	return client.fakeClient.DeleteAllOf(ctx, object, options...)
}

// MockStuckTermination sets a flag determining whether an object should get stuck in terminating when it is deleted by setting a finalizer.
func (client *MockClient) MockStuckTermination(object ctrlClient.Object, terminating bool) error {
	if terminating {
		object.SetFinalizers(append(object.GetFinalizers(), "foundationdb.org/testing"))
	} else {
		object.SetFinalizers(nil)
	}

	// We have to update the state in the mock client
	return client.Update(context.Background(), object)
}

// SubResource returns a client for the specified SubResource.
func (client *MockClient) SubResource(subResource string) ctrlClient.SubResourceClient {
	return client.fakeClient.SubResource(subResource)
}

// MockStatusClient wraps a client to provide specialized operations for
// updating status.
type MockStatusClient struct {
	*MockClient
}

// Create will create the specified SubResource
func (client MockStatusClient) Create(
	ctx context.Context,
	obj ctrlClient.Object,
	subResource ctrlClient.Object,
	opts ...ctrlClient.SubResourceCreateOption,
) error {
	return client.fakeClient.Status().Create(ctx, obj, subResource, opts...)
}

// Update updates an object.
// This does not support the options argument yet.
func (client MockStatusClient) Update(
	ctx context.Context,
	object ctrlClient.Object,
	options ...ctrlClient.SubResourceUpdateOption,
) error {
	return client.fakeClient.Status().Update(ctx, object, options...)
}

// Patch patches an object's status.
// This is not yet implemented.
func (client MockStatusClient) Patch(
	ctx context.Context,
	object ctrlClient.Object,
	patch ctrlClient.Patch,
	options ...ctrlClient.SubResourcePatchOption,
) error {
	// Currently the SSA patch type is not supported in the fake client: https://github.com/kubernetes/client-go/issues/992
	return client.fakeClient.Status().Patch(ctx, object, patch, options...)
}

// Status returns a writer for updating status.
func (client *MockClient) Status() ctrlClient.StatusWriter {
	return MockStatusClient{client}
}

func (client *MockClient) createEvent(event *corev1.Event) {
	err := client.Create(context.TODO(), event)
	if err != nil {
		panic(err)
	}
}

func buildEvent(
	object runtime.Object,
	eventType string,
	reason string,
	message string,
) *corev1.Event {
	objectMeta, _ := meta.Accessor(object)

	return &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: objectMeta.GetNamespace(),
			Name:      fmt.Sprintf("%s-%v", eventType, uuid.NewUUID()),
		},
		InvolvedObject: corev1.ObjectReference{
			Namespace: objectMeta.GetNamespace(),
			Name:      objectMeta.GetName(),
			UID:       objectMeta.GetUID(),
		},
		Type:      eventType,
		Message:   message,
		Reason:    reason,
		EventTime: metav1.MicroTime{Time: time.Now()},
	}
}

// Event sends an event
func (client *MockClient) Event(
	object runtime.Object,
	eventType string,
	reason string,
	message string,
) {
	client.createEvent(buildEvent(object, eventType, reason, message))
}

// Eventf is just like Event, but with Sprintf for the message field.
func (client *MockClient) Eventf(
	object runtime.Object,
	eventType string,
	reason string,
	messageFormat string,
	args ...interface{},
) {
	client.createEvent(buildEvent(object, eventType, reason, fmt.Sprintf(messageFormat, args...)))
}

// PastEventf is just like Eventf, but with an option to specify the event's 'timestamp' field.
func (client *MockClient) PastEventf(
	object runtime.Object,
	timestamp metav1.Time,
	eventType string,
	reason string,
	messageFormat string,
	args ...interface{},
) {
	event := buildEvent(object, eventType, reason, fmt.Sprintf(messageFormat, args...))
	event.EventTime = metav1.MicroTime(timestamp)
	client.createEvent(event)
}

// AnnotatedEventf is just like eventf, but with annotations attached
func (client *MockClient) AnnotatedEventf(
	object runtime.Object,
	annotations map[string]string,
	eventType string,
	reason string,
	messageFormat string,
	args ...interface{},
) {
	event := buildEvent(object, eventType, reason, fmt.Sprintf(messageFormat, args...))
	event.ObjectMeta.Annotations = annotations
	client.createEvent(event)
}

// SetPodIntoFailed sets a Pod into a failed status with the given reason
func (client *MockClient) SetPodIntoFailed(
	ctx context.Context,
	object ctrlClient.Object,
	reason string,
) error {
	existingObject := object.DeepCopyObject().(ctrlClient.Object)
	err := client.Get(ctx, ctrlClient.ObjectKeyFromObject(object), existingObject)
	if err != nil {
		return err
	}

	pod, ok := existingObject.(*corev1.Pod)
	if !ok {
		return fmt.Errorf("expected to get a corev1.Pod as input object, got: %s", object)
	}

	pod.Status.Phase = corev1.PodFailed
	pod.Status.Reason = reason
	err = client.Status().Update(ctx, pod)
	if err != nil {
		return err
	}

	pod.CreationTimestamp = metav1.Time{Time: time.Now().Add(-30 * time.Minute)}

	return client.Update(ctx, pod)
}

// RemovePodIP sets the IP address of the Pod to an empty string
func (client *MockClient) RemovePodIP(pod *corev1.Pod) error {
	pod.Status.PodIP = ""
	pod.Status.PodIPs = nil

	return client.Status().Update(context.TODO(), pod)
}

// generatePodIPv4 generates a mock IPv4 address for Pods
func (client *MockClient) generatePodIPv4() string {
	client.ipCounter++
	return fmt.Sprintf("1.1.%d.%d", client.ipCounter/256, client.ipCounter%256)
}

// generatePodIPv6 generates a mock IPv6 address for Pods
func (client *MockClient) generatePodIPv6() string {
	client.ipCounter++
	if client.ipCounter < 256 {
		return fmt.Sprintf("::%d", client.ipCounter)
	}
	return fmt.Sprintf("::%d:%d", client.ipCounter/256, client.ipCounter%256)
}

// Watch implements the watch methods of the controller runtime client.
func (client *MockClient) Watch(
	ctx context.Context,
	obj ctrlClient.ObjectList,
	opts ...ctrlClient.ListOption,
) (watch.Interface, error) {
	return client.fakeClient.Watch(ctx, obj, opts...)
}

// GroupVersionKindFor returns the GroupVersionKind for the given object.
func (client *MockClient) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	return client.fakeClient.GroupVersionKindFor(obj)
}

// IsObjectNamespaced returns true if the GroupVersionKind of the object is namespaced.
func (client *MockClient) IsObjectNamespaced(obj runtime.Object) (bool, error) {
	return client.fakeClient.IsObjectNamespaced(obj)
}
