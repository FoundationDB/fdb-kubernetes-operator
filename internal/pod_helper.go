/*
 * pod_helper.go
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

package internal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strconv"

	"k8s.io/utils/pointer"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var processGroupIDRegex = regexp.MustCompile(`^([\w-]+)-(\d+)`)

// GetPublicIPsForPod returns the public IPs for a Pod
func GetPublicIPsForPod(pod *corev1.Pod) []string {
	var podIPFamily *int

	if pod == nil {
		return []string{}
	}

	for _, container := range pod.Spec.Containers {
		if container.Name != "foundationdb-kubernetes-sidecar" {
			continue
		}
		for indexOfArgument, argument := range container.Args {
			if argument == "--public-ip-family" && indexOfArgument < len(container.Args)-1 {
				familyString := container.Args[indexOfArgument+1]
				family, err := strconv.Atoi(familyString)
				if err != nil {
					log.Error(err, "Error parsing public IP family", "family", familyString)
					return nil
				}
				podIPFamily = &family
				break
			}
		}
	}

	if podIPFamily != nil {
		podIPs := pod.Status.PodIPs
		matchingIPs := make([]string, 0, len(podIPs))

		for _, podIP := range podIPs {
			ip := net.ParseIP(podIP.IP)
			if ip == nil {
				log.Error(nil, "Failed to parse IP from pod", "ip", podIP)
				continue
			}
			matches := false
			switch *podIPFamily {
			case 4:
				matches = ip.To4() != nil
			case 6:
				matches = ip.To4() == nil
			default:
				log.Error(nil, "Could not match IP address against IP family", "family", *podIPFamily)
			}
			if matches {
				matchingIPs = append(matchingIPs, podIP.IP)
			}
		}
		return matchingIPs
	}

	return []string{pod.Status.PodIP}
}

// GetProcessGroupIDFromMeta fetches the process group ID from an object's metadata.
func GetProcessGroupIDFromMeta(cluster *fdbtypes.FoundationDBCluster, metadata metav1.ObjectMeta) string {
	return metadata.Labels[cluster.GetProcessGroupIDLabel()]
}

// GetPodSpecHash builds the hash of the expected spec for a pod.
func GetPodSpecHash(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, id int, spec *corev1.PodSpec) (string, error) {
	var err error
	if spec == nil {
		spec, err = GetPodSpec(cluster, processClass, id)
		if err != nil {
			return "", err
		}
	}

	return GetJSONHash(spec)
}

// GetJSONHash serializes an object to JSON and takes a hash of the resulting
// JSON.
func GetJSONHash(object interface{}) (string, error) {
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	err := encoder.Encode(object)
	if err != nil {
		return "", err
	}
	specHash := hash.Sum(make([]byte, 0))
	return hex.EncodeToString(specHash), nil
}

// GetPodLabels creates the labels that we will apply to a Pod
func GetPodLabels(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, id string) map[string]string {
	labels := map[string]string{}

	for key, value := range cluster.Spec.LabelConfig.MatchLabels {
		labels[key] = value
	}

	if processClass != "" {
		for _, label := range cluster.Spec.LabelConfig.ProcessClassLabels {
			labels[label] = string(processClass)
		}
	}

	if id != "" {
		for _, label := range cluster.Spec.LabelConfig.ProcessGroupIDLabels {
			labels[label] = id
		}
	}

	return labels
}

// GetPodMatchLabels creates the labels that we will use when filtering for a pod.
func GetPodMatchLabels(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, id string) map[string]string {
	labels := map[string]string{}

	for key, value := range cluster.Spec.LabelConfig.MatchLabels {
		labels[key] = value
	}

	if processClass != "" {
		labels[cluster.GetProcessClassLabel()] = string(processClass)
	}

	if id != "" {
		labels[cluster.GetProcessGroupIDLabel()] = id
	}

	return labels
}

// BuildOwnerReference returns an OwnerReference for the provided input
func BuildOwnerReference(ownerType metav1.TypeMeta, ownerMetadata metav1.ObjectMeta) []metav1.OwnerReference {
	return []metav1.OwnerReference{{
		APIVersion: ownerType.APIVersion,
		Kind:       ownerType.Kind,
		Name:       ownerMetadata.Name,
		UID:        ownerMetadata.UID,
		Controller: pointer.Bool(true),
	}}
}

// GetSinglePodListOptions returns the listOptions to list a single Pod
func GetSinglePodListOptions(cluster *fdbtypes.FoundationDBCluster, processGroupID string) []client.ListOption {
	return []client.ListOption{client.InNamespace(cluster.ObjectMeta.Namespace), client.MatchingLabels(GetPodMatchLabels(cluster, "", processGroupID))}
}

// GetPodListOptions returns the listOptions to list Pods
func GetPodListOptions(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, id string) []client.ListOption {
	return []client.ListOption{client.InNamespace(cluster.ObjectMeta.Namespace), client.MatchingLabels(GetPodMatchLabels(cluster, processClass, id))}
}

// GetPvcMetadata returns the metadata for a PVC
func GetPvcMetadata(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, id string) metav1.ObjectMeta {
	var customMetadata *metav1.ObjectMeta

	processSettings := cluster.GetProcessSettings(processClass)
	if processSettings.VolumeClaimTemplate != nil {
		customMetadata = &processSettings.VolumeClaimTemplate.ObjectMeta
	} else {
		customMetadata = nil
	}
	return GetObjectMetadata(cluster, customMetadata, processClass, id)
}

// GetSidecarImage returns the expected sidecar image for a specific process class
func GetSidecarImage(cluster *fdbtypes.FoundationDBCluster, pClass fdbtypes.ProcessClass) (string, error) {
	settings := cluster.GetProcessSettings(pClass)

	image := ""
	if settings.PodTemplate != nil {
		for _, container := range settings.PodTemplate.Spec.Containers {
			if container.Name == "foundationdb-kubernetes-sidecar" && container.Image != "" {
				image = container.Image
			}
		}
	}

	var imageConfigs []fdbtypes.ImageConfig
	if pointer.BoolDeref(cluster.Spec.UseUnifiedImage, false) {
		imageConfigs = cluster.Spec.MainContainer.ImageConfigs
	} else {
		imageConfigs = cluster.Spec.SidecarContainer.ImageConfigs
	}

	return GetImage(image, imageConfigs, cluster.Spec.Version, settings.GetAllowTagOverride())
}

// CreatePodMap creates a map with the process group ID as a key and the according Pod as a value
func CreatePodMap(cluster *fdbtypes.FoundationDBCluster, pods []*corev1.Pod) map[string]*corev1.Pod {
	podProcessGroupMap := make(map[string]*corev1.Pod, len(pods))
	for _, pod := range pods {
		processGroupID := GetProcessGroupIDFromMeta(cluster, pod.ObjectMeta)
		if processGroupID == "" {
			continue
		}
		podProcessGroupMap[processGroupID] = pod
	}

	return podProcessGroupMap
}

// ParseProcessGroupID extracts the components of an process group ID.
func ParseProcessGroupID(id string) (fdbtypes.ProcessClass, int, error) {
	result := processGroupIDRegex.FindStringSubmatch(id)
	if result == nil {
		return "", 0, fmt.Errorf("could not parse process group ID %s", id)
	}
	prefix := result[1]
	number, err := strconv.Atoi(result[2])
	if err != nil {
		return "", 0, err
	}
	return fdbtypes.ProcessClass(prefix), number, nil
}

// GetPublicIPSource determines how a Pod has gotten its public IP.
func GetPublicIPSource(pod *corev1.Pod) (fdbtypes.PublicIPSource, error) {
	if pod == nil {
		return "", fmt.Errorf("failed to fetch public IP source from nil Pod")
	}

	source := pod.ObjectMeta.Annotations[fdbtypes.PublicIPSourceAnnotation]
	if source == "" {
		return fdbtypes.PublicIPSourcePod, nil
	}
	return fdbtypes.PublicIPSource(source), nil
}
