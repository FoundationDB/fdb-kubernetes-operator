/*
 * cluster_config.go
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
	"math"
	"strconv"
	"strings"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type cloudProvider string

const (
	cloudProviderKind = "kind"
)

// HaMode represents the targeted HA mode for the created cluster.
type HaMode int

const (
	// HaModeNone refers to a single namespace without HA.
	HaModeNone HaMode = iota
	// HaFourZoneSingleSat refers to a cluster running in 4 namespaces and the DB config has only one satellite.
	HaFourZoneSingleSat
	// HaFourZoneDoubleSat refers to a cluster running in 4 namespaces and the DB config has two satellites.
	HaFourZoneDoubleSat
	// HaThreeZoneDoubleSat refers to a cluster running in 3 namespaces and the DB config has two satellites (triplet configuration).
	HaThreeZoneDoubleSat
	// HaFourZoneDoubleSatRF4 same as HaFourZoneDoubleSat but with the RedundancyModeDouble.
	HaFourZoneDoubleSatRF4
)

// GetRedundancyMode returns the redundancy mode based on the cluster configuration.
func (config ClusterConfig) GetRedundancyMode() fdbv1beta2.RedundancyMode {
	if config.HaMode == HaFourZoneDoubleSatRF4 {
		return fdbv1beta2.RedundancyModeDouble
	}

	return fdbv1beta2.RedundancyModeTriple
}

// ClusterConfig defines the target configuration for the FoundationDBCLuster.
type ClusterConfig struct {
	// If enabled we will use the performance setup.
	Performance bool
	// If enabled the debug images will be used for this test case.
	DebugSymbols bool
	// UseMaintenanceMode if enabled the FoundationDBCluster resource will enable the maintenance mode.
	UseMaintenanceMode bool
	// UseLocalityBasedExclusions if enabled the FoundationDBCluster resource will enable the locality based exclusions.
	UseLocalityBasedExclusions bool
	// UseDNS if enabled the FoundationDBCluster resource will enable the DNS feature.
	UseDNS bool
	// CreationTracker if specified will be used to log the time between the creations steps.
	CreationTracker CreationTrackerLogger
	// Number of machines, this is used for calculating the number of Pods and is not correlated to the actual number
	// of machines that will be used.
	MachineCount int
	// This is also used for calculating the number of Pods.
	DisksPerMachine int
	// StorageServerPerPod defines the value that is set in the FoundationDBClusterSpec for this setting.
	StorageServerPerPod int
	// LogServersPerPod defines the value that is set in the FoundationDBClusterSpec for this setting.
	LogServersPerPod int
	// VolumeSize the size of the volumes that should be created for stateful Pods.
	VolumeSize string
	// Namespace to create the cluster in, if empty will use a randomly generated namespace. The setup won't create the
	// namespace if it's not created.
	Namespace string
	// Name of the cluster to be created, if empty a random name will be used.
	Name string
	// cloudProvider defines the cloud provider used to create the Kubernetes cluster. This value is set in the SetDefaults
	// method.
	cloudProvider cloudProvider
	// The storage engine that should be used to create the cluster
	StorageEngine fdbv1beta2.StorageEngine
	// NodeSelector of the cluster to be created.
	NodeSelector map[string]string
	// Defines the HA mode that will be used, per default this will point to HaModeNone.
	HaMode HaMode
	// CustomParameters allows to define the custom parameters that should be set during the cluster creation.
	CustomParameters map[fdbv1beta2.ProcessClass]fdbv1beta2.FoundationDBCustomParameters
	// CreationCallback allows to specify a method that will be called after the cluster was created.
	CreationCallback func(fdbCluster *FdbCluster)
}

// DefaultClusterConfigWithHaMode returns the default cluster configuration with the provided HA Mode.
func DefaultClusterConfigWithHaMode(haMode HaMode, debugSymbols bool) *ClusterConfig {
	return &ClusterConfig{
		HaMode:       haMode,
		DebugSymbols: debugSymbols,
	}
}

// DefaultClusterConfig returns the default cluster configuration with the HA Mode None.
func DefaultClusterConfig(debugSymbols bool) *ClusterConfig {
	return DefaultClusterConfigWithHaMode(HaModeNone, debugSymbols)
}

// SetDefaults will set all unset fields to the default values.
func (config *ClusterConfig) SetDefaults(factory *Factory) {
	if config.Name == "" {
		config.Name = factory.getClusterName()
	}

	// Only create the namespace for non HA clusters, otherwise the namespaces will be created in a different way.
	if config.Namespace == "" && config.HaMode == HaModeNone {
		config.Namespace = factory.SingleNamespace()
	}

	if config.StorageEngine == "" {
		config.StorageEngine = factory.getStorageEngine()
	}

	if config.StorageServerPerPod == 0 {
		config.StorageServerPerPod = 2
	}

	if config.CreationCallback == nil {
		config.CreationCallback = func(fdbCluster *FdbCluster) {
			if fdbCluster == nil {
				return
			}

			log.Println("FoundationDBCluster", fdbCluster.Name(), "successfully created in", fdbCluster.Namespace())
		}
	}

	if config.cloudProvider == "" {
		config.cloudProvider = cloudProvider(factory.options.cloudProvider)
	}
}

// getVolumeSize returns the volume size in as a string. If no volume size is defined a default will be set based on
// the provided cloud provider.
func (config *ClusterConfig) getVolumeSize() string {
	if config.VolumeSize != "" {
		return config.VolumeSize
	}

	return "16Gi"
}

// generateVolumeClaimTemplate generates a PersistentVolumeClaim for the specified configuration
func (config *ClusterConfig) generateVolumeClaimTemplate(
	storageClass string,
) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClass,
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(config.getVolumeSize()),
				},
			},
		},
	}
}

// generateSidecarResources generates a ResourceList for the sidecar container
func (config *ClusterConfig) generateSidecarResources() corev1.ResourceList {
	// Minimal resources for the sidecar containers
	return corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse("0.1"),
		corev1.ResourceMemory: resource.MustParse("256Mi"),
	}
}

// generatePodResources generates a ResourceList for the specified configuration
func (config *ClusterConfig) generatePodResources(
	processClass fdbv1beta2.ProcessClass,
) corev1.ResourceList {
	if !config.Performance {
		// Minimal resource requests for this cluster to be functional
		return corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("0.2"),
			corev1.ResourceMemory: resource.MustParse("2Gi"),
		}
	}

	// FDB is single threaded so we can assign 1 CPU per process in this Pod and 8 Gi is the default memory footprint
	// for an fdbserver process.
	cpu := 1
	memory := 8
	if processClass == fdbv1beta2.ProcessClassStorage && config.StorageServerPerPod > 1 {
		cpu *= config.StorageServerPerPod
		memory *= config.StorageServerPerPod
	}

	if processClass == fdbv1beta2.ProcessClassLog && config.LogServersPerPod > 1 {
		cpu *= config.LogServersPerPod
		memory *= config.LogServersPerPod
	}

	return corev1.ResourceList{
		corev1.ResourceCPU:    resource.MustParse(strconv.Itoa(cpu)),
		corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dGi", memory)),
	}
}

// CreateDatabaseConfiguration returns the fdbv1beta2.DatabaseConfiguration based on the provided cluster configuration.
func (config *ClusterConfig) CreateDatabaseConfiguration() fdbv1beta2.DatabaseConfiguration {
	switch config.HaMode {
	case HaModeNone:
		return fdbv1beta2.DatabaseConfiguration{
			RedundancyMode: config.GetRedundancyMode(),
			RoleCounts:     config.CalculateRoleCounts(),
			StorageEngine:  config.StorageEngine,
		}
	case HaFourZoneSingleSat:
		return getDatabaseConfigurationFourZoneSingleSat(
			config.CalculateRoleCounts(),
			config.StorageEngine,
			config.GetRedundancyMode(),
		)
	case HaFourZoneDoubleSat, HaFourZoneDoubleSatRF4:
		return getDatabaseConfigurationFourZoneDoubleSat(
			config.CalculateRoleCounts(),
			config.StorageEngine,
			config.GetRedundancyMode(),
		)
	case HaThreeZoneDoubleSat:
		return getDatabaseConfigurationThreeZoneDoubleSat(
			config.CalculateRoleCounts(),
			config.StorageEngine,
			config.GetRedundancyMode(),
		)
	}

	ginkgo.Fail("unknown configuration")
	return fdbv1beta2.DatabaseConfiguration{}
}

func (config *ClusterConfig) getCustomParametersForProcessClass(processClass fdbv1beta2.ProcessClass) fdbv1beta2.FoundationDBCustomParameters {
	customParameters, ok := config.CustomParameters[processClass]
	if !ok {
		return []fdbv1beta2.FoundationDBCustomParameter{
			"trace_format=json",
		}
	}

	containsTraceParameter := false
	for _, customParameter := range customParameters {
		if !strings.Contains(string(customParameter), "trace_format") {
			continue
		}

		containsTraceParameter = true
	}

	if !containsTraceParameter {
		customParameters = append(customParameters, "trace_format=json")
	}

	return customParameters
}

func getDatabaseConfigurationFourZoneSingleSat(
	roleCounts fdbv1beta2.RoleCounts,
	storageEngine fdbv1beta2.StorageEngine,
	redundancyMode fdbv1beta2.RedundancyMode,
) fdbv1beta2.DatabaseConfiguration {
	dbConfig := fdbv1beta2.DatabaseConfiguration{
		Regions: []fdbv1beta2.Region{
			{
				DataCenters: []fdbv1beta2.DataCenter{
					{
						ID:       PrimaryID,
						Priority: 1,
					},
					{
						ID:        PrimarySatelliteID,
						Satellite: 1,
						Priority:  2,
					},
				},
				SatelliteLogs:           roleCounts.Logs,
				SatelliteRedundancyMode: "one_satellite_double",
			},
			{
				DataCenters: []fdbv1beta2.DataCenter{
					{
						ID:       RemoteID,
						Priority: 0,
					},
					{
						ID:        RemoteSatelliteID,
						Satellite: 1,
						Priority:  2,
					},
				},
				SatelliteLogs:           roleCounts.Logs,
				SatelliteRedundancyMode: "one_satellite_double",
			},
		},
	}
	completeDatabaseConfiguration(&dbConfig, roleCounts, storageEngine, redundancyMode)
	return dbConfig
}

func getDatabaseConfigurationFourZoneDoubleSat(
	roleCounts fdbv1beta2.RoleCounts,
	storageEngine fdbv1beta2.StorageEngine,
	redundancyMode fdbv1beta2.RedundancyMode,
) fdbv1beta2.DatabaseConfiguration {
	dbConfig := fdbv1beta2.DatabaseConfiguration{
		Regions: []fdbv1beta2.Region{
			{
				DataCenters: []fdbv1beta2.DataCenter{
					{
						ID:       PrimaryID,
						Priority: 1,
					},
					{
						ID:        PrimarySatelliteID,
						Satellite: 1,
						Priority:  2,
					},
					{
						ID:        RemoteSatelliteID,
						Satellite: 1,
						Priority:  1,
					},
				},
				SatelliteLogs:           roleCounts.Logs,
				SatelliteRedundancyMode: "one_satellite_double",
			},
			{
				DataCenters: []fdbv1beta2.DataCenter{
					{
						ID:       RemoteID,
						Priority: 0,
					},
					{
						ID:        RemoteSatelliteID,
						Satellite: 1,
						Priority:  2,
					},
					{
						ID:        PrimarySatelliteID,
						Satellite: 1,
						Priority:  1,
					},
				},
				SatelliteLogs:           roleCounts.Logs,
				SatelliteRedundancyMode: "one_satellite_double",
			},
		},
	}
	completeDatabaseConfiguration(&dbConfig, roleCounts, storageEngine, redundancyMode)
	return dbConfig
}

func getDatabaseConfigurationThreeZoneDoubleSat(
	roleCounts fdbv1beta2.RoleCounts,
	storageEngine fdbv1beta2.StorageEngine,
	redundancyMode fdbv1beta2.RedundancyMode,
) fdbv1beta2.DatabaseConfiguration {
	dbConfig := fdbv1beta2.DatabaseConfiguration{
		Regions: []fdbv1beta2.Region{
			{
				DataCenters: []fdbv1beta2.DataCenter{
					{
						ID:       PrimaryID,
						Priority: 1,
					},
					{
						ID:        SatelliteID,
						Satellite: 1,
						Priority:  2,
					},
					{
						ID:        RemoteID,
						Satellite: 1,
						Priority:  1,
					},
				},
				SatelliteLogs:           roleCounts.Logs,
				SatelliteRedundancyMode: "one_satellite_double",
			},
			{
				DataCenters: []fdbv1beta2.DataCenter{
					{
						ID:       RemoteID,
						Priority: 0,
					},
					{
						ID:        SatelliteID,
						Satellite: 1,
						Priority:  2,
					},
					{
						ID:        PrimaryID,
						Satellite: 1,
						Priority:  1,
					},
				},
				SatelliteLogs:           roleCounts.Logs,
				SatelliteRedundancyMode: "one_satellite_double",
			},
		},
	}
	completeDatabaseConfiguration(&dbConfig, roleCounts, storageEngine, redundancyMode)
	return dbConfig
}

func completeDatabaseConfiguration(
	dbConfig *fdbv1beta2.DatabaseConfiguration,
	roleCounts fdbv1beta2.RoleCounts,
	storageEngine fdbv1beta2.StorageEngine,
	redundancyMode fdbv1beta2.RedundancyMode,
) {
	dbConfig.RedundancyMode = redundancyMode
	dbConfig.UsableRegions = len(dbConfig.Regions)
	dbConfig.RoleCounts = roleCounts
	dbConfig.StorageEngine = storageEngine
}

// CalculateRoleCounts attempt to scale role counts in a way that is reasonable for read-heavy OLTP-style workloads
// 1 disk will hold logs. The rest holds data.
func (config *ClusterConfig) CalculateRoleCounts() fdbv1beta2.RoleCounts {
	desiredFaultTolerance := fdbv1beta2.DesiredFaultTolerance(config.GetRedundancyMode())
	machineCount := config.MachineCount
	disksPerMachine := config.DisksPerMachine

	grv, commit := calculateProxies(machineCount - desiredFaultTolerance)

	roleCounts := fdbv1beta2.RoleCounts{
		// One disk is used by the log process the rest of those is used by storage processes.
		Storage: max(machineCount*(disksPerMachine-1), 5),
		// We run one log process per disk
		Logs:          max(machineCount-desiredFaultTolerance, 3),
		Proxies:       grv + commit,
		GrvProxies:    grv,
		CommitProxies: commit,
		// This is a simple heuristic that might be true or not for the current workload.
		Resolvers: max(int(math.Floor(float64(machineCount)/7)), 1),
	}

	if config.HaMode > 0 {
		// For a HA cluster set log routers and remote logs the same as logs.
		roleCounts.RemoteLogs = roleCounts.Logs
		roleCounts.LogRouters = roleCounts.Logs
	}

	return roleCounts
}

func max(a int, b int) int {
	if a > b {
		return a
	}

	return b
}

func min(a int, b int) int {
	if a < b {
		return a
	}

	return b
}

func calculateProxies(proxies int) (int, int) {
	// This calculation is only a rough estimate and can change based on the workload.
	// Use 1/4 of the proxies for GRV or at max 4 processes
	grv := min(proxies/4, 4)
	commit := proxies - grv

	// Ensure we create at least 1 process of each proxy type
	return max(grv, 1), max(commit, 1)
}

// Copy will return a new struct of the ClusterConfig.
func (config *ClusterConfig) Copy() *ClusterConfig {
	return &ClusterConfig{
		Performance:                config.Performance,
		DebugSymbols:               config.DebugSymbols,
		UseMaintenanceMode:         config.UseMaintenanceMode,
		CreationTracker:            config.CreationTracker,
		MachineCount:               config.MachineCount,
		DisksPerMachine:            config.DisksPerMachine,
		StorageServerPerPod:        config.StorageServerPerPod,
		LogServersPerPod:           config.LogServersPerPod,
		VolumeSize:                 config.VolumeSize,
		Namespace:                  config.Namespace,
		Name:                       config.Name,
		cloudProvider:              config.cloudProvider,
		StorageEngine:              config.StorageEngine,
		NodeSelector:               config.NodeSelector,
		HaMode:                     config.HaMode,
		CustomParameters:           config.CustomParameters,
		CreationCallback:           config.CreationCallback,
		UseDNS:                     config.UseDNS,
		UseLocalityBasedExclusions: config.UseLocalityBasedExclusions,
	}
}
