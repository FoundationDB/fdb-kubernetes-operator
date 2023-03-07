/*
 * fdb_operator_fixtures.go
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
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"k8s.io/apimachinery/pkg/api/equality"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/pointer"
	"log"
)

func (factory *Factory) ensureFdbClusterExists(
	clusterSpec *fdbv1beta2.FoundationDBCluster,
	config *ClusterConfig,
) (*FdbCluster, error) {
	clusterStatus, err := factory.getClusterStatus(clusterSpec.Name, clusterSpec.Namespace)
	if err != nil && !k8serrors.IsNotFound(err) {
		return nil, fmt.Errorf("could not look up FDB cluster: %w", err)
	}
	if err == nil {
		log.Printf("reuse cluster: %s/%s", clusterSpec.Namespace, clusterStatus.Name)
		return factory.createFdbClusterObject(clusterStatus), nil
	}

	log.Printf("preparing to create fdb cluster: %s/%s", clusterSpec.Namespace, clusterSpec.Name)
	fdbCluster := factory.createFdbClusterObject(clusterSpec)
	err = fdbCluster.Create()
	if err != nil {
		// consider checking k8serrors.IsAlreadyExists(err), but if that's
		// the case, we're probably running concurrently with another
		// test that's using this cluster name -- may as well fail now.
		return nil, err
	}
	// Wait until the cluster CRD object exists. The caller should wait for whatever state they care about.
	fdbCluster.WaitUntilExists()
	// Wait until cluster is reconciled -- otherwise, the operator may not have
	// assigned pods, etc.

	err = fdbCluster.WaitForReconciliation(CreationTrackerLoggerOption(config.CreationTracker))
	if err != nil {
		return nil, err
	}

	config.CreationCallback(fdbCluster)

	return fdbCluster, nil
}
func (factory *Factory) ensureHaMemberClusterExists(
	haFdbCluster *HaFdbCluster,
	dcID string,
	namespace string,
	clusterPrefix string,
	seedConnection string,
	processes map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings,
	databaseConfiguration *fdbv1beta2.DatabaseConfiguration,
	mainContainerOverrides fdbv1beta2.ContainerOverrides,
	sidecarContainerOverrides fdbv1beta2.ContainerOverrides,
	options []ClusterOption,
) error {
	clusterName := fmt.Sprintf("%s-%s", clusterPrefix, dcID)
	var initMode bool
	if len(databaseConfiguration.Regions) == 1 {
		initMode = true
	}

	completeDatabaseConfiguration(
		databaseConfiguration,
		databaseConfiguration.RoleCounts,
		databaseConfiguration.StorageEngine,
		databaseConfiguration.RedundancyMode,
	)

	spec := factory.createHaFdbClusterSpec(
		clusterName,
		namespace,
		dcID,
		seedConnection,
		databaseConfiguration,
		processes,
		mainContainerOverrides,
		sidecarContainerOverrides,
	)

	for _, option := range options {
		option(factory, spec)
	}

	curCluster := factory.createFdbClusterObject(spec)
	factory.logClusterInfo(spec)
	// We have to trigger here an update since the cluster already exists!
	fetchedClusterStatus, err := factory.getClusterStatus(clusterName, namespace)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			log.Printf(
				"preparing to create ha fdb cluster: %s/%s",
				curCluster.cluster.Namespace,
				curCluster.cluster.Name,
			)
			err = curCluster.Create()
			if err != nil && !k8serrors.IsAlreadyExists(err) {
				return err
			}
			log.Printf(
				"wait for ha fdb cluster: %s/%s",
				curCluster.cluster.Namespace,
				curCluster.cluster.Name,
			)

			curCluster.WaitUntilExists()
			return haFdbCluster.addCluster(curCluster)
		}
		return err
	}
	fetchedCluster := factory.createFdbClusterObject(fetchedClusterStatus)

	// Cluster already exists, so we want to update it if something is missing. If we call this method for the first time
	// we create a single FDB cluster without a multi-region config, we don't want to overwrite the config if we already
	// have a HA cluster running, since this would change the HA config to a single cluster. We only want to update the
	// database configuration if they have the same number of regions configured. We use the number of regions as a heuristic
	// if the cluster is already running in a HA configuration.
	if !equality.Semantic.DeepEqual(
		fetchedCluster.cluster.Spec.DatabaseConfiguration,
		curCluster.cluster.Spec.DatabaseConfiguration,
	) && !initMode {
		fetchedCluster.cluster.Spec.DatabaseConfiguration = curCluster.cluster.Spec.DatabaseConfiguration
		fetchedCluster.cluster.Spec.SeedConnectionString = seedConnection
		log.Printf("update cluster: %s/%s", curCluster.cluster.Namespace, curCluster.cluster.Name)
		fetchedCluster.UpdateClusterSpec()
		if err != nil {
			return err
		}
	}

	return haFdbCluster.addCluster(fetchedCluster)
}

func (factory *Factory) ensureHAFdbClusterExists(
	dcIDs []string,
	namespaces []string,
	processes map[fdbv1beta2.ProcessClass]fdbv1beta2.ProcessSettings,
	databaseConfiguration fdbv1beta2.DatabaseConfiguration,
	mainContainerOverrides fdbv1beta2.ContainerOverrides,
	sidecarContainerOverrides fdbv1beta2.ContainerOverrides,
	config *ClusterConfig,
	options []ClusterOption,
) (*HaFdbCluster, error) {
	fdb := &HaFdbCluster{}
	clusterPrefix := factory.getClusterPrefix()

	initialDatabaseConfiguration := databaseConfiguration.DeepCopy()
	initialDatabaseConfiguration.Regions = []fdbv1beta2.Region{
		{
			DataCenters: []fdbv1beta2.DataCenter{
				{
					ID: dcIDs[0],
				},
			},
		},
	}

	log.Printf("ensureHAFDBClusterExists namespaces=%s", namespaces)
	err := factory.ensureHaMemberClusterExists(
		fdb,
		dcIDs[0],
		namespaces[0],
		clusterPrefix,
		"",
		processes,
		initialDatabaseConfiguration,
		mainContainerOverrides,
		sidecarContainerOverrides,
		options,
	)
	if err != nil {
		return nil, err
	}
	err = fdb.WaitForReconciliation(CreationTrackerLoggerOption(config.CreationTracker))
	log.Printf("primary cluster is reconciled in namespaces=%s", namespaces)
	if err != nil {
		return nil, err
	}
	cluster, err := factory.getClusterStatus(fdb.GetPrimary().Name(), fdb.GetPrimary().Namespace())
	if err != nil {
		return nil, err
	}

	for idx := range dcIDs {
		err = factory.ensureHaMemberClusterExists(
			fdb,
			dcIDs[idx],
			namespaces[idx],
			clusterPrefix,
			cluster.Status.ConnectionString,
			processes,
			&databaseConfiguration,
			mainContainerOverrides,
			sidecarContainerOverrides,
			options,
		)
		if err != nil {
			return nil, err
		}
	}

	// Wait until clusters are ready
	err = fdb.WaitForReconciliation(CreationTrackerLoggerOption(config.CreationTracker))
	if err != nil {
		return nil, err
	}

	config.CreationCallback(fdb.GetPrimary())

	return fdb, nil
}

// GetDcIDsFromConfig returns  unique DC IDs from the current config.
// TODO (johscheuer): Should this be part of v1beta2?
func GetDcIDsFromConfig(databaseConfiguration fdbv1beta2.DatabaseConfiguration) []string {
	dcSet := map[string]struct{}{}
	dcIDs := make([]string, 0)

	for _, region := range databaseConfiguration.Regions {
		for _, dc := range region.DataCenters {
			if _, ok := dcSet[dc.ID]; ok {
				continue
			}
			dcSet[dc.ID] = struct{}{}

			dcIDs = append(dcIDs, dc.ID)
		}
	}

	return dcIDs
}

// UseVersionBeforeUpgrade is an option that uses an older version of FDB to prepare a
// cluster for being upgraded.
func UseVersionBeforeUpgrade(factory *Factory, cluster *fdbv1beta2.FoundationDBCluster) {
	cluster.Spec.Version = factory.GetBeforeVersion()
}

// WithTLSEnabled is an option that enables TLS for a cluster.
func WithTLSEnabled(_ *Factory, cluster *fdbv1beta2.FoundationDBCluster) {
	cluster.Spec.MainContainer.EnableTLS = true
	cluster.Spec.SidecarContainer.EnableTLS = true
}

// WithDNSEnabled is an option that enables DNS for a cluster.
func WithDNSEnabled(_ *Factory, cluster *fdbv1beta2.FoundationDBCluster) {
	cluster.Spec.Routing.UseDNSInClusterFile = pointer.Bool(true)
}

// WithOneMinuteMinimumUptimeSecondsForBounce sets the MinimumUptimeSecondsForBounce setting to 60.
func WithOneMinuteMinimumUptimeSecondsForBounce(
	_ *Factory,
	cluster *fdbv1beta2.FoundationDBCluster,
) {
	cluster.Spec.MinimumUptimeSecondsForBounce = 60
}

// WithLocalitiesForExclusion is an option that exclusions based on localities for a cluster.
func WithLocalitiesForExclusion(_ *Factory, cluster *fdbv1beta2.FoundationDBCluster) {
	cluster.Spec.AutomationOptions.UseLocalitiesForExclusion = pointer.Bool(true)
}

// WithLUnifiedImage is an option that enables the unified image for a cluster.
func WithLUnifiedImage(_ *Factory, cluster *fdbv1beta2.FoundationDBCluster) {
	cluster.Spec.UseUnifiedImage = pointer.Bool(true)
}
