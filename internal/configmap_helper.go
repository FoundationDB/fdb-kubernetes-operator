/*
 * configmap_helper.go
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
	"encoding/json"
	"fmt"
	"strings"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ClusterFileKey defines the key name in the ConfigMap
	ClusterFileKey = "cluster-file"
)

// GetConfigMap builds a config map for a cluster's dynamic config
func GetConfigMap(cluster *fdbv1beta2.FoundationDBCluster) (*corev1.ConfigMap, error) {
	data := make(map[string]string)

	connectionString := cluster.Status.ConnectionString
	data[ClusterFileKey] = connectionString
	data["running-version"] = cluster.Status.RunningVersion

	var caFile strings.Builder
	for _, ca := range cluster.Spec.TrustedCAs {
		if caFile.Len() > 0 {
			caFile.WriteString("\n")
		}
		caFile.WriteString(ca)
	}

	if caFile.Len() > 0 {
		data["ca-file"] = caFile.String()
	}

	desiredCountStruct, err := cluster.GetProcessCountsWithDefaults()
	if err != nil {
		return nil, err
	}
	desiredCounts := desiredCountStruct.Map()

	imageTypes := make(map[FDBImageType]fdbv1beta2.None, len(cluster.Status.ImageTypes))
	for _, imageType := range cluster.Status.ImageTypes {
		imageTypes[FDBImageType(imageType)] = fdbv1beta2.None{}
	}

	for processClass, count := range desiredCounts {
		if count == 0 {
			continue
		}

		serversPerPodSlice := []int{1}
		if processClass == fdbv1beta2.ProcessClassStorage {
			// If the status field is not initialized we fallback to only the specified count
			// in the cluster spec. This should only happen in the initial phase of a new cluster.
			if len(cluster.Status.StorageServersPerDisk) == 0 {
				serversPerPodSlice = []int{cluster.GetDesiredServersPerPod(processClass)}
			} else {
				serversPerPodSlice = cluster.Status.StorageServersPerDisk
			}
		}

		if processClass.SupportsMultipleLogServers() {
			// If the status field is not initialized we fallback to only the specified count
			// in the cluster spec. This should only happen in the initial phase of a new cluster.
			if len(cluster.Status.LogServersPerDisk) == 0 {
				serversPerPodSlice = []int{cluster.GetDesiredServersPerPod(processClass)}
			} else {
				serversPerPodSlice = cluster.Status.LogServersPerDisk
			}
		}

		if _, useUnifiedImage := imageTypes[FDBImageTypeUnified]; useUnifiedImage {
			for _, serversPerPod := range serversPerPodSlice {
				filename, jsonData, err := getDataForMonitorConf(cluster, FDBImageTypeUnified, processClass, serversPerPod)
				if err != nil {
					return nil, err
				}
				data[filename] = string(jsonData)
			}
		}

		if _, useSplitImage := imageTypes[FDBImageTypeSplit]; useSplitImage {
			for _, serversPerPod := range serversPerPodSlice {
				err := setMonitorConfForFilename(cluster, data, GetConfigMapMonitorConfEntry(processClass, FDBImageTypeSplit, serversPerPod), connectionString, processClass, serversPerPod)
				if err != nil {
					return nil, err
				}
			}
		}
	}

	if cluster.Spec.ConfigMap != nil {
		for k, v := range cluster.Spec.ConfigMap.Data {
			data[k] = v
		}
	}

	metadata := getConfigMapMetadata(cluster)
	metadata.OwnerReferences = BuildOwnerReference(cluster.TypeMeta, cluster.ObjectMeta)

	return &corev1.ConfigMap{
		ObjectMeta: metadata,
		Data:       data,
	}, nil
}

func getConfigMapMetadata(cluster *fdbv1beta2.FoundationDBCluster) metav1.ObjectMeta {
	var metadata metav1.ObjectMeta
	if cluster.Spec.ConfigMap != nil {
		metadata = GetObjectMetadata(cluster, &cluster.Spec.ConfigMap.ObjectMeta, "", "")
	} else {
		metadata = GetObjectMetadata(cluster, nil, "", "")
	}

	if metadata.Name == "" {
		metadata.Name = fmt.Sprintf("%s-config", cluster.Name)
	} else {
		metadata.Name = fmt.Sprintf("%s-%s", cluster.Name, metadata.Name)
	}

	return metadata
}

func getDataForMonitorConf(cluster *fdbv1beta2.FoundationDBCluster, imageType FDBImageType, pClass fdbv1beta2.ProcessClass, serversPerPod int) (string, []byte, error) {
	config, err := GetMonitorProcessConfiguration(cluster, pClass, serversPerPod, imageType, nil)
	if err != nil {
		return "", nil, err
	}
	jsonData, err := json.Marshal(config)
	if err != nil {
		return "", nil, err
	}
	filename := GetConfigMapMonitorConfEntry(pClass, imageType, serversPerPod)
	return filename, jsonData, nil
}

func setMonitorConfForFilename(cluster *fdbv1beta2.FoundationDBCluster, data map[string]string, filename string, connectionString string, processClass fdbv1beta2.ProcessClass, serversPerPod int) error {
	if connectionString == "" {
		data[filename] = ""
	} else {
		conf, err := GetMonitorConf(cluster, processClass, nil, serversPerPod)
		if err != nil {
			return err
		}
		data[filename] = conf
	}

	return nil
}

// GetConfigMapMonitorConfEntry returns the specific key for the monitor conf in the ConfigMap
func GetConfigMapMonitorConfEntry(pClass fdbv1beta2.ProcessClass, imageType FDBImageType, serversPerPod int) string {
	if imageType == FDBImageTypeUnified {
		if serversPerPod > 1 {
			return fmt.Sprintf("fdbmonitor-conf-%s-json-multiple", pClass)
		}
		return fmt.Sprintf("fdbmonitor-conf-%s-json", pClass)
	}
	if serversPerPod > 1 {
		return fmt.Sprintf("fdbmonitor-conf-%s-density-%d", pClass, serversPerPod)
	}
	return fmt.Sprintf("fdbmonitor-conf-%s", pClass)
}

// GetDynamicConfHash gets a hash of the data from the config map holding the
// cluster's dynamic conf.
//
// This will omit keys that we do not expect the Pods to reference e.g. for storage Pods only include the storage config.
func GetDynamicConfHash(configMap *corev1.ConfigMap, pClass fdbv1beta2.ProcessClass, imageType FDBImageType, serversPerPod int) (string, error) {
	fields := []string{
		ClusterFileKey,
		GetConfigMapMonitorConfEntry(pClass, imageType, serversPerPod),
		"running-version",
		"ca-file",
		"sidecar-conf",
	}
	var data = make(map[string]string, len(fields))

	for _, field := range fields {
		if val, ok := configMap.Data[field]; ok {
			data[field] = val
		}
	}

	return GetJSONHash(data)
}
