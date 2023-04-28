/*
 * upgrade_test_configuration.go
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
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/onsi/ginkgo/v2"
	"log"
	"strings"
)

// UpgradeTestConfiguration represents the configuration for an upgrade test. This includes the initial FoundationDB version
// and the target FoundationDB version to upgrade the cluster to.
type UpgradeTestConfiguration struct {
	// InitialVersion represents the version before the upgrade.
	InitialVersion fdbv1beta2.Version
	// TargetVersion represents the version to upgrade to.
	TargetVersion fdbv1beta2.Version
}

func parseUpgradeVersionPair(upgradeConfig string) UpgradeTestConfiguration {
	versions := strings.Split(upgradeConfig, ":")
	if len(versions) != 2 {
		log.Fatalf(
			"expected to have two versions for upgrade string separated by \":\" got: \"%s\"",
			upgradeConfig,
		)
	}

	initialVersion, err := fdbv1beta2.ParseFdbVersion(versions[0])
	if err != nil {
		log.Fatalf("\"%s\" is not a valid FDB version", versions[0])
	}

	targetVersion, err := fdbv1beta2.ParseFdbVersion(versions[1])
	if err != nil {
		log.Fatalf("\"%s\" is not a valid FDB version", versions[1])
	}

	if initialVersion.IsAtLeast(targetVersion) {
		log.Fatalf("downgrade from \"%s\" to \"%s\" is not supported", versions[0], versions[1])
	}

	return UpgradeTestConfiguration{
		InitialVersion: initialVersion,
		TargetVersion:  targetVersion,
	}
}

// GetUpgradeVersions returns the upgrade versions as a string slice based on the command line flag. Each entry will be
// a FoundationDB version. This slice can contain duplicate entries. For upgrade tests it's expected that two versions
// form one test, e.g. where the odd number is the initial version and the even number is the
func (factory *Factory) GetUpgradeVersions() []UpgradeTestConfiguration {
	return getUpgradeVersions(factory.options.upgradeString)
}

// getUpgradeVersions returns the upgrade versions as a string slice based on the command line flag. Each entry will be
// a FoundationDB version. This slice can contain duplicate entries. For upgrade tests it's expected that two versions
// form one test, e.g. where the odd number is the initial version and the even number is the
// This method is only internally used. Users that import this test suite should use the factory method.
func getUpgradeVersions(upgradeString string) []UpgradeTestConfiguration {
	if upgradeString == "" {
		return nil
	}

	upgradeVersionStrings := strings.Split(upgradeString, ",")
	upgradeVersions := make([]UpgradeTestConfiguration, 0, len(upgradeVersionStrings))
	for _, upgradeTest := range upgradeVersionStrings {
		upgradeVersions = append(upgradeVersions, parseUpgradeVersionPair(upgradeTest))
	}

	return upgradeVersions
}

// GenerateUpgradeTableEntries creates the ginkgo.TableEntry slice based of the provided options.
func GenerateUpgradeTableEntries(options *FactoryOptions) []ginkgo.TableEntry {
	upgradeString := options.upgradeString
	if upgradeString == "" {
		return nil
	}

	upgradeTests := strings.Split(upgradeString, ",")

	tests := make([]ginkgo.TableEntry, 0, len(upgradeTests))
	for _, upgradeTest := range getUpgradeVersions(upgradeString) {
		tests = append(tests, ginkgo.Entry(nil, upgradeTest.InitialVersion.String(), upgradeTest.TargetVersion.String()))
	}

	return tests
}
