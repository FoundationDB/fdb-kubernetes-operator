/*
 * options.go
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
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
)

// FactoryOptions defines the (command line) options that are support for the e2e test cases.
type FactoryOptions struct {
	namespace                      string
	chaosNamespace                 string
	context                        string
	fdbImage                       string
	unifiedFDBImage                string
	sidecarImage                   string
	operatorImage                  string
	seaweedFSImage                 string
	dataLoaderImage                string
	registry                       string
	fdbVersion                     string
	username                       string
	storageClass                   string
	upgradeString                  string
	cloudProvider                  string
	clusterName                    string
	storageEngine                  string
	fdbVersionTagMapping           string
	synchronizationMode            string
	nodeSelector                   string
	enableChaosTests               bool
	enableDataLoading              bool
	cleanup                        bool
	featureOperatorDNS             bool
	featureOperatorLocalities      bool
	featureOperatorUnifiedImage    bool
	featureOperatorServerSideApply bool
	featureManagementAPI           bool
	dumpOperatorState              bool
	defaultUnavailableThreshold    time.Duration
}

// BindFlags binds the FactoryOptions flags to the provided FlagSet. This can be used to extend the current test setup
// with custom/additional flags.
func (options *FactoryOptions) BindFlags(fs *flag.FlagSet) {
	fs.StringVar(
		&options.namespace,
		"namespace",
		"",
		"defines the namespace to run the test (will be created if missing)",
	)
	fs.StringVar(
		&options.chaosNamespace,
		"chaos-namespace",
		"",
		"defines the chaos namespace to run experiments (will be created if missing)",
	)
	fs.StringVar(
		&options.context,
		"context",
		"",
		"defines the Kubernetes context to run the test (will use the active context if missing)",
	)
	fs.StringVar(
		&options.fdbImage,
		"fdb-image",
		fdbv1beta2.FoundationDBBaseImage,
		"defines the FoundationDB image that should be used for testing",
	)
	fs.StringVar(
		&options.unifiedFDBImage,
		"unified-fdb-image",
		fdbv1beta2.FoundationDBKubernetesBaseImage,
		"defines the unified FoundationDB image that should be used for testing",
	)
	fs.StringVar(
		&options.sidecarImage,
		"sidecar-image",
		fdbv1beta2.FoundationDBSidecarBaseImage,
		"defines the FoundationDB sidecar image that should be used for testing",
	)
	fs.StringVar(
		&options.operatorImage,
		"operator-image",
		"foundationdb/fdb-kubernetes-operator",
		"defines the Kubernetes Operator image that should be used for testing",
	)
	fs.StringVar(
		&options.registry,
		"registry",
		"",
		"specify the name of the container registry to be used when downloading images",
	)
	fs.StringVar(
		&options.fdbVersion,
		"fdb-version",
		"",
		"overrides the version number of the FoundationDB image that is used for testing. Mandatory unless the FoundationDB image tag is of the form 'x.y.z'",
	)
	fs.StringVar(
		&options.username,
		"username",
		"",
		"username to tag kubernetes resources with (defaults to $USER)",
	)
	fs.StringVar(
		&options.storageClass,
		"storage-class",
		"gp3-std",
		"defines the 'performant' storage class to use for FDB benchmarks and migration tests",
	)
	fs.StringVar(
		&options.upgradeString,
		"upgrade-versions",
		"",
		"defines the upgrade version pairs for upgrade tests.",
	)
	fs.StringVar(
		&options.cloudProvider,
		"cloud-provider",
		"",
		"defines the cloud provider used for the Kubernetes cluster, for defining cloud provider specific behaviour. Currently only Kind is support.",
	)
	fs.StringVar(
		&options.dataLoaderImage,
		"data-loader-image",
		"foundationdb/fdb-data-loader:latest",
		"defines the data loader image that should be used for testing",
	)
	fs.StringVar(
		&options.storageEngine,
		"storage-engine",
		"",
		"defines the storage-engine that should be used by the created FDB cluster.",
	)
	fs.BoolVar(
		&options.enableChaosTests,
		"enable-chaos-tests",
		true,
		"defines if the chaos tests should be enabled in operator related tests cases.",
	)
	fs.BoolVar(
		&options.enableDataLoading,
		"enable-data-load",
		true,
		"defines if the created FDB cluster should be loaded with some random data.",
	)
	fs.BoolVar(
		&options.cleanup,
		"cleanup",
		true,
		"defines if the test namespace should be removed after the test run.",
	)
	fs.BoolVar(
		&options.featureOperatorUnifiedImage,
		"feature-unified-image",
		false,
		"defines if the operator tests should make use of the unified image.",
	)
	fs.BoolVar(
		&options.featureOperatorLocalities,
		"feature-localities",
		false,
		"defines if the operator tests should make use of localities for exclusions.",
	)
	fs.BoolVar(
		&options.featureOperatorDNS,
		"feature-dns",
		false,
		"defines if the operator tests should make use of DNS in cluster files.",
	)
	fs.BoolVar(
		&options.dumpOperatorState,
		"dump-operator-state",
		true,
		"defines if the operator tests should print the state of the cluster and the according Pods for better debugging.",
	)
	fs.BoolVar(
		&options.featureOperatorServerSideApply,
		"feature-server-side-apply",
		false,
		"defines if the operator should make use of server side apply.",
	)
	fs.BoolVar(
		&options.featureManagementAPI,
		"feature-management-api",
		false,
		"defines if the operator should make use of the management API",
	)
	fs.StringVar(
		&options.clusterName,
		"cluster-name",
		"",
		"if defined, the test suite will create a cluster with the specified name or update the setting of an existing cluster."+
			"For multi-region clusters, this will define the prefix for all clusters.",
	)
	fs.StringVar(
		&options.fdbVersionTagMapping,
		"fdb-version-tag-mapping",
		"",
		"if defined, the test suite will use this information to map the image tag to the specified version. Multiple entries can be"+
			"provided by separating them with a \",\". The mapping must have the format $version:$tag, e.g. 7.1.57:7.1.57-testing."+
			"This option will only work for the main container with the split image (sidecar).",
	)
	fs.StringVar(
		&options.seaweedFSImage,
		"seaweedfs-image",
		"chrislusf/seaweedfs:3.73",
		"defines the seaweedfs image that should be used for testing. SeaweedFS is used for backup and restore testing to spin up a S3 compatible blobstore.",
	)
	fs.StringVar(
		&options.nodeSelector,
		"node-selector",
		"",
		"if defined, specifies a Kubernetes node selector for the FDB cluster in the format key=value",
	)
	fs.StringVar(
		&options.synchronizationMode,
		"feature-synchronization-mode",
		"local",
		"defines the synchronization mode that should be used. Only applies for multi-region clusters.",
	)
	fs.DurationVar(
		&options.defaultUnavailableThreshold,
		"default-unavailable-threshold",
		30*time.Second,
		"defines the default unavailability threshold. If the database is unavailable for a longer period the test will fail if unavailability checks are enabled.",
	)
}

func (options *FactoryOptions) validateFlags() error {
	if options.fdbImage == "" {
		return errors.New("no fdb image is supplied")
	}

	if options.enableChaosTests && options.chaosNamespace == "" {
		return errors.New("no chaos testing namespace provided but chaos tests are enabled")
	}

	if options.sidecarImage == "" {
		return errors.New(
			"no fdb sidecar image is supplied",
		)
	}

	if options.operatorImage == "" {
		return errors.New(
			"no fdb operator image supplied for testing",
		)
	}

	versionFlags := [][]string{
		{"fdbVersion", options.fdbVersion},
	}

	for _, flagData := range versionFlags {
		err := validateVersion(flagData[0], flagData[1])
		if err != nil {
			return err
		}
	}

	upgradeString := options.upgradeString
	if upgradeString == "" {
		return nil
	}

	for _, upgradeTest := range strings.Split(upgradeString, ",") {
		versions := strings.Split(upgradeTest, ":")
		if len(versions) != 2 {
			return fmt.Errorf(
				"expected to have two versions for upgrade string separated by \":\" got: \"%s\"",
				upgradeTest,
			)
		}

		err := validateVersion("beforeVersion", versions[0])
		if err != nil {
			return err
		}

		err = validateVersion("targetVersion", versions[1])
		if err != nil {
			return err
		}
	}

	// Make sure we handle the cloud provider string internally as lower cases.
	if options.cloudProvider != "" {
		options.cloudProvider = strings.ToLower(options.cloudProvider)
	}

	err := options.validateNodeSelector()
	if err != nil {
		return err
	}

	err = options.validateSynchronizationMode()
	if err != nil {
		return err
	}

	return options.validateFDBVersionTagMapping()
}

func (options *FactoryOptions) validateSynchronizationMode() error {
	if options.synchronizationMode == "" {
		options.synchronizationMode = string(fdbv1beta2.SynchronizationModeLocal)
		return nil
	}

	options.synchronizationMode = strings.ToLower(options.synchronizationMode)
	mode := fdbv1beta2.SynchronizationMode(options.synchronizationMode)
	if mode == fdbv1beta2.SynchronizationModeGlobal || mode == fdbv1beta2.SynchronizationModeLocal {
		return nil
	}

	return fmt.Errorf(
		"synchronizationMode must be either \"local\" or \"global\", got :\"%s\"",
		mode,
	)
}

func validateVersion(label string, version string) error {
	if version == "" {
		return fmt.Errorf("no fdb version supplied in %s", label)
	}

	parsedVersion, err := fdbv1beta2.ParseFdbVersion(version)
	if err != nil {
		return fmt.Errorf("could not parse %s with error %w", label, err)
	}

	if parsedVersion.String() != version {
		return fmt.Errorf(
			"expected that parsed version %s matches input version %s",
			parsedVersion.String(),
			version,
		)
	}

	return nil
}

func (options *FactoryOptions) validateFDBVersionTagMapping() error {
	if options.fdbVersionTagMapping == "" {
		return nil
	}

	mappings := strings.Split(options.fdbVersionTagMapping, ",")
	for _, mapping := range mappings {
		versionMapping := strings.Split(mapping, ":")
		if len(versionMapping) != 2 {
			return fmt.Errorf(
				"mapping %s is invalid, expected format is $version:$tag",
				versionMapping,
			)
		}
	}

	return nil
}

func (options *FactoryOptions) validateNodeSelector() error {
	if options.nodeSelector == "" {
		return nil
	}
	splitSelector := strings.Split(options.nodeSelector, "=")
	if len(splitSelector) != 2 {
		return fmt.Errorf("node selector must have format key=value, got: %s", options.nodeSelector)
	}
	return nil
}

// getTagSuffix returns "-1" if the tag suffix should be used for a sidecar image.
func getTagSuffix(isSidecar bool, debugSymbols bool) string {
	var tag strings.Builder

	// The sidecar has per default the -1 suffix.
	if isSidecar {
		tag.WriteString("-1")
	}

	// If the debug symbols should be used add the debug suffix.
	if debugSymbols {
		tag.WriteString("-debug")
	}

	return tag.String()
}

// getTagWithSuffix returns the tag with the required suffix if the image is needed for the sidecar image.
func getTagWithSuffix(tag string, isSidecar bool, debugSymbols bool) string {
	tagSuffix := getTagSuffix(isSidecar, debugSymbols)
	// Suffix is already present, so we don't have to add it again.
	if strings.HasSuffix(tag, tagSuffix) || tagSuffix == "" {
		return tag
	}

	return tag + tagSuffix
}

func (options *FactoryOptions) getImageVersionConfig(
	baseImage string,
	versionTag string,
	isSidecar bool,
	debugSymbols bool,
) []fdbv1beta2.ImageConfig {
	if options.fdbVersionTagMapping == "" {
		var versionMapping fdbv1beta2.ImageConfig

		if versionTag != "" {
			versionMapping = fdbv1beta2.ImageConfig{
				BaseImage: baseImage,
				Version:   options.fdbVersion,
				// TODO should this really be false? If yes we should add a comment
				Tag: getTagWithSuffix(versionTag, false, debugSymbols),
			}
		} else {
			versionMapping = fdbv1beta2.ImageConfig{
				BaseImage: baseImage,
				Version:   options.fdbVersion,
				TagSuffix: getTagSuffix(isSidecar, debugSymbols),
			}
		}

		return []fdbv1beta2.ImageConfig{
			versionMapping,
			{
				BaseImage: baseImage,
				TagSuffix: getTagSuffix(isSidecar, debugSymbols),
			},
		}
	}

	mappings := strings.Split(options.fdbVersionTagMapping, ",")
	imageConfig := make([]fdbv1beta2.ImageConfig, len(mappings)+1)
	for idx, mapping := range mappings {
		versionMapping := strings.Split(mapping, ":")

		imageConfig[idx] = fdbv1beta2.ImageConfig{
			BaseImage: baseImage,
			Version:   strings.TrimSpace(versionMapping[0]),
			Tag: getTagWithSuffix(
				strings.TrimSpace(versionMapping[1]),
				isSidecar,
				debugSymbols,
			),
		}
	}

	// Always add the base image config to make sure that the default images can be used, even if only a subset
	// of versions use a version tag mapping.
	imageConfig[len(mappings)] = fdbv1beta2.ImageConfig{
		BaseImage: baseImage,
		TagSuffix: getTagSuffix(isSidecar, debugSymbols),
	}

	return imageConfig
}
