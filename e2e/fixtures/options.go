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
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"strings"
)

// FactoryOptions defines the (command line) options that are support for the e2e test cases.
type FactoryOptions struct {
	namespace                   string
	chaosNamespace              string
	context                     string
	fdbImage                    string // TODO (johscheuer): Make this optional if we use the default
	sidecarImage                string // TODO (johscheuer): Make this optional if we use the default
	operatorImage               string
	registry                    string
	fdbVersion                  string
	username                    string
	storageClass                string
	upgradeString               string
	cloudProvider               string
	enableChaosTests            bool
	cleanup                     bool
	featureOperatorDNS          bool
	featureOperatorLocalities   bool
	featureOperatorUnifiedImage bool
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
		"",
		"defines the FoundationDB image that should be used for testing",
	)
	fs.StringVar(
		&options.sidecarImage,
		"sidecar-image",
		"",
		"defines the FoundationDB sidecar image that should be used for testing",
	)
	fs.StringVar(
		&options.operatorImage,
		"operator-image",
		"",
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
	fs.BoolVar(
		&options.enableChaosTests,
		"enable-chaos-tests",
		true,
		"defines if the chaos tests should be enabled in operator related tests cases",
	)
	fs.BoolVar(
		&options.cleanup,
		"cleanup",
		true,
		"defines if the test namespace should be removed after the test run",
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

	return nil
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
