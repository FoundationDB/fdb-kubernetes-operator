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
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"math/rand"
	"strings"
)

// CreateDefaultCluster creates a default FoundationDBCluster for testing
func CreateDefaultCluster() *fdbv1beta2.FoundationDBCluster {
	return &fdbv1beta2.FoundationDBCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "operator-test-1",
			Namespace: "my-ns",
		},
		Spec: fdbv1beta2.FoundationDBClusterSpec{
			Version: fdbv1beta2.Versions.Default.String(),
			ProcessCounts: fdbv1beta2.ProcessCounts{
				Storage:           4,
				ClusterController: 1,
			},
			FaultDomain: fdbv1beta2.FoundationDBClusterFaultDomain{
				Key: fdbv1beta2.NoneFaultDomainKey,
			},
			AutomationOptions: fdbv1beta2.FoundationDBClusterAutomationOptions{
				Replacements: fdbv1beta2.AutomaticReplacementOptions{
					Enabled:                     pointer.Bool(true),
					FailureDetectionTimeSeconds: pointer.Int(1),
					TaintReplacementTimeSeconds: pointer.Int(1),
				},
				WaitBetweenRemovalsSeconds: pointer.Int(0),
			},
			MinimumUptimeSecondsForBounce: 1,
		},
		Status: fdbv1beta2.FoundationDBClusterStatus{
			RequiredAddresses: fdbv1beta2.RequiredAddressSet{
				NonTLS: true,
			},
			ProcessGroups:  make([]*fdbv1beta2.ProcessGroupStatus, 0),
			RunningVersion: fdbv1beta2.Versions.Default.String(),
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
func CreateDefaultBackup(cluster *fdbv1beta2.FoundationDBCluster) *fdbv1beta2.FoundationDBBackup {
	agentCount := 3
	return &fdbv1beta2.FoundationDBBackup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		},
		Spec: fdbv1beta2.FoundationDBBackupSpec{
			BackupState: fdbv1beta2.BackupStateRunning,
			Version:     cluster.Spec.Version,
			ClusterName: cluster.Name,
			AgentCount:  &agentCount,
			BlobStoreConfiguration: &fdbv1beta2.BlobStoreConfiguration{
				AccountName: "test@test-service",
				BackupName:  "test-backup",
			},
		},
		Status: fdbv1beta2.FoundationDBBackupStatus{},
	}
}

// GetProcessGroup is a helper method that creates a ProcessGroup based on the provided process class and id number.
func GetProcessGroup(cluster *fdbv1beta2.FoundationDBCluster, processClass fdbv1beta2.ProcessClass, idNum int) *fdbv1beta2.ProcessGroupStatus {
	_, processGroupID := cluster.GetProcessGroupID(processClass, idNum)

	return &fdbv1beta2.ProcessGroupStatus{
		ProcessClass:   processClass,
		ProcessGroupID: processGroupID,
	}
}

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// GenerateRandomString can be used to generate a random string with length n
func GenerateRandomString(n int) string {
	var res strings.Builder
	for i := 0; i < n; i++ {
		res.WriteByte(letterBytes[rand.Intn(len(letterBytes))])
	}

	return res.String()
}
