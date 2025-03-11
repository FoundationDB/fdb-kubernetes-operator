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
	monitorapi "github.com/apple/foundationdb/fdbkubernetesmonitor/api"
	"net"
	"strconv"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetPublicIPsForPod returns the public IPs for a Pod
func GetPublicIPsForPod(pod *corev1.Pod, log logr.Logger) []string {
	if pod == nil {
		return []string{}
	}

	podIPFamily, err := GetIPFamilyFromPod(pod)
	if err != nil {
		log.Error(err, "Could not parse IP family")
		return []string{pod.Status.PodIP}
	}

	if podIPFamily == nil {
		return []string{pod.Status.PodIP}
	}

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
		case fdbv1beta2.PodIPFamilyIPv4:
			matches = ip.To4() != nil
		case fdbv1beta2.PodIPFamilyIPv6:
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

// GetProcessGroupIDFromMeta fetches the process group ID from an object's metadata.
func GetProcessGroupIDFromMeta(cluster *fdbv1beta2.FoundationDBCluster, metadata metav1.ObjectMeta) fdbv1beta2.ProcessGroupID {
	return fdbv1beta2.ProcessGroupID(metadata.Labels[cluster.GetProcessGroupIDLabel()])
}

// GetPodSpecHash builds the hash of the expected spec for a pod.
func GetPodSpecHash(cluster *fdbv1beta2.FoundationDBCluster, processGroup *fdbv1beta2.ProcessGroupStatus, spec *corev1.PodSpec) (string, error) {
	var err error
	if spec == nil {
		spec, err = GetPodSpec(cluster, processGroup)
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
func GetPodLabels(cluster *fdbv1beta2.FoundationDBCluster, processClass fdbv1beta2.ProcessClass, id string) map[string]string {
	labels := map[string]string{}

	for key, value := range cluster.GetMatchLabels() {
		labels[key] = value
	}

	if processClass != "" {
		for _, label := range cluster.GetProcessClassLabels() {
			labels[label] = string(processClass)
		}
	}

	if id != "" {
		for _, label := range cluster.GetProcessGroupIDLabels() {
			labels[label] = id
		}
	}

	return labels
}

// GetPodMatchLabels creates the labels that we will use when filtering for a pod.
func GetPodMatchLabels(cluster *fdbv1beta2.FoundationDBCluster, processClass fdbv1beta2.ProcessClass, id string) map[string]string {
	labels := map[string]string{}

	for key, value := range cluster.GetMatchLabels() {
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
func GetSinglePodListOptions(cluster *fdbv1beta2.FoundationDBCluster, processGroupID fdbv1beta2.ProcessGroupID) []client.ListOption {
	return []client.ListOption{client.InNamespace(cluster.ObjectMeta.Namespace), client.MatchingLabels(GetPodMatchLabels(cluster, "", string(processGroupID)))}
}

// GetPodListOptions returns the listOptions to list Pods
func GetPodListOptions(cluster *fdbv1beta2.FoundationDBCluster, processClass fdbv1beta2.ProcessClass, id string) []client.ListOption {
	return []client.ListOption{client.InNamespace(cluster.ObjectMeta.Namespace), client.MatchingLabels(GetPodMatchLabels(cluster, processClass, id))}
}

// GetPvcMetadata returns the metadata for a PVC
func GetPvcMetadata(cluster *fdbv1beta2.FoundationDBCluster, processClass fdbv1beta2.ProcessClass, id fdbv1beta2.ProcessGroupID) metav1.ObjectMeta {
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
func GetSidecarImage(cluster *fdbv1beta2.FoundationDBCluster, pClass fdbv1beta2.ProcessClass) (string, error) {
	settings := cluster.GetProcessSettings(pClass)

	image := ""
	if settings.PodTemplate != nil {
		for _, container := range settings.PodTemplate.Spec.Containers {
			if container.Name == fdbv1beta2.SidecarContainerName && container.Image != "" {
				image = container.Image
			}
		}
	}

	var imageConfigs []fdbv1beta2.ImageConfig
	if cluster.UseUnifiedImage() {
		imageConfigs = cluster.Spec.MainContainer.ImageConfigs
	} else {
		imageConfigs = cluster.Spec.SidecarContainer.ImageConfigs
	}

	return GetImage(image, imageConfigs, cluster.Spec.Version, false)
}

// GetPublicIPSource determines how a Pod has gotten its public IP.
func GetPublicIPSource(pod *corev1.Pod) (fdbv1beta2.PublicIPSource, error) {
	if pod == nil {
		return "", fmt.Errorf("failed to fetch public IP source from nil Pod")
	}

	source := pod.ObjectMeta.Annotations[fdbv1beta2.PublicIPSourceAnnotation]
	if source == "" {
		return fdbv1beta2.PublicIPSourcePod, nil
	}
	return fdbv1beta2.PublicIPSource(source), nil
}

// GetIPFamilyFromPod returns the IP family from the pod configuration.
func GetIPFamilyFromPod(pod *corev1.Pod) (*int, error) {
	if GetImageType(pod) == fdbv1beta2.ImageTypeUnified {
		currentData, present := pod.Annotations[monitorapi.CurrentConfigurationAnnotation]
		if !present {
			return nil, fmt.Errorf("could not read current launcher configruations")
		}

		currentConfiguration := monitorapi.ProcessConfiguration{}
		err := json.Unmarshal([]byte(currentData), &currentConfiguration)
		if err != nil {
			return nil, fmt.Errorf("could not parse current process configuration: %w", err)
		}

		for _, argument := range currentConfiguration.Arguments {
			if argument.ArgumentType != monitorapi.IPListArgumentType {
				continue
			}

			return pointer.Int(argument.IPFamily), nil
		}

		// No IP List setting is defined.
		return nil, nil
	}

	for _, container := range pod.Spec.Containers {
		if container.Name != fdbv1beta2.SidecarContainerName {
			continue
		}

		for indexOfArgument, argument := range container.Args {
			if argument == "--public-ip-family" && indexOfArgument < len(container.Args)-1 {
				familyString := container.Args[indexOfArgument+1]
				family, err := strconv.Atoi(familyString)
				if err == nil {
					return &family, nil
				}
				break
			}
		}
	}

	return nil, nil
}

// GetIPFamily determines the IP family based on the annotation.
// TODO (johscheuer): Make use of this method once we did the next release 2.2.0. This will ensure that the operator
// can add the fdbv1beta2.IPFamilyAnnotation to all pods.
//func GetIPFamily(pod *corev1.Pod) (*int, error) {
//	if pod == nil {
//		return nil, fmt.Errorf("failed to fetch IP family from nil Pod")
//	}
//
//	ipFamilyValue := pod.ObjectMeta.Annotations[fdbv1beta2.IPFamilyAnnotation]
//	if ipFamilyValue != "" {
//		ipFamily, err := strconv.Atoi(ipFamilyValue)
//		if err != nil {
//			return nil, err
//		}
//
//		return pointer.Int(ipFamily), nil
//	}
//
//	return nil, nil
//}

// PodHasSidecarTLS determines whether a pod currently has TLS enabled for the sidecar process.
// This method should only be used for split images.
func PodHasSidecarTLS(pod *corev1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		if container.Name == fdbv1beta2.SidecarContainerName {
			for _, arg := range container.Args {
				if arg == "--tls" {
					return true
				}
			}
		}
	}

	return false
}
