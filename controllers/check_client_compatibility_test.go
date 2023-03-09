/*
 * check_client_compatibility_test.go
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

package controllers

import (
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// generateSupportedVersion will generate the slice of fdbv1beta2.FoundationDBStatusSupportedVersion the assumption for this method is that all keys in maxProtocolVersions
// are also keys in versions.
func generateSupportedVersion(versions map[string][]string, maxProtocolVersions map[string][]string) []fdbv1beta2.FoundationDBStatusSupportedVersion {
	supportedVersions := make([]fdbv1beta2.FoundationDBStatusSupportedVersion, 0, len(versions))
	for version, addresses := range versions {
		protocolClients := make([]fdbv1beta2.FoundationDBStatusConnectedClient, 0, len(addresses))
		for _, address := range addresses {
			protocolClients = append(protocolClients, fdbv1beta2.FoundationDBStatusConnectedClient{
				Address: address,
			})
		}

		parsedVersion, err := fdbv1beta2.ParseFdbVersion(version)
		Expect(err).NotTo(HaveOccurred())

		maxProtocolClients := make([]fdbv1beta2.FoundationDBStatusConnectedClient, 0, len(addresses))
		if maxProtocolAddresses, ok := maxProtocolVersions[version]; ok {
			for _, addr := range maxProtocolAddresses {
				maxProtocolClients = append(maxProtocolClients, fdbv1beta2.FoundationDBStatusConnectedClient{
					Address: addr,
				})
			}
		}

		supportedVersions = append(supportedVersions, fdbv1beta2.FoundationDBStatusSupportedVersion{
			ClientVersion:      version,
			ProtocolVersion:    parsedVersion.Compact(),
			ConnectedClients:   protocolClients,
			MaxProtocolClients: maxProtocolClients,
			Count:              len(protocolClients),
		})
	}

	return supportedVersions
}

var _ = Describe("checkClientCompatibility", func() {
	When("getting the list of unsupported clients", func() {
		var supportedVersions []fdbv1beta2.FoundationDBStatusSupportedVersion
		var unsupportedClients []string
		var compatibleClientsCount int
		var version fdbv1beta2.Version
		var protocolVersion string
		var inputSupportedVersions map[string][]string

		JustBeforeEach(func() {
			var err error
			unsupportedClients, compatibleClientsCount, err = getUnsupportedClientsAndCheckIfCompatibleClientsAreIncluded(supportedVersions, protocolVersion, version)
			Expect(err).NotTo(HaveOccurred())
		})

		BeforeEach(func() {
			inputSupportedVersions = map[string][]string{
				"7.1.27": {
					"192.168.0.1:4500",
					"192.168.0.2:4500",
					"192.168.0.3:4500",
				},
				"6.3.25": {
					"192.168.0.1:4500",
					"192.168.0.2:4500",
					"192.168.0.3:4500",
				},
				"6.2.23": {
					"192.168.0.1:4500",
					"192.168.0.2:4500",
					"192.168.0.3:4500",
				},
				"6.1.23": {
					"192.168.0.1:4500",
					"192.168.0.2:4500",
					"192.168.0.3:4500",
				},
			}
		})

		When("checking for the latest reported version", func() {
			BeforeEach(func() {
				version = fdbv1beta2.Version{Major: 7, Minor: 1, Patch: 27}
				protocolVersion = version.Compact()
			})

			When("all clients support the latest version", func() {
				BeforeEach(func() {
					supportedVersions = generateSupportedVersion(inputSupportedVersions, map[string][]string{
						version.String(): {
							"192.168.0.1:4500",
							"192.168.0.2:4500",
							"192.168.0.3:4500",
						},
					})
				})

				It("should return an empty list of unsupported clients", func() {
					Expect(unsupportedClients).To(BeEmpty())
					Expect(compatibleClientsCount).To(BeNumerically("==", 3))
				})
			})

			When("one client doesn't support the latest version", func() {
				BeforeEach(func() {
					supportedVersions = generateSupportedVersion(inputSupportedVersions, map[string][]string{
						version.String(): {
							"192.168.0.1:4500",
							"192.168.0.2:4500",
						},
						"6.3.25": {
							"192.168.0.3:4500",
						},
					})
				})

				It("should return an empty list of unsupported clients", func() {
					Expect(unsupportedClients).To(ContainElements("192.168.0.3:4500"))
					Expect(unsupportedClients).To(HaveLen(1))
					Expect(compatibleClientsCount).To(BeNumerically("==", 3))
				})
			})
		})

		When("checking for a version that is not the latest version", func() {
			BeforeEach(func() {
				version = fdbv1beta2.Version{Major: 6, Minor: 3, Patch: 25}
				protocolVersion = version.Compact()
			})

			When("all clients support an even newer version", func() {
				BeforeEach(func() {
					supportedVersions = generateSupportedVersion(inputSupportedVersions, map[string][]string{
						"7.1.27": {
							"192.168.0.1:4500",
							"192.168.0.2:4500",
							"192.168.0.3:4500",
						},
					})
				})

				It("should return an empty list of unsupported clients", func() {
					Expect(unsupportedClients).To(BeEmpty())
					Expect(compatibleClientsCount).To(BeNumerically("==", 3))
				})
			})

			When("all clients support an even newer version but not support the requested version", func() {
				BeforeEach(func() {
					delete(inputSupportedVersions, version.String())
					supportedVersions = generateSupportedVersion(inputSupportedVersions, map[string][]string{
						"7.1.27": {
							"192.168.0.1:4500",
							"192.168.0.2:4500",
							"192.168.0.3:4500",
						},
					})

				})

				It("should return an empty list of unsupported clients", func() {
					Expect(unsupportedClients).To(BeEmpty())
					Expect(compatibleClientsCount).To(BeNumerically("==", 0))
				})
			})

			When("all clients support an even newer version but one client doesn't support the requested version", func() {
				BeforeEach(func() {
					inputSupportedVersions[version.String()] = inputSupportedVersions[version.String()][:2]
					supportedVersions = generateSupportedVersion(inputSupportedVersions, map[string][]string{
						"7.1.27": {
							"192.168.0.1:4500",
							"192.168.0.2:4500",
							"192.168.0.3:4500",
						},
					})

				})

				It("should return an empty list of unsupported clients", func() {
					Expect(unsupportedClients).To(BeEmpty())
					Expect(compatibleClientsCount).To(BeNumerically("==", 2))
				})
			})
		})
	})
})
