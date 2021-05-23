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
	ctx "context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes/scheme"
	ctrlClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// MockClient provides a mock Kubernetes client.
type MockClient struct {
	// data is the internal mock data.
	// This maps a type, and then a namespace/name, to the generic
	// representation of an object.
	data map[string]map[string]map[string]interface{}

	// ipCounter provides monotonically incrementing IP addresses.
	ipCounter int

	// stuckTerminatingObjects tracks which objects should be stuck in terminating.
	stuckTerminatingObjects map[string]map[string]bool
}

// Clear erases any mock data.
func (client *MockClient) Clear() {
	client.data = make(map[string]map[string]map[string]interface{})
	client.stuckTerminatingObjects = nil
}

// Scheme returns the runtime Scheme
func (client *MockClient) Scheme() *runtime.Scheme {
	return nil
}

// RESTMapper returns the RESTMapper
func (client *MockClient) RESTMapper() meta.RESTMapper {
	return nil
}

// buildKindKey gets the key identifying an object's type.
func buildKindKey(object runtime.Object) (string, error) {
	gvk, err := apiutil.GVKForObject(object, scheme.Scheme)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s/%s", gvk.Group, gvk.Version, gvk.Kind), nil
}

// buildObjectKey gets the key identifying an object.
func buildObjectKey(key ctrlClient.ObjectKey) string {
	return fmt.Sprintf("%s/%s", key.Namespace, key.Name)
}

// buildJSONObjectKey gets the key identifying an object from its JSON
// representation.
func buildJSONObjectKey(jsonData []byte) (string, error) {
	genericData := make(map[string]interface{})
	err := json.Unmarshal(jsonData, &genericData)
	if err != nil {
		return "", err
	}

	namespace, err := lookupJSONString(genericData, "metadata", "namespace")
	if err != nil {
		return "", err
	}
	name, err := lookupJSONString(genericData, "metadata", "name")
	if err != nil {
		return "", err
	}
	return buildObjectKey(types.NamespacedName{Namespace: namespace, Name: name}), nil
}

// buildRuntimeObjectKey gets the key identifying an object.
func buildRuntimeObjectKey(object ctrlClient.Object) (string, error) {
	jsonData, err := json.Marshal(object)
	if err != nil {
		return "", err
	}

	return buildJSONObjectKey(jsonData)
}

// fillInMaps ensures that we have maps for a given object type.
func (client *MockClient) fillInMaps(kind string) {
	if client.data == nil {
		client.data = make(map[string]map[string]map[string]interface{})
	}
	if client.data[kind] == nil {
		client.data[kind] = make(map[string]map[string]interface{})
	}
}

// lookupJSONValue looks up a value in a generic JSON object.
func lookupJSONValue(genericData map[string]interface{}, path []string, defaultValue interface{}) (interface{}, error) {
	currentData := genericData

	if len(path) == 0 {
		return "", fmt.Errorf("Cannot look up empty path")
	}
	var result interface{}
	for index, key := range path {
		genericValue, present := currentData[key]
		if !present {
			return defaultValue, nil
		}
		var ok bool
		if index == len(path)-1 {
			result = genericValue
		} else {
			currentData, ok = genericValue.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("Type error for key %v", path[0:index+1])
			}
		}
	}
	return result, nil
}

// lookupJSONString looks up a string in a generic JSON object.
func lookupJSONString(genericData map[string]interface{}, path ...string) (string, error) {
	result, err := lookupJSONValue(genericData, path, "")
	if err != nil {
		return "", err
	}
	stringResult, isString := result.(string)
	if !isString {
		return "", fmt.Errorf("type error for key %v", path)
	}
	return stringResult, nil
}

// lookupJSONStringMap looks up a map of string to string in a generic JSON object.
func lookupJSONStringMap(genericData map[string]interface{}, path ...string) (map[string]string, error) {
	currentData := genericData

	if len(path) == 0 {
		return nil, fmt.Errorf("Cannot look up empty path")
	}
	var result = make(map[string]string)
	for index, key := range path {
		var ok bool
		if index == len(path)-1 {
			genericValue, present := currentData[key]
			if !present {
				return nil, nil
			}

			mapValue, isMap := genericValue.(map[string]interface{})
			if !isMap {
				return nil, fmt.Errorf("Type error for %v", path)
			}
			for key, value := range mapValue {
				stringValue, isString := value.(string)
				if !isString {
					return nil, fmt.Errorf("Type error for key %s of %v", key, path)
				}
				result[key] = stringValue
			}
		} else {
			genericValue, present := currentData[key]
			if !present {
				return nil, fmt.Errorf("Missing key %v", path[0:index+1])
			}

			currentData, ok = genericValue.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("Type error for key %v", path[0:index+1])
			}
		}
	}
	return result, nil
}

// lookupJSONStringFields provides a map to all string fields in a generic JSON object.
// The paths to each field will be represented as a string of the form key1.key2....keyN
func lookupJSONStringFields(genericData map[string]interface{}) map[string]string {
	var result = make(map[string]string)
	for key, genericValue := range genericData {
		switch castValue := genericValue.(type) {
		case map[string]interface{}:
			nestedFields := lookupJSONStringFields(castValue)
			for nestedKey, value := range nestedFields {
				result[fmt.Sprintf("%s.%s", key, nestedKey)] = value
			}
		case string:
			result[key] = castValue
		}
	}
	return result
}

// setField sets a field in a generic object.
func setJSONValue(genericData map[string]interface{}, path []string, value interface{}) error {
	currentData := genericData
	for index, key := range path {
		if index == len(path)-1 {
			currentData[key] = value
		} else {
			if currentData[key] == nil {
				currentData[key] = make(map[string]interface{})
			}
			var isMap bool
			currentData, isMap = currentData[key].(map[string]interface{})
			if !isMap {
				return fmt.Errorf("Invalid type for %v", path[0:index+1])
			}
		}
	}
	return nil
}

// incrementGeneration increments the generation field in an object's metadata.
func incrementGeneration(genericData map[string]interface{}) (float64, error) {
	generationValue, err := lookupJSONValue(genericData, []string{"metadata", "generation"}, float64(0))
	if err != nil {
		return 0, err
	}
	generationNumber, isNumber := generationValue.(float64)
	if !isNumber {
		return 0, fmt.Errorf("Invalid metadata generation type")
	}
	generationNumber++
	err = setJSONValue(genericData, []string{"metadata", "generation"}, generationNumber)
	if err != nil {
		return 0, err
	}
	return generationNumber, nil
}

// setGeneration sets the generation field in an object's metadata.
func setGeneration(genericData map[string]interface{}, generation float64) error {
	return setJSONValue(genericData, []string{"metadata", "generation"}, generation)
}

// setUID sets the UID in the object metadata.
func setUID(genericData map[string]interface{}) error {
	return setJSONValue(genericData, []string{"metadata", "uid"}, uuid.NewUUID())
}

// generateIP generates a unique IP address.
func (client *MockClient) generateIP() string {
	client.ipCounter++
	return fmt.Sprintf("192.168.%d.%d", client.ipCounter/256, client.ipCounter%256)
}

// checkPresence checks the presence of an object in the data.
func (client *MockClient) checkPresence(kindKey string, objectKey string) error {
	client.fillInMaps(kindKey)
	if client.data[kindKey][objectKey] == nil {
		return &k8serrors.StatusError{ErrStatus: metav1.Status{
			Status:  "Failure",
			Message: "Not Found",
			Code:    404,
			Reason:  metav1.StatusReasonNotFound,
		}}
	}
	return nil
}

// Create creates a new object
func (client *MockClient) Create(context ctx.Context, object ctrlClient.Object, options ...ctrlClient.CreateOption) error {
	object.SetCreationTimestamp(metav1.Time{Time: time.Now()})

	jsonData, err := json.Marshal(object)
	if err != nil {
		return err
	}

	kindKey, err := buildKindKey(object)
	if err != nil {
		return err
	}

	objectKey, err := buildJSONObjectKey(jsonData)
	if err != nil {
		return err
	}

	genericObject := make(map[string]interface{})
	err = json.Unmarshal(jsonData, &genericObject)
	if err != nil {
		return err
	}

	_, err = incrementGeneration(genericObject)
	if err != nil {
		return err
	}

	err = setUID(genericObject)
	if err != nil {
		return err
	}

	if kindKey == "/v1/Service" {
		clusterIP, err := lookupJSONString(genericObject, "spec", "clusterIP")
		if err != nil {
			return err
		}

		if clusterIP == "" {
			err = setJSONValue(genericObject, []string{"spec", "clusterIP"}, client.generateIP())
			if err != nil {
				return err
			}
		}
	} else if kindKey == "/v1/Pod" {
		err = setJSONValue(genericObject, []string{"status", "podIP"}, generatePodIP(object.GetLabels()))
		if err != nil {
			return err
		}
	}

	client.fillInMaps(kindKey)
	if client.data[kindKey][objectKey] != nil {
		return &k8serrors.StatusError{ErrStatus: metav1.Status{
			Status:  "Failure",
			Message: "Conflict",
			Code:    409,
			Reason:  metav1.StatusReasonAlreadyExists,
		}}
	}
	client.data[kindKey][objectKey] = genericObject

	jsonData, err = json.Marshal(genericObject)
	if err != nil {
		return err
	}
	err = json.Unmarshal(jsonData, object)
	if err != nil {
		return err
	}

	return nil
}

// Get retrieves an object.
func (client *MockClient) Get(context ctx.Context, key ctrlClient.ObjectKey, object ctrlClient.Object) error {
	kindKey, err := buildKindKey(object)
	if err != nil {
		return err
	}

	client.fillInMaps(kindKey)
	objectKey := buildObjectKey(key)
	err = client.checkPresence(kindKey, objectKey)
	if err != nil {
		return err
	}

	jsonData, err := json.Marshal(client.data[kindKey][objectKey])
	if err != nil {
		return err
	}

	err = json.Unmarshal(jsonData, object)

	return err
}

// List lists objects.
func (client *MockClient) List(context ctx.Context, list ctrlClient.ObjectList, options ...ctrlClient.ListOption) error {
	kindKey, err := buildKindKey(list)
	if err != nil {
		return err
	}
	kindKey = strings.TrimSuffix(kindKey, "List")

	objects := make([]map[string]interface{}, 0)

	fullListOptions := &ctrlClient.ListOptions{}
	for _, option := range options {
		option.ApplyToList(fullListOptions)
	}

	client.fillInMaps(kindKey)
	for _, object := range client.data[kindKey] {
		if fullListOptions.Namespace != "" {
			namespace, err := lookupJSONString(object, "metadata", "namespace")
			if err != nil {
				return err
			}
			if namespace != fullListOptions.Namespace {
				continue
			}
		}

		if fullListOptions.LabelSelector != nil {
			objectLabels, err := lookupJSONStringMap(object, "metadata", "labels")
			if err != nil {
				return err
			}
			if !fullListOptions.LabelSelector.Matches(labels.Set(objectLabels)) {
				continue
			}
		}

		if fullListOptions.FieldSelector != nil {
			objectFields := lookupJSONStringFields(object)
			if !fullListOptions.FieldSelector.Matches(fields.Set(objectFields)) {
				continue
			}
		}

		objects = append(objects, object)
		if fullListOptions.Limit > 0 && len(objects) >= int(fullListOptions.Limit) {
			break
		}
	}

	listJSON, err := json.Marshal(objects)
	if err != nil {
		return err
	}
	fullJSON := fmt.Sprintf("{\"items\":%s}", listJSON)
	err = json.Unmarshal([]byte(fullJSON), list)
	if err != nil {
		return err
	}

	return nil
}

// Delete deletes an object.
// This does not support the options argument yet.
func (client *MockClient) Delete(context ctx.Context, object ctrlClient.Object, options ...ctrlClient.DeleteOption) error {
	kindKey, err := buildKindKey(object)
	if err != nil {
		return err
	}

	client.fillInMaps(kindKey)

	objectKey, err := buildRuntimeObjectKey(object)
	if err != nil {
		return err
	}

	err = client.checkPresence(kindKey, objectKey)
	if err != nil {
		return err
	}

	stuckTerminating := client.stuckTerminatingObjects != nil && client.stuckTerminatingObjects[kindKey] != nil && client.stuckTerminatingObjects[kindKey][objectKey]
	if !stuckTerminating {
		delete(client.data[kindKey], objectKey)
	}

	return nil
}

// Update updates an object.
// This does not support the options argument yet.
func (client *MockClient) Update(context ctx.Context, object ctrlClient.Object, options ...ctrlClient.UpdateOption) error {
	kindKey, err := buildKindKey(object)
	if err != nil {
		return err
	}

	client.fillInMaps(kindKey)

	jsonData, err := json.Marshal(object)
	if err != nil {
		return err
	}

	objectKey, err := buildJSONObjectKey(jsonData)
	if err != nil {
		return err
	}

	err = client.checkPresence(kindKey, objectKey)
	if err != nil {
		return err
	}

	existingObject := client.data[kindKey][objectKey]

	newObject := make(map[string]interface{})
	err = json.Unmarshal(jsonData, &newObject)
	if err != nil {
		return err
	}

	if !reflect.DeepEqual(existingObject["spec"], newObject["spec"]) {
		generation, err := incrementGeneration(existingObject)
		if err != nil {
			return err
		}

		err = setGeneration(newObject, generation)
		if err != nil {
			return err
		}
	}

	client.data[kindKey][objectKey] = newObject

	jsonData, err = json.Marshal(newObject)
	if err != nil {
		return err
	}
	err = json.Unmarshal(jsonData, object)
	if err != nil {
		return err
	}

	return nil
}

// Patch patches an object.
// This is not yet implemented.
func (client *MockClient) Patch(context ctx.Context, object ctrlClient.Object, patch ctrlClient.Patch, options ...ctrlClient.PatchOption) error {
	return fmt.Errorf("Not implemented")
}

// DeleteAllOf deletes all objects of the given type matching the given options.
// This is not yet implemented.
func (client *MockClient) DeleteAllOf(context ctx.Context, object ctrlClient.Object, options ...ctrlClient.DeleteAllOfOption) error {
	return fmt.Errorf("Not implemented")
}

// MockStuckTermination sets a flag determining whether an object should get stuck in terminating when it is deleted.
func (client *MockClient) MockStuckTermination(object ctrlClient.Object, terminating bool) error {
	kindKey, err := buildKindKey(object)
	if err != nil {
		return err
	}

	if terminating {
		object.SetDeletionTimestamp(&metav1.Time{Time: time.Now()})
	} else {
		object.SetDeletionTimestamp(nil)
	}

	// We have to update the state in the mock client
	err = client.Update(context.Background(), object)
	if err != nil {
		return err
	}

	objectKey, err := buildRuntimeObjectKey(object)
	if err != nil {
		return err
	}

	if client.stuckTerminatingObjects == nil {
		client.stuckTerminatingObjects = make(map[string]map[string]bool)
	}

	if client.stuckTerminatingObjects[kindKey] == nil {
		client.stuckTerminatingObjects[kindKey] = make(map[string]bool)
	}

	client.stuckTerminatingObjects[kindKey][objectKey] = terminating
	return nil
}

// MockStatusClient wraps a client to provide specialized operations for
// updating status.
type MockStatusClient struct {
	rawClient *MockClient
}

// Update updates an object.
// This does not support the options argument yet.
func (client MockStatusClient) Update(context ctx.Context, object ctrlClient.Object, options ...ctrlClient.UpdateOption) error {
	kindKey, err := buildKindKey(object)
	if err != nil {
		return err
	}

	client.rawClient.fillInMaps(kindKey)

	jsonData, err := json.Marshal(object)
	if err != nil {
		return err
	}

	objectKey, err := buildJSONObjectKey(jsonData)
	if err != nil {
		return err
	}

	err = client.rawClient.checkPresence(kindKey, objectKey)
	if err != nil {
		return err
	}

	existingObject := client.rawClient.data[kindKey][objectKey]

	newObject := make(map[string]interface{})
	err = json.Unmarshal(jsonData, &newObject)
	if err != nil {
		return err
	}

	existingObject["status"] = newObject["status"]
	client.rawClient.data[kindKey][objectKey] = existingObject

	return nil
}

// Patch patches an object's status.
// This is not yet implemented.
func (client MockStatusClient) Patch(context ctx.Context, object ctrlClient.Object, patch ctrlClient.Patch, options ...ctrlClient.PatchOption) error {
	return fmt.Errorf("not implemented")
}

// Status returns a writer for updating status.
func (client *MockClient) Status() ctrlClient.StatusWriter {
	return MockStatusClient{rawClient: client}
}

func (client *MockClient) createEvent(event *corev1.Event) {
	err := client.Create(context.TODO(), event)
	if err != nil {
		panic(err)
	}
}

func buildEvent(object runtime.Object, eventType string, reason string, message string) *corev1.Event {
	jsonData, err := json.Marshal(object)
	if err != nil {
		panic(err)
	}
	genericObject := make(map[string]interface{})
	err = json.Unmarshal(jsonData, &genericObject)
	if err != nil {
		panic(err)
	}
	namespace, err := lookupJSONString(genericObject, "metadata", "namespace")
	if err != nil {
		panic(err)
	}
	name, err := lookupJSONString(genericObject, "metadata", "name")
	if err != nil {
		panic(err)
	}
	uid, err := lookupJSONString(genericObject, "metadata", "uid")
	if err != nil {
		panic(err)
	}

	eventName := fmt.Sprintf("%s-%v", eventType, uuid.NewUUID())

	return &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      eventName,
		},
		InvolvedObject: corev1.ObjectReference{
			Namespace: namespace,
			Name:      name,
			UID:       types.UID(uid),
		},
		Type:      eventType,
		Message:   message,
		Reason:    reason,
		EventTime: metav1.MicroTime{Time: time.Now()},
	}
}

// Event sends an event
func (client *MockClient) Event(object runtime.Object, eventType string, reason string, message string) {
	client.createEvent(buildEvent(object, eventType, reason, message))
}

// Eventf is just like Event, but with Sprintf for the message field.
func (client *MockClient) Eventf(object runtime.Object, eventType string, reason string, messageFormat string, args ...interface{}) {
	client.createEvent(buildEvent(object, eventType, reason, fmt.Sprintf(messageFormat, args...)))
}

// PastEventf is just like Eventf, but with an option to specify the event's 'timestamp' field.
func (client *MockClient) PastEventf(object runtime.Object, timestamp metav1.Time, eventType string, reason string, messageFormat string, args ...interface{}) {
	event := buildEvent(object, eventType, reason, fmt.Sprintf(messageFormat, args...))
	event.EventTime = metav1.MicroTime(timestamp)
	client.createEvent(event)
}

// AnnotatedEventf is just like eventf, but with annotations attached
func (client *MockClient) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventType string, reason string, messageFormat string, args ...interface{}) {
	event := buildEvent(object, eventType, reason, fmt.Sprintf(messageFormat, args...))
	event.ObjectMeta.Annotations = annotations
	client.createEvent(event)
}

// SetPodIntoFailed sets a Pod into a failed status with the given reason
func (client *MockClient) SetPodIntoFailed(context ctx.Context, object ctrlClient.Object, reason string) error {
	data, err := json.Marshal(object)
	if err != nil {
		return err
	}

	pod := &corev1.Pod{}
	err = json.Unmarshal(data, pod)
	if err != nil {
		return err
	}

	pod.Status.Phase = corev1.PodFailed
	pod.Status.Reason = reason
	pod.CreationTimestamp = metav1.Time{Time: time.Now().Add(-30 * time.Minute)}

	return client.Update(context, pod)
}

// RemovePodIP sets the IP address of the Pod to an empty string
func (client *MockClient) RemovePodIP(pod *corev1.Pod) error {
	pod.Status.PodIP = ""

	return client.Update(ctx.TODO(), pod)
}

// generatePodIP generates a mock IP for Pods
func generatePodIP(labels map[string]string) string {
	instanceID, ok := labels[fdbtypes.FDBInstanceIDLabel]
	if !ok {
		return ""
	}

	components := strings.Split(instanceID, "-")
	for index, class := range fdbtypes.ProcessClasses {
		if string(class) == components[len(components)-2] {
			return fmt.Sprintf("1.1.%d.%s", index, components[len(components)-1])
		}
	}

	return "0.0.0.0"
}
