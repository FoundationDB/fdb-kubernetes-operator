package internal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"strconv"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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

// GetInstanceIDFromMeta fetches the instance ID from an object's metadata.
func GetInstanceIDFromMeta(metadata metav1.ObjectMeta) string {
	return metadata.Labels[fdbtypes.FDBInstanceIDLabel]
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

// GetMinimalPodLabels creates the minimal required labels for a Pod
func GetMinimalPodLabels(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, id string) map[string]string {
	labels := map[string]string{}

	for key, value := range cluster.Spec.LabelConfig.MatchLabels {
		labels[key] = value
	}

	if processClass != "" {
		labels[fdbtypes.FDBProcessClassLabel] = string(processClass)
	}

	if id != "" {
		labels[fdbtypes.FDBInstanceIDLabel] = id
	}

	return labels
}

// BuildOwnerReference returns an OwnerReference for the provided input
func BuildOwnerReference(ownerType metav1.TypeMeta, ownerMetadata metav1.ObjectMeta) []metav1.OwnerReference {
	var isController = true
	return []metav1.OwnerReference{{
		APIVersion: ownerType.APIVersion,
		Kind:       ownerType.Kind,
		Name:       ownerMetadata.Name,
		UID:        ownerMetadata.UID,
		Controller: &isController,
	}}
}

// GetSinglePodListOptions returns the listOptions to list a single Pod
func GetSinglePodListOptions(cluster *fdbtypes.FoundationDBCluster, instanceID string) []client.ListOption {
	return []client.ListOption{client.InNamespace(cluster.ObjectMeta.Namespace), client.MatchingLabels(GetMinimalSinglePodLabels(cluster, instanceID))}
}

// GetPodListOptions returns the listOptions to list Pods
func GetPodListOptions(cluster *fdbtypes.FoundationDBCluster, processClass fdbtypes.ProcessClass, id string) []client.ListOption {
	return []client.ListOption{client.InNamespace(cluster.ObjectMeta.Namespace), client.MatchingLabels(GetMinimalPodLabels(cluster, processClass, id))}
}

// GetMinimalSinglePodLabels returns the minimal listOptions to list Pods
func GetMinimalSinglePodLabels(cluster *fdbtypes.FoundationDBCluster, id string) map[string]string {
	return GetMinimalPodLabels(cluster, "", id)
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

	return GetImage(image, cluster.Spec.SidecarContainer.ImageConfigs, cluster.Spec.Version, settings.GetAllowTagOverride())
}
