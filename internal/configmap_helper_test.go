/*
 * configmap_helper_test.go
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
	"encoding/json"
	"fmt"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	monitorapi "github.com/apple/foundationdb/fdbkubernetesmonitor/api"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("configmap_helper", func() {
	var cluster *fdbv1beta2.FoundationDBCluster
	var fakeConnectionString string
	var err error

	BeforeEach(func() {
		cluster = CreateDefaultCluster()
		err = NormalizeClusterSpec(cluster, DeprecationOptions{})
		cluster.Status.ImageTypes = []fdbv1beta2.ImageType{"split"}
		Expect(err).NotTo(HaveOccurred())
		fakeConnectionString = "operator-test:asdfasf@127.0.0.1:4501"
	})

	Describe("GetConfigMap", func() {
		var configMap *corev1.ConfigMap
		var err error

		BeforeEach(func() {
			cluster.Status.ConnectionString = fakeConnectionString
			cluster.Status.RunningVersion = cluster.Spec.Version
			err = NormalizeClusterSpec(cluster, DeprecationOptions{})
			Expect(err).NotTo(HaveOccurred())
		})

		JustBeforeEach(func() {
			configMap, err = GetConfigMap(cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		Context("with a basic cluster", func() {
			It("should populate the metadata", func() {
				Expect(configMap.Namespace).To(Equal("my-ns"))
				Expect(configMap.Name).To(Equal(fmt.Sprintf("%s-config", cluster.Name)))
				Expect(configMap.Labels).To(Equal(map[string]string{
					fdbv1beta2.FDBClusterLabel: cluster.Name,
				}))
				Expect(configMap.Annotations).To(BeNil())
			})

			It("should have the basic files", func() {
				expectedConf, err := GetMonitorConf(cluster, fdbv1beta2.ProcessClassStorage, nil, 1)
				Expect(err).NotTo(HaveOccurred())

				Expect(configMap.Data[ClusterFileKey]).To(Equal("operator-test:asdfasf@127.0.0.1:4501"))
				Expect(configMap.Data["fdbmonitor-conf-storage"]).To(Equal(expectedConf))
				Expect(configMap.Data["running-version"]).To(Equal(fdbv1beta2.Versions.Default.String()))
				Expect(configMap.Data["sidecar-conf"]).To(Equal(""))
			})
		})

		When("only the unified image is enabled", func() {
			BeforeEach(func() {
				cluster.Status.ImageTypes = []fdbv1beta2.ImageType{"unified"}
			})

			It("includes the data for the unified monitor conf", func() {
				jsonData, present := configMap.Data["fdbmonitor-conf-storage-json"]
				Expect(present).To(BeTrue())
				config := monitorapi.ProcessConfiguration{}
				err = json.Unmarshal([]byte(jsonData), &config)
				Expect(err).NotTo(HaveOccurred())
				expectedConfig, err := GetMonitorProcessConfiguration(cluster, fdbv1beta2.ProcessClassStorage, 1, FDBImageTypeUnified, nil)
				Expect(err).NotTo(HaveOccurred())
				Expect(config).To(Equal(expectedConfig))
			})

			It("does not include the data for the split monitor conf", func() {
				_, present := configMap.Data["fdbmonitor-conf-storage"]
				Expect(present).To(BeFalse())
			})
		})

		When("only the split image is enabled", func() {
			BeforeEach(func() {
				cluster.Status.ImageTypes = []fdbv1beta2.ImageType{"split"}
			})

			It("includes the data for the split monitor conf", func() {
				expectedConf, err := GetMonitorConf(cluster, fdbv1beta2.ProcessClassStorage, nil, 1)
				Expect(err).NotTo(HaveOccurred())
				Expect(configMap.Data["fdbmonitor-conf-storage"]).To(Equal(expectedConf))
			})

			It("does not include the data for the unified monitor conf", func() {
				_, present := configMap.Data["fdbmonitor-conf-storage-json"]
				Expect(present).To(BeFalse())
			})
		})

		When("both image types are enabled", func() {
			BeforeEach(func() {
				cluster.Status.ImageTypes = []fdbv1beta2.ImageType{"split", "unified"}
			})

			It("includes the data for the both images", func() {
				_, present := configMap.Data["fdbmonitor-conf-storage-json"]
				Expect(present).To(BeTrue())
				_, present = configMap.Data["fdbmonitor-conf-storage"]
				Expect(present).To(BeTrue())
			})
		})

		Context("with multiple storage servers per disk", func() {
			BeforeEach(func() {
				cluster.Status.StorageServersPerDisk = []int{1, 2}
			})

			When("using the split image", func() {
				BeforeEach(func() {
					cluster.Status.ImageTypes = []fdbv1beta2.ImageType{"split"}
				})
				It("includes the data for both configurations", func() {
					expectedConf, err := GetMonitorConf(cluster, fdbv1beta2.ProcessClassStorage, nil, 1)
					Expect(err).NotTo(HaveOccurred())
					Expect(configMap.Data["fdbmonitor-conf-storage"]).To(Equal(expectedConf))

					expectedConf, err = GetMonitorConf(cluster, fdbv1beta2.ProcessClassStorage, nil, 2)
					Expect(err).NotTo(HaveOccurred())
					Expect(configMap.Data["fdbmonitor-conf-storage-density-2"]).To(Equal(expectedConf))
				})
			})

			When("using the unified image", func() {
				BeforeEach(func() {
					cluster.Status.ImageTypes = []fdbv1beta2.ImageType{"unified"}
				})

				It("includes the data for both configurations", func() {
					jsonData, present := configMap.Data["fdbmonitor-conf-storage-json"]
					Expect(present).To(BeTrue())
					config := monitorapi.ProcessConfiguration{}
					err = json.Unmarshal([]byte(jsonData), &config)
					Expect(err).NotTo(HaveOccurred())
					expectedConfig, err := GetMonitorProcessConfiguration(cluster, fdbv1beta2.ProcessClassStorage, 1, FDBImageTypeUnified, nil)
					Expect(err).NotTo(HaveOccurred())
					Expect(config).To(Equal(expectedConfig))

					jsonData, present = configMap.Data["fdbmonitor-conf-storage-json-multiple"]
					Expect(present).To(BeTrue())
					config = monitorapi.ProcessConfiguration{}
					err = json.Unmarshal([]byte(jsonData), &config)
					Expect(err).NotTo(HaveOccurred())
					expectedConfig, err = GetMonitorProcessConfiguration(cluster, fdbv1beta2.ProcessClassStorage, 2, FDBImageTypeUnified, nil)
					Expect(err).NotTo(HaveOccurred())
					Expect(config).To(Equal(expectedConfig))
				})

			})
		})

		Context("with multiple log servers per disk", func() {
			BeforeEach(func() {
				cluster.Status.LogServersPerDisk = []int{1, 2}
			})

			When("using the split image", func() {
				BeforeEach(func() {
					cluster.Status.ImageTypes = []fdbv1beta2.ImageType{"split"}
				})
				It("includes the data for both configurations", func() {
					expectedConf, err := GetMonitorConf(cluster, fdbv1beta2.ProcessClassLog, nil, 1)
					Expect(err).NotTo(HaveOccurred())
					Expect(configMap.Data["fdbmonitor-conf-log"]).To(Equal(expectedConf))

					expectedConf, err = GetMonitorConf(cluster, fdbv1beta2.ProcessClassLog, nil, 2)
					Expect(err).NotTo(HaveOccurred())
					Expect(configMap.Data["fdbmonitor-conf-log-density-2"]).To(Equal(expectedConf))
				})
			})

			When("using the unified image", func() {
				BeforeEach(func() {
					cluster.Status.ImageTypes = []fdbv1beta2.ImageType{"unified"}
				})

				It("includes the data for both configurations", func() {
					jsonData, present := configMap.Data["fdbmonitor-conf-log-json"]
					Expect(present).To(BeTrue())
					config := monitorapi.ProcessConfiguration{}
					err = json.Unmarshal([]byte(jsonData), &config)
					Expect(err).NotTo(HaveOccurred())
					expectedConfig, err := GetMonitorProcessConfiguration(cluster, fdbv1beta2.ProcessClassLog, 1, FDBImageTypeUnified, nil)
					Expect(err).NotTo(HaveOccurred())
					Expect(config).To(Equal(expectedConfig))

					jsonData, present = configMap.Data["fdbmonitor-conf-log-json-multiple"]
					Expect(present).To(BeTrue())
					config = monitorapi.ProcessConfiguration{}
					err = json.Unmarshal([]byte(jsonData), &config)
					Expect(err).NotTo(HaveOccurred())
					expectedConfig, err = GetMonitorProcessConfiguration(cluster, fdbv1beta2.ProcessClassLog, 2, FDBImageTypeUnified, nil)
					Expect(err).NotTo(HaveOccurred())
					Expect(config).To(Equal(expectedConfig))
				})

			})
		})

		Context("with custom resource labels", func() {
			BeforeEach(func() {
				cluster.Spec.LabelConfig = fdbv1beta2.LabelConfig{
					MatchLabels:    map[string]string{"fdb-custom-name": cluster.Name, "fdb-managed-by-operator": "true"},
					ResourceLabels: map[string]string{"fdb-new-custom-name": cluster.Name},
				}
			})

			It("should populate the metadata", func() {
				Expect(configMap.Namespace).To(Equal("my-ns"))
				Expect(configMap.Name).To(Equal(fmt.Sprintf("%s-config", cluster.Name)))
				Expect(configMap.Labels).To(Equal(map[string]string{
					"fdb-custom-name":         cluster.Name,
					"fdb-new-custom-name":     cluster.Name,
					"fdb-managed-by-operator": "true",
				}))
				Expect(configMap.Annotations).To(BeNil())
			})
		})

		Context("with a custom CA", func() {
			BeforeEach(func() {
				cluster.Spec.TrustedCAs = []string{
					"-----BEGIN CERTIFICATE-----\nMIIFyDCCA7ACCQDqRnbTl1OkcTANBgkqhkiG9w0BAQsFADCBpTELMAkGA1UEBhMC",
					"---CERT2----",
				}
			})

			Context("with a version that uses sidecar command-line arguments", func() {
				BeforeEach(func() {
					cluster.Status.RunningVersion = fdbv1beta2.Versions.Default.String()
				})

				It("should populate the CA file", func() {
					Expect(configMap.Data["ca-file"]).To(Equal("-----BEGIN CERTIFICATE-----\nMIIFyDCCA7ACCQDqRnbTl1OkcTANBgkqhkiG9w0BAQsFADCBpTELMAkGA1UEBhMC\n---CERT2----"))
				})
			})
		})

		Context("with an empty connection string", func() {
			BeforeEach(func() {
				cluster.Status.ConnectionString = ""
			})

			It("should empty the monitor conf and cluster file", func() {
				Expect(configMap.Data[ClusterFileKey]).To(Equal(""))
				Expect(configMap.Data["fdbmonitor-conf-storage"]).To(Equal(""))
			})
		})

		Context("with a custom label", func() {
			BeforeEach(func() {
				cluster.Spec.ConfigMap = &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"fdb-label": "value1",
						},
					},
				}
			})

			It("should put the label on the config map", func() {
				Expect(configMap.Labels).To(Equal(map[string]string{
					fdbv1beta2.FDBClusterLabel: cluster.Name,
					"fdb-label":                "value1",
				}))
			})
		})

		Context("with a custom annotation", func() {
			BeforeEach(func() {
				cluster.Spec.ConfigMap = &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"fdb-annotation": "value1",
						},
					},
				}
			})

			It("should put the annotation on the config map", func() {
				Expect(configMap.Annotations).To(Equal(map[string]string{
					"fdb-annotation": "value1",
				}))
			})
		})

		Context("with a custom configmap", func() {
			BeforeEach(func() {
				cluster.Spec.ConfigMap = &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name: "name1",
					},
				}
			})

			It("should use the configmap name as suffix", func() {
				Expect(configMap.Name).To(Equal(fmt.Sprintf("%s-%s", cluster.Name, "name1")))
			})
		})

		Context("without a configmap", func() {
			It("should use the default suffix", func() {
				Expect(configMap.Name).To(Equal(fmt.Sprintf("%s-%s", cluster.Name, "config")))
			})
		})

		Context("with configmap having items", func() {
			BeforeEach(func() {
				cluster.Spec.ConfigMap = &corev1.ConfigMap{
					Data: map[string]string{
						"itemKey": "itemVal",
					},
				}
			})

			It("should have items from the clusterSpec", func() {
				Expect(configMap.Data["itemKey"]).To(Equal("itemVal"))
			})
		})
	})
})
