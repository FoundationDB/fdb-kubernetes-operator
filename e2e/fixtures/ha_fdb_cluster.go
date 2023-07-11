/*
 * ha_fdb_cluster.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2023 Apple Inc. and the FoundationDB project authors
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

package fixtures

import (
	"fmt"
	"log"
	"strings"
	"sync"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// This file contains fixtures to set up HA configurations.
const (
	// PrimaryID is the suffix for the primary FoundationDBCluster
	PrimaryID = "primary"
	// RemoteID is the suffix for the remote FoundationDBCluster
	RemoteID = "remote"
	// PrimarySatelliteID is the suffix for the primary satellite FoundationDBCluster
	PrimarySatelliteID = "primary-satellite"
	// RemoteSatelliteID is the suffix for the remote satellite FoundationDBCluster
	RemoteSatelliteID = "remote-satellite"
	// SatelliteID is the suffix for the satellite FoundationDBCluster
	SatelliteID = "satellite"
)

// HaFdbCluster is a struct around handling HA FounationDBClusters.
type HaFdbCluster struct {
	clusters []*FdbCluster
}

// GetPrimary returns the FoundationDBCluster matching the suffix from PrimaryID.
func (haFDBCluster *HaFdbCluster) GetPrimary() *FdbCluster {
	return haFDBCluster.GetCluster(PrimaryID)
}

// GetRemote returns the FoundationDBCluster matching the suffix from RemoteID.
func (haFDBCluster *HaFdbCluster) GetRemote() *FdbCluster {
	return haFDBCluster.GetCluster(RemoteID)
}

// GetPrimarySatellite returns the FoundationDBCluster matching the suffix from PrimarySatelliteID.
// This function is only valid for HA clusters using 2 satellites.
func (haFDBCluster *HaFdbCluster) GetPrimarySatellite() *FdbCluster {
	if len(haFDBCluster.clusters) == 3 {
		return nil
	}
	return haFDBCluster.GetCluster(PrimarySatelliteID)
}

// GetRemoteSatellite returns the FoundationDBCluster matching the suffix from RemoteSatelliteID.
// This function is only valid for HA clusters using 2 satellites.
func (haFDBCluster *HaFdbCluster) GetRemoteSatellite() *FdbCluster {
	if len(haFDBCluster.clusters) == 3 {
		return nil
	}
	return haFDBCluster.GetCluster(RemoteSatelliteID)
}

// GetSatellite this is only for threeZoneDoubleSat config, where `satellite` is primary satellite for both regions.
func (haFDBCluster *HaFdbCluster) GetSatellite() *FdbCluster {
	if len(haFDBCluster.clusters) == 4 {
		return nil
	}
	return haFDBCluster.GetCluster(SatelliteID)
}

// GetAllClusters Returns all FoundationDBClusters that span this HA cluster.
func (haFDBCluster *HaFdbCluster) GetAllClusters() []*FdbCluster {
	return haFDBCluster.clusters
}

// GetCluster returns the FoundationDBCluster with the provided suffix.
func (haFDBCluster *HaFdbCluster) GetCluster(suffix string) *FdbCluster {
	for _, fdbCluster := range haFDBCluster.clusters {
		if strings.HasSuffix(fdbCluster.Name(), suffix) {
			return fdbCluster
		}
	}

	return nil
}

func (haFDBCluster *HaFdbCluster) addCluster(fdbCluster *FdbCluster) error {
	if haFDBCluster.clusters == nil {
		haFDBCluster.clusters = []*FdbCluster{}
	}
	// Check if the cluster is already in the slice
	if haFDBCluster.GetCluster(fdbCluster.Name()) != nil {
		return haFDBCluster.updateCluster(fdbCluster)
	}
	haFDBCluster.clusters = append(haFDBCluster.clusters, fdbCluster)
	return nil
}

func (haFDBCluster *HaFdbCluster) updateCluster(fdbCluster *FdbCluster) error {
	for idx, cluster := range haFDBCluster.clusters {
		if strings.HasSuffix(cluster.Name(), fdbCluster.Name()) {
			haFDBCluster.clusters[idx] = fdbCluster
			return nil
		}
	}
	return fmt.Errorf("cluster %s does not exist", fdbCluster.Name())
}

// GetNamespaces returns the namespaces where all FoundationDBClusters are running in.
func (haFDBCluster *HaFdbCluster) GetNamespaces() []string {
	res := make([]string, 0, len(haFDBCluster.clusters))
	for _, fdbCluster := range haFDBCluster.clusters {
		res = append(res, fdbCluster.Namespace())
	}

	return res
}

// getLabelSelector returns the LabelSelectorRequirement that includes all selectors for the FDB cluster. The key will
// be the same and the value will be the 4 clusters that were created.
func (haFDBCluster *HaFdbCluster) getLabelSelector() []metav1.LabelSelectorRequirement {
	var key string
	var values = make([]string, 0, len(haFDBCluster.clusters))

	for _, fdbCluster := range haFDBCluster.clusters {
		// GetResourceLabels will return a map containing one key and one value
		for labelKey, value := range fdbCluster.GetCachedCluster().GetResourceLabels() {
			key = labelKey
			values = append(values, value)
		}
	}

	return []metav1.LabelSelectorRequirement{
		{
			Key:      key,
			Values:   values,
			Operator: metav1.LabelSelectorOpIn,
		},
	}
}

// GetNamespaceSelector returns the chaos mesh selector for this FDB HA cluster and all associated Pods.
func (haFDBCluster *HaFdbCluster) GetNamespaceSelector() v1alpha1.PodSelectorSpec {
	return chaosNamespaceLabelRequirement(
		haFDBCluster.GetNamespaces(),
		haFDBCluster.getLabelSelector(),
	)
}

// SetDatabaseConfiguration sets the new DatabaseConfiguration, without waiting for reconciliation.
func (haFDBCluster *HaFdbCluster) SetDatabaseConfiguration(
	config fdbv1beta2.DatabaseConfiguration,
) {
	for _, fdbCluster := range haFDBCluster.clusters {
		gomega.Expect(fdbCluster.SetDatabaseConfiguration(config, false)).NotTo(gomega.HaveOccurred())
	}
}

// WaitForReconciliation waits for all associated FoundationDBClusters to be reconciled
func (haFDBCluster *HaFdbCluster) WaitForReconciliation(
	options ...func(*ReconciliationOptions),
) error {
	wg := sync.WaitGroup{}
	wg.Add(len(haFDBCluster.clusters))
	mut := sync.Mutex{}

	var err error
	for _, fdbCluster := range haFDBCluster.clusters {
		go func(fdbCluster *FdbCluster) {
			reconcileErr := fdbCluster.WaitForReconciliation(options...)
			if reconcileErr != nil {
				log.Println("error during WaitForReconciliation for", fdbCluster.Name(), "error:", reconcileErr.Error())
				if err != nil {
					mut.Lock()
					err = reconcileErr
					mut.Unlock()
				}
			}
			wg.Done()
		}(fdbCluster)
	}

	wg.Wait()
	if err != nil {
		return err
	}

	return nil
}

func (factory Factory) createHaFdbClusterSpec(
	clusterName string,
	namespace string,
	dcID string,
	seedConnection string,
	databaseConfiguration *fdbv1beta2.DatabaseConfiguration,
	processes map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings,
	mainContainerOverrides fdbv1beta2.ContainerOverrides,
	sidecarContainerOverrides fdbv1beta2.ContainerOverrides,
) *fdbv1beta2.FoundationDBCluster {
	cluster := factory.createFDBClusterSpec(
		clusterName,
		namespace,
		processes,
		*databaseConfiguration,
		// TODO(johscheuer): make this configurable.
		1,
		1,
		mainContainerOverrides,
		sidecarContainerOverrides,
	)

	cluster.Spec.ProcessGroupIDPrefix = dcID
	cluster.Spec.DataCenter = dcID
	cluster.Spec.SeedConnectionString = seedConnection

	return cluster
}

// Delete removes all Clusters associated FoundationDBClusters.
func (haFDBCluster *HaFdbCluster) Delete() {
	for _, cluster := range haFDBCluster.GetAllClusters() {
		gomega.Expect(cluster.Destroy()).NotTo(gomega.HaveOccurred())
	}
}

// UpgradeCluster upgrades the HA FoundationDBCluster to the specified version.
func (haFDBCluster *HaFdbCluster) UpgradeCluster(version string, waitForReconciliation bool) error {
	return haFDBCluster.UpgradeClusterWithTimeout(version, waitForReconciliation, 0)
}

// UpgradeClusterWithTimeout upgrades the HA FoundationDBCluster to the specified version with the provided timeout.
func (haFDBCluster *HaFdbCluster) UpgradeClusterWithTimeout(
	version string,
	waitForReconciliation bool,
	timeout int,
) error {
	expectedGenerations := map[string]int64{}
	for _, cluster := range haFDBCluster.GetAllClusters() {
		curCluster := cluster.GetCluster()

		generation := curCluster.ObjectMeta.Generation
		// Only increase the expected generation if the cluster is not already upgraded.
		if curCluster.Spec.Version != version {
			generation++
		}

		expectedGenerations[cluster.Name()] = generation
		cluster.cluster = curCluster

		err := cluster.UpgradeCluster(version, false)
		if err != nil {
			return err
		}
	}

	if !waitForReconciliation {
		return nil
	}

	for _, cluster := range haFDBCluster.GetAllClusters() {
		err := cluster.WaitForReconciliation(MinimumGenerationOption(expectedGenerations[cluster.Name()]), TimeOutInSecondsOption(timeout), PollTimeInSecondsOption(60))
		if err != nil {
			return err
		}
	}

	return nil
}

// DumpState logs the current state of all FoundationDBClusters.
func (haFDBCluster *HaFdbCluster) DumpState() {
	for _, cluster := range haFDBCluster.clusters {
		cluster.factory.DumpState(cluster)
	}
}

// SetCustomParameters sets the custom parameters for the provided process class.
func (haFDBCluster *HaFdbCluster) SetCustomParameters(processClass fdbv1beta2.ProcessClass,
	customParameters fdbv1beta2.FoundationDBCustomParameters,
	waitForReconcile bool) error {
	for _, cluster := range haFDBCluster.clusters {
		err := cluster.SetCustomParameters(processClass, customParameters, false)
		if err != nil {
			return err
		}
	}

	if !waitForReconcile {
		return nil
	}

	for _, cluster := range haFDBCluster.clusters {
		err := cluster.WaitForReconciliation()
		if err != nil {
			return err
		}
	}

	return nil
}
