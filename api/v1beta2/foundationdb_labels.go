/*
 * foundationdb_labels.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021-2022 Apple Inc. and the FoundationDB project authors
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

package v1beta2

const (
	// LastSpecKey provides the annotation name we use to store the hash of the
	// pod spec.
	LastSpecKey = "foundationdb.org/last-applied-spec"

	// LastConfigMapKey provides the annotation name we use to store the hash of the
	// config map.
	LastConfigMapKey = "foundationdb.org/last-applied-config-map"

	// OutdatedConfigMapKey provides the annotation name we use to store the
	// timestamp when we saw an outdated config map.
	OutdatedConfigMapKey = "foundationdb.org/outdated-config-map-seen"

	// BackupDeploymentLabel provides the label we use to connect backup
	// deployments to a cluster.
	BackupDeploymentLabel = "foundationdb.org/backup-for"

	// BackupDeploymentPodLabel provides the label to select Pods for a specific Backup deployment.
	BackupDeploymentPodLabel = "foundationdb.org/deployment-name"

	// PublicIPSourceAnnotation is an annotation key that specifies where a pod
	// gets its public IP from.
	PublicIPSourceAnnotation = "foundationdb.org/public-ip-source"

	// PublicIPAnnotation is an annotation key that specifies the current public
	// IP for a pod.
	PublicIPAnnotation = "foundationdb.org/public-ip"

	// IsolateProcessGroupAnnotation is the annotation that defines if the current Pod should be isolated. Isolated
	// process groups will shutdown the fdbserver instance but keep the Pod and other Kubernetes resources running
	// for debugging purpose.
	IsolateProcessGroupAnnotation = "foundationdb.org/isolate-process-group"

	// NodeAnnotation is an annotation key that specifies where a Pod is currently running on.
	// The information is fetched from Pod.Spec.NodeName of the Pod resource.
	NodeAnnotation = "foundationdb.org/current-node"

	// ImageTypeAnnotation is an annotation key that specifies the image type of the Pod.
	ImageTypeAnnotation = "foundationdb.org/image-type"

	// FDBProcessGroupIDLabel represents the label that is used to represent a instance ID
	FDBProcessGroupIDLabel = "foundationdb.org/fdb-process-group-id"

	// FDBProcessClassLabel represents the label that is used to represent the process class
	FDBProcessClassLabel = "foundationdb.org/fdb-process-class"

	// FDBClusterLabel represents the label that is used to represent the cluster of an instance
	FDBClusterLabel = "foundationdb.org/fdb-cluster-name"

	// NodeSelectorNoScheduleLabel is a label used when adding node selectors to block scheduling.
	NodeSelectorNoScheduleLabel = "foundationdb.org/no-schedule-allowed"

	// FDBLocalityInstanceIDKey represents the key in the locality map that
	// holds the instance ID.
	FDBLocalityInstanceIDKey = "instance_id"

	// FDBLocalityZoneIDKey represents the key in the locality map that holds
	// the zone ID.
	FDBLocalityZoneIDKey = "zoneid"

	// FDBLocalityMachineIDKey represents the key in the locality map that holds
	// the machine ID.
	FDBLocalityMachineIDKey = "machineid"

	// FDBLocalityDCIDKey represents the key in the locality map that holds
	// the DC ID.
	FDBLocalityDCIDKey = "dcid"

	// FDBLocalityDNSNameKey represents the key in the locality map that holds
	// the DNS name for the pod.
	FDBLocalityDNSNameKey = "dns_name"

	// FDBLocalityProcessIDKey represents the key in the locality map that
	// holds the process ID.
	FDBLocalityProcessIDKey = "process_id"

	// FDBLocalityExclusionPrefix represents the exclusion prefix for locality based exclusions.
	FDBLocalityExclusionPrefix = "locality_instance_id"

	// FDBLocalityDataHallKey represents the key in the locality map that holds
	// the data hall.
	FDBLocalityDataHallKey = "data_hall"
)
