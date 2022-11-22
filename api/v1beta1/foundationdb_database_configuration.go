/*
 * foundationdb_labels.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2022 Apple Inc. and the FoundationDB project authors
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

import (
	"encoding/json"
	"fmt"
	"html/template"
	"reflect"
	"sort"
	"strings"
)

// DatabaseConfiguration represents the configuration of the database
type DatabaseConfiguration struct {
	// RedundancyMode defines the core replication factor for the database.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=single;double;triple;three_data_hall
	// +kubebuilder:default:double
	RedundancyMode RedundancyMode `json:"redundancy_mode,omitempty"`

	// StorageEngine defines the storage engine the database uses.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Enum=ssd;ssd-1;ssd-2;memory;memory-1;memory-2;ssd-redwood-1-experimental;ssd-rocksdb-experimental;memory-radixtree-beta;custom
	// +kubebuilder:default:=ssd-2
	StorageEngine StorageEngine `json:"storage_engine,omitempty"`

	// UsableRegions defines how many regions the database should store data in.
	UsableRegions int `json:"usable_regions,omitempty"`

	// Regions defines the regions that the database can replicate in.
	Regions []Region `json:"regions,omitempty"`

	// RoleCounts defines how many processes the database should recruit for
	// each role.
	RoleCounts `json:""`

	// VersionFlags defines internal flags for testing new features in the
	// database.
	VersionFlags `json:""`
}

// Region represents a region in the database configuration
type Region struct {
	// The data centers in this region.
	DataCenters []DataCenter `json:"datacenters,omitempty"`

	// The number of satellite logs that we should recruit.
	SatelliteLogs int `json:"satellite_logs,omitempty"`

	// The replication strategy for satellite logs.
	SatelliteRedundancyMode string `json:"satellite_redundancy_mode,omitempty"`
}

// DataCenter represents a data center in the region configuration
type DataCenter struct {
	// The ID of the data center. This must match the dcid locality field.
	ID string `json:"id,omitempty"`

	// The priority of this data center when we have to choose a location.
	// Higher priorities are preferred over lower priorities.
	Priority int `json:"priority,omitempty"`

	// Satellite indicates whether the data center is serving as a satellite for
	// the region. A value of 1 indicates that it is a satellite, and a value of
	// 0 indicates that it is not a satellite.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
	Satellite int `json:"satellite,omitempty"`
}

// FailOver returns a new DatabaseConfiguration that switches the priority for the main and remote DC
func (configuration *DatabaseConfiguration) FailOver() DatabaseConfiguration {
	if len(configuration.Regions) <= 1 {
		return *configuration
	}

	newConfiguration := configuration.DeepCopy()
	newRegions := make([]Region, 0, len(configuration.Regions))
	priorityMap := map[string]int{}

	// Get the priority of the DCs
	for _, region := range newConfiguration.Regions {
		for _, dc := range region.DataCenters {
			// Don't change the Satellite config
			if dc.Satellite == 1 {
				continue
			}

			priorityMap[dc.ID] = dc.Priority
		}
	}

	for _, region := range newConfiguration.Regions {
		// In order to trigger a fail over we have to change the priority
		// of the main and remote dc
		newRegion := Region{}
		for _, dc := range region.DataCenters {
			// Don't change the Satellite config
			if dc.Satellite == 1 {
				newRegion.DataCenters = append(newRegion.DataCenters, dc)
				continue
			}

			// This will only work as long as we have only two
			// regions.
			for key, val := range priorityMap {
				if key == dc.ID {
					continue
				}

				newRegion.DataCenters = append(newRegion.DataCenters, DataCenter{
					ID:       dc.ID,
					Priority: val,
				})
			}
		}

		newRegions = append(newRegions, newRegion)
	}

	newConfiguration.Regions = newRegions

	return *newConfiguration
}

// NormalizeConfiguration ensures a standardized format and defaults when
// comparing database configuration in the cluster spec with database
// configuration in the cluster status.
//
// This will fill in defaults of -1 for some fields that have a default of 0,
// and will ensure that the region configuration is ordered consistently.
func (configuration DatabaseConfiguration) NormalizeConfiguration() DatabaseConfiguration {
	result := configuration.DeepCopy()

	if result.RemoteLogs == 0 {
		result.RemoteLogs = -1
	}
	if result.LogRouters == 0 {
		result.LogRouters = -1
	}

	if result.UsableRegions < 1 {
		result.UsableRegions = 1
	}

	if result.RedundancyMode == RedundancyModeUnset {
		result.RedundancyMode = RedundancyModeDouble
	}

	if result.StorageEngine == "" {
		result.StorageEngine = StorageEngineSSD2
	}

	for _, region := range result.Regions {
		sort.Slice(region.DataCenters, func(leftIndex int, rightIndex int) bool {
			if region.DataCenters[leftIndex].Satellite != region.DataCenters[rightIndex].Satellite {
				return region.DataCenters[leftIndex].Satellite < region.DataCenters[rightIndex].Satellite
			} else if region.DataCenters[leftIndex].Priority != region.DataCenters[rightIndex].Priority {
				return region.DataCenters[leftIndex].Priority > region.DataCenters[rightIndex].Priority
			} else {
				return region.DataCenters[leftIndex].ID < region.DataCenters[rightIndex].ID
			}
		})
	}

	sort.Slice(result.Regions, func(leftIndex int, rightIndex int) bool {
		leftID, leftPriority := getMainDataCenter(result.Regions[leftIndex])
		rightID, rightPriority := getMainDataCenter(result.Regions[rightIndex])
		if leftPriority != rightPriority {
			return leftPriority > rightPriority
		}
		return leftID < rightID
	})

	return *result
}

func (configuration DatabaseConfiguration) getRegion(id string, priority int) Region {
	var matchingRegion Region

	for _, region := range configuration.Regions {
		for dataCenterIndex, dataCenter := range region.DataCenters {
			if dataCenter.Satellite == 0 && dataCenter.ID == id {
				matchingRegion = *region.DeepCopy()
				matchingRegion.DataCenters[dataCenterIndex].Priority = priority
				break
			}
		}
		if len(matchingRegion.DataCenters) > 0 {
			break
		}
	}

	if len(matchingRegion.DataCenters) == 0 {
		matchingRegion.DataCenters = append(matchingRegion.DataCenters, DataCenter{ID: id, Priority: priority})
	}

	return matchingRegion
}

// GetNextConfigurationChange produces the next marginal change that should
// be made to transform this configuration into another configuration.
//
// If there are multiple changes between the two configurations that can not be
// made simultaneously, this will produce a subset of the changes that move
// in the correct direction. Applying this method repeatedly will eventually
// converge on the final configuration.
func (configuration DatabaseConfiguration) GetNextConfigurationChange(finalConfiguration DatabaseConfiguration) DatabaseConfiguration {
	if !reflect.DeepEqual(configuration.Regions, finalConfiguration.Regions) {
		result := configuration.DeepCopy()
		currentPriorities := configuration.getRegionPriorities()
		nextPriorities := finalConfiguration.getRegionPriorities()
		finalPriorities := finalConfiguration.getRegionPriorities()

		// Step 1: Apply any changes to the satellites and satellite redundancy
		// from the final configuration to the next configuration.
		for regionIndex, region := range result.Regions {
			for _, dataCenter := range region.DataCenters {
				if dataCenter.Satellite == 0 {
					result.Regions[regionIndex] = finalConfiguration.getRegion(dataCenter.ID, dataCenter.Priority)
					break
				}
			}
		}

		// Step 2: If we have a region that is in the final config that is not
		// in the current config, add it.
		//
		// We can currently only add a maximum of two regions at a time.
		//
		// The new region will join at a negative priority, unless it is the
		// first region in the list.
		regionToAdd := "none"
		for len(result.Regions) < 2 && regionToAdd != "" {
			regionToAdd = ""

			for id, priority := range nextPriorities {
				_, present := currentPriorities[id]
				if !present && (regionToAdd == "" || priority > nextPriorities[regionToAdd]) {
					regionToAdd = id
				}
			}

			if regionToAdd != "" {
				priority := -1
				if len(result.Regions) == 0 {
					priority = 1
				}
				result.Regions = append(result.Regions, finalConfiguration.getRegion(regionToAdd, priority))
				currentPriorities[regionToAdd] = priority
			}
		}
		if len(result.Regions) != len(configuration.Regions) {
			return *result
		}

		currentRegions := make([]string, 0, len(configuration.Regions))
		for _, region := range configuration.Regions {
			for _, dataCenter := range region.DataCenters {
				if dataCenter.Satellite == 0 {
					currentRegions = append(currentRegions, dataCenter.ID)
				}
			}
		}

		// Step 3: If we currently have multiple regions, and one of them is not
		// in the final config, remove it.
		//
		// If that region has a positive priority, we must first give it a
		// negative priority.
		//
		// Before removing regions, the UsableRegions must be set to the next
		// region count.
		//
		// We skip this step if we are going to be removing region configuration
		// entirely.
		for _, regionID := range currentRegions {
			_, present := finalPriorities[regionID]
			if !present && len(configuration.Regions) > 1 && len(finalConfiguration.Regions) > 0 {
				if currentPriorities[regionID] >= 0 {
					continue
				} else if result.UsableRegions != len(result.Regions)-1 {
					result.UsableRegions = len(result.Regions) - 1
				} else {
					newRegions := make([]Region, 0, len(result.Regions)-1)
					for _, region := range result.Regions {
						toRemove := false
						for _, dataCenter := range region.DataCenters {
							if dataCenter.Satellite == 0 && dataCenter.ID == regionID {
								toRemove = true
							}
						}
						if !toRemove {
							newRegions = append(newRegions, region)
						}
					}
					result.Regions = newRegions
				}
				return *result
			}
		}

		for _, regionID := range currentRegions {
			priority := currentPriorities[regionID]
			_, present := finalPriorities[regionID]
			if !present && len(configuration.Regions) > 1 && len(finalConfiguration.Regions) > 0 {
				if priority > 0 && configuration.UsableRegions < 2 {
					continue
				} else if priority >= 0 {
					hasAlternativePrimary := false
					for regionIndex, region := range result.Regions {
						for dataCenterIndex, dataCenter := range region.DataCenters {
							if dataCenter.Satellite == 0 {
								if dataCenter.ID == regionID {
									result.Regions[regionIndex].DataCenters[dataCenterIndex].Priority = -1
								} else if dataCenter.Priority > 0 {
									hasAlternativePrimary = true
								}
							}
						}
					}

					if !hasAlternativePrimary {
						for regionIndex, region := range result.Regions {
							for dataCenterIndex, dataCenter := range region.DataCenters {
								if dataCenter.Satellite == 0 {
									if dataCenter.ID != regionID {
										result.Regions[regionIndex].DataCenters[dataCenterIndex].Priority = 1
										break
									}
								}
							}
						}
					}

					return *result
				} else if result.UsableRegions != len(result.Regions)-1 {
					result.UsableRegions = len(result.Regions) - 1
				} else {
					newRegions := make([]Region, 0, len(result.Regions)-1)
					for _, region := range result.Regions {
						toRemove := false
						for _, dataCenter := range region.DataCenters {
							if dataCenter.Satellite == 0 && dataCenter.ID == regionID {
								toRemove = true
							}
						}
						if !toRemove {
							newRegions = append(newRegions, region)
						}
					}
					result.Regions = newRegions
				}
				return *result
			}
		}

		// Step 4: Set all priorities for the regions to the desired value.
		//
		// If no region is configured to have a positive priority, ensure that
		// at least one region has a positive priority.
		//
		// Before changing priorities, we must ensure that all regions are
		// usable.

		maxCurrent := ""
		maxNext := ""

		for id, priority := range currentPriorities {
			_, present := nextPriorities[id]
			if !present {
				nextPriorities[id] = -1
			}
			if maxCurrent == "" || currentPriorities[maxCurrent] < priority {
				maxCurrent = id
			}
		}

		for id, priority := range nextPriorities {
			_, present := currentPriorities[id]
			if !present {
				currentPriorities[id] = -1
			}
			if maxNext == "" || nextPriorities[maxNext] < priority {
				maxNext = id
			}
		}

		if maxNext == "" || nextPriorities[maxNext] < 0 {
			nextPriorities[maxCurrent] = currentPriorities[maxCurrent]
		}

		if !reflect.DeepEqual(currentPriorities, nextPriorities) {
			if configuration.UsableRegions != len(configuration.Regions) {
				result.UsableRegions = len(configuration.Regions)
			} else {
				for regionIndex, region := range result.Regions {
					for dataCenterIndex, dataCenter := range region.DataCenters {
						if dataCenter.Satellite == 0 {
							result.Regions[regionIndex].DataCenters[dataCenterIndex].Priority = nextPriorities[dataCenter.ID]
							break
						}
					}
				}
			}
			return *result
		}

		// Step 5: Set the final region count.
		if configuration.UsableRegions != finalConfiguration.UsableRegions {
			result.UsableRegions = finalConfiguration.UsableRegions
			return *result
		}

		// Step 6: Set the final region config.
		result.Regions = finalConfiguration.Regions
		return *result
	}

	return finalConfiguration
}

func (configuration DatabaseConfiguration) getRegionPriorities() map[string]int {
	priorities := make(map[string]int, len(configuration.Regions))

	for _, region := range configuration.Regions {
		for _, dataCenter := range region.DataCenters {
			if dataCenter.Satellite == 0 {
				priorities[dataCenter.ID] = dataCenter.Priority
			}
		}
	}

	return priorities
}

// FillInDefaultsFromStatus adds in missing fields from the database
// configuration in the database status to make sure they match the fields that
// will appear in the cluster spec.
//
// Deprecated: Use NormalizeConfiguration instead.
func (configuration DatabaseConfiguration) FillInDefaultsFromStatus() DatabaseConfiguration {
	result := configuration.DeepCopy()

	if result.RemoteLogs == 0 {
		result.RemoteLogs = -1
	}
	if result.LogRouters == 0 {
		result.LogRouters = -1
	}
	return *result
}

// GetConfigurationString gets the CLI command for configuring a database.
func (configuration DatabaseConfiguration) GetConfigurationString() (string, error) {
	configurationString := fmt.Sprintf("%s %s", configuration.RedundancyMode, configuration.StorageEngine)

	counts := configuration.RoleCounts.Map()
	configurationString += fmt.Sprintf(" usable_regions=%d", configuration.UsableRegions)
	// TODO: roleNames !
	for _, role := range roleNames {
		if role != ProcessClassStorage {
			configurationString += fmt.Sprintf(" %s=%d", role, counts[role])
		}
	}

	flags := configuration.VersionFlags.Map()
	for flag, value := range flags {
		if value != 0 {
			configurationString += fmt.Sprintf(" %s:=%d", flag, value)
		}
	}

	var regionString string
	if configuration.Regions == nil {
		regionString = "[]"
	} else {
		regionBytes, err := json.Marshal(configuration.Regions)
		if err != nil {
			return "", err
		}
		regionString = template.JSEscapeString(string(regionBytes))
	}

	configurationString += " regions=" + regionString

	return configurationString, nil
}

// FillInDefaultVersionFlags adds in missing version flags so they match the
// running configuration.
//
// Deprecated: Use ClearMissingVersionFlags instead on the live configuration
// instead.
func (configuration *DatabaseConfiguration) FillInDefaultVersionFlags(liveConfiguration DatabaseConfiguration) {
	if configuration.LogSpill == 0 {
		configuration.LogSpill = liveConfiguration.LogSpill
	}
}

func getMainDataCenter(region Region) (string, int) {
	for _, dataCenter := range region.DataCenters {
		if dataCenter.Satellite == 0 {
			return dataCenter.ID, dataCenter.Priority
		}
	}
	return "", -1
}

// DesiredFaultTolerance returns the number of replicas we should be able to
// lose given a redundancy mode.
func DesiredFaultTolerance(redundancyMode RedundancyMode) int {
	switch redundancyMode {
	case RedundancyModeSingle:
		return 0
	case RedundancyModeDouble, RedundancyModeUnset:
		return 1
	case RedundancyModeTriple:
		return 2
	case RedundancyModeThreeDataHall:
		return 2
	default:
		return 0
	}
}

// MinimumFaultDomains returns the number of fault domains given a redundancy mode.
func MinimumFaultDomains(redundancyMode RedundancyMode) int {
	switch redundancyMode {
	case RedundancyModeSingle:
		return 1
	case RedundancyModeDouble, RedundancyModeUnset:
		return 2
	case RedundancyModeTriple:
		return 3
	case RedundancyModeThreeDataHall:
		return 3
	default:
		return 1
	}
}

// RedundancyMode defines the core replication factor for the database
// +kubebuilder:validation:MaxLength=100
type RedundancyMode string

const (
	// RedundancyModeSingle defines the replication factor 1.
	RedundancyModeSingle RedundancyMode = "single"
	// RedundancyModeDouble defines the replication factor 2.
	RedundancyModeDouble RedundancyMode = "double"
	// RedundancyModeTriple defines the replication factor 3.
	RedundancyModeTriple RedundancyMode = "triple"
	// RedundancyModeThreeDataHall defines the replication factor three_data_hall.
	RedundancyModeThreeDataHall RedundancyMode = "three_data_hall"
	// RedundancyModeUnset defines the replication factor unset.
	RedundancyModeUnset RedundancyMode = ""
)

// StorageEngine defines the storage engine for the database
// +kubebuilder:validation:MaxLength=100
type StorageEngine string

const (
	// StorageEngineSSD defines the storage engine ssd.
	StorageEngineSSD StorageEngine = "ssd"
	// StorageEngineSSD2 defines the storage engine ssd-2.
	StorageEngineSSD2 StorageEngine = "ssd-2"
	// StorageEngineMemory defines the storage engine memory.
	StorageEngineMemory StorageEngine = "memory"
	// StorageEngineMemory2 defines the storage engine memory-2.
	StorageEngineMemory2 StorageEngine = "memory-2"
)

// RoleCounts represents the roles whose counts can be customized.
type RoleCounts struct {
	Storage    int `json:"storage,omitempty"`
	Logs       int `json:"logs,omitempty"`
	Proxies    int `json:"proxies,omitempty"`
	Resolvers  int `json:"resolvers,omitempty"`
	LogRouters int `json:"log_routers,omitempty"`
	RemoteLogs int `json:"remote_logs,omitempty"`
}

// Map returns a map from process classes to the desired count for that role
func (counts RoleCounts) Map() map[ProcessClass]int {
	countMap := make(map[ProcessClass]int, len(roleIndices))
	countValue := reflect.ValueOf(counts)
	for role, index := range roleIndices {
		if role != ProcessClassStorage {
			value := int(countValue.Field(index).Int())
			countMap[role] = value
		}
	}
	return countMap
}

// VersionFlags defines internal flags for new features in the database.
type VersionFlags struct {
	LogSpill   int `json:"log_spill,omitempty"`
	LogVersion int `json:"log_version,omitempty"`
}

// Map returns a map from process classes to the desired count for that role
func (flags VersionFlags) Map() map[string]int {
	flagMap := make(map[string]int, len(versionFlagIndices))
	flagValue := reflect.ValueOf(flags)
	for flag, index := range versionFlagIndices {
		value := int(flagValue.Field(index).Int())
		flagMap[flag] = value
	}
	return flagMap
}

// roleIndices provides the indices of each role in the list of roles.
var roleIndices = make(map[ProcessClass]int)

// versionFlagIndices provides the indices of each flag in the list of supported
// version flags..
var versionFlagIndices = make(map[string]int)

// roleNames provides a consistent ordered list of the supported roles.
var roleNames = fieldNames(RoleCounts{})

// fieldNames provides the names of fields on a structure.
func fieldNames(value interface{}) []ProcessClass {
	countType := reflect.TypeOf(value)
	names := make([]ProcessClass, 0, countType.NumField())
	for index := 0; index < countType.NumField(); index++ {
		tag := strings.Split(countType.Field(index).Tag.Get("json"), ",")
		names = append(names, ProcessClass(tag[0]))
	}
	return names
}

// processClassIndices provides the indices of each process class in the list
// of process classes.
var processClassIndices = make(map[ProcessClass]int)

// ProcessClasses provides a consistent ordered list of the supported process
// classes.
var ProcessClasses = fieldNames(ProcessCounts{})

func init() {
	fieldIndices(ProcessCounts{}, processClassIndices, reflect.TypeOf(ProcessClassStorage))
	fieldIndices(RoleCounts{}, roleIndices, reflect.TypeOf(ProcessClassStorage))
	fieldIndices(VersionFlags{}, versionFlagIndices, reflect.TypeOf(""))
}

// fieldIndices provides a map from the names of fields in a structure to the
// index of each field in the list of fields.
func fieldIndices(value interface{}, result interface{}, keyType reflect.Type) {
	countType := reflect.TypeOf(value)
	resultValue := reflect.ValueOf(result)
	for index := 0; index < countType.NumField(); index++ {
		tag := strings.Split(countType.Field(index).Tag.Get("json"), ",")
		resultValue.SetMapIndex(reflect.ValueOf(tag[0]).Convert(keyType), reflect.ValueOf(index))
	}
}

// ProcessCounts represents the number of processes we have for each valid
// process class.
//
// If one of the counts in the spec is set to 0, we will infer the process count
// for that class from the role counts. If one of the counts in the spec is set
// to -1, we will not create any processes for that class. See
// GetProcessCountsWithDefaults for more information on the rules for inferring
// process counts.
type ProcessCounts struct {
	Unset       int `json:"unset,omitempty"`
	Storage     int `json:"storage,omitempty"`
	Transaction int `json:"transaction,omitempty"`
	Resolution  int `json:"resolution,omitempty"`
	// Deprecated: This setting will be removed in the next major release.
	// use Test
	Tester            int `json:"tester,omitempty"`
	Test              int `json:"test,omitempty"`
	Proxy             int `json:"proxy,omitempty"`
	Master            int `json:"master,omitempty"`
	Stateless         int `json:"stateless,omitempty"`
	Log               int `json:"log,omitempty"`
	ClusterController int `json:"cluster_controller,omitempty"`
	LogRouter         int `json:"router,omitempty"`
	FastRestore       int `json:"fast_restore,omitempty"`
	DataDistributor   int `json:"data_distributor,omitempty"`
	Coordinator       int `json:"coordinator,omitempty"`
	Ratekeeper        int `json:"ratekeeper,omitempty"`
	StorageCache      int `json:"storage_cache,omitempty"`
	BackupWorker      int `json:"backup,omitempty"`

	// Deprecated: This is unsupported and any processes with this process class
	// will fail to start.
	Resolver int `json:"resolver,omitempty"`
}

// Map returns a map from process classes to the number of processes with that
// class.
func (counts ProcessCounts) Map() map[ProcessClass]int {
	countMap := make(map[ProcessClass]int, len(processClassIndices))
	countValue := reflect.ValueOf(counts)
	for processClass, index := range processClassIndices {
		value := int(countValue.Field(index).Int())
		if value > 0 {
			countMap[processClass] = value
		}
	}
	return countMap
}

// IncreaseCount adds to one of the process counts based on the name.
func (counts *ProcessCounts) IncreaseCount(name ProcessClass, amount int) {
	index, present := processClassIndices[name]
	if present {
		countValue := reflect.ValueOf(counts)
		value := countValue.Elem().Field(index)
		value.SetInt(value.Int() + int64(amount))
	}
}

// DecreaseCount adds to one of the process counts based on the name.
func (counts *ProcessCounts) DecreaseCount(name ProcessClass, amount int) {
	index, present := processClassIndices[name]
	if present {
		countValue := reflect.ValueOf(counts)
		value := countValue.Elem().Field(index)
		value.SetInt(value.Int() - int64(amount))
	}
}

// CountsAreSatisfied checks whether the current counts of processes satisfy
// a desired set of counts.
func (counts ProcessCounts) CountsAreSatisfied(currentCounts ProcessCounts) bool {
	return len(counts.Diff(currentCounts)) == 0
}

// Diff gets the Diff between two sets of process counts.
func (counts ProcessCounts) Diff(currentCounts ProcessCounts) map[ProcessClass]int64 {
	diff := make(map[ProcessClass]int64)
	desiredValue := reflect.ValueOf(counts)
	currentValue := reflect.ValueOf(currentCounts)
	for label, index := range processClassIndices {
		desired := desiredValue.Field(index).Int()
		current := currentValue.Field(index).Int()
		if (desired > 0 || current > 0) && desired != current {
			diff[label] = desired - current
		}
	}
	return diff
}
