/*
 * test_helper.go
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
	"time"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CreateDefaultCluster creates a default FoundationDBCluster for testing
func CreateDefaultCluster() *fdbtypes.FoundationDBCluster {
	trueValue := true
	failureDetectionWindow := 1

	return &fdbtypes.FoundationDBCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "operator-test-1",
			Namespace: "my-ns",
		},
		Spec: fdbtypes.FoundationDBClusterSpec{
			Version: fdbtypes.Versions.Default.String(),
			ProcessCounts: fdbtypes.ProcessCounts{
				Storage:           4,
				ClusterController: 1,
			},
			FaultDomain: fdbtypes.FoundationDBClusterFaultDomain{
				Key: "foundationdb.org/none",
			},
			AutomationOptions: fdbtypes.FoundationDBClusterAutomationOptions{
				Replacements: fdbtypes.AutomaticReplacementOptions{
					Enabled:                     &trueValue,
					FailureDetectionTimeSeconds: &failureDetectionWindow,
				},
				WaitDurationBetweenRemovals: 1 * time.Nanosecond,
			},
			MinimumUptimeSecondsForBounce: 1,
		},
		Status: fdbtypes.FoundationDBClusterStatus{
			RequiredAddresses: fdbtypes.RequiredAddressSet{
				NonTLS: true,
			},
			ProcessGroups: make([]*fdbtypes.ProcessGroupStatus, 0),
		},
	}
}

// GetEnvVars returns a HashMap of EnvVars for the container
func GetEnvVars(container v1.Container) map[string]*v1.EnvVar {
	results := make(map[string]*v1.EnvVar)
	for index, env := range container.Env {
		results[env.Name] = &container.Env[index]
	}

	return results
}

// CreateDefaultBackup creates a defaultFoundationDBCluster for testing
func CreateDefaultBackup(cluster *fdbtypes.FoundationDBCluster) *fdbtypes.FoundationDBBackup {
	agentCount := 3
	return &fdbtypes.FoundationDBBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		},
		Spec: fdbtypes.FoundationDBBackupSpec{
			BackupState: fdbtypes.BackupStateRunning,
			Version:     cluster.Spec.Version,
			ClusterName: cluster.Name,
			AgentCount:  &agentCount,
			BlobStoreConfiguration: &fdbtypes.BlobStoreConfiguration{
				AccountName: "test@test-service",
				BackupName:  "test-backup",
			},
		},
		Status: fdbtypes.FoundationDBBackupStatus{},
	}
}
