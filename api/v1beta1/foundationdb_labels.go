/*
 * foundationdb_labels.go
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

package v1beta1

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

	// PublicIPSourceAnnotation is an annotation key that specifies where a pod
	// gets its public IP from.
	PublicIPSourceAnnotation = "foundationdb.org/public-ip-source"

	// PublicIPAnnotation is an annotation key that specifies the current public
	// IP for a pod.
	PublicIPAnnotation = "foundationdb.org/public-ip"

	// FDBInstanceIDLabel represents the label that is used to represent a instance ID
	FDBInstanceIDLabel = "fdb-instance-id"

	// FDBProcessClassLabel represents the label that is used to represent the process class
	FDBProcessClassLabel = "fdb-process-class"

	// FDBClusterLabel represents the label that is used to represent the cluster of an instance
	FDBClusterLabel = "fdb-cluster-name"

	// NodeSelectorNoScheduleLabel is a label used when adding node selectors to block scheduling.
	NodeSelectorNoScheduleLabel = "foundationdb.org/no-schedule-allowed"

	// FDBLocalityInstanceIDKey represents the key in the locality map that
	// holds the instance ID.
	FDBLocalityInstanceIDKey = "instance_id"

	// FDBLocalityZoneIDKey represents the key in the locality map that holds
	// the zone ID.
	FDBLocalityZoneIDKey = "zoneid"

	// FDBLocalityDCIDKey represents the key in the locality map that holds
	// the DC ID.
	FDBLocalityDCIDKey = "dcid"
)
