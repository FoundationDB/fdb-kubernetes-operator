package internal

import (
	"fmt"
	"strings"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("[internal] deprecations", func() {
	When("Providing a custom parameter", func() {
		type testCase struct {
			Input              []string
			ExpectedViolations []string
		}

		DescribeTable("should set the expected parameter or print the violation",
			func(tc testCase) {
				err := ValidateCustomParameters(tc.Input)
				var errMsg error
				if len(tc.ExpectedViolations) > 0 {
					errMsg = fmt.Errorf("found the following customParameters violations:\n%s", strings.Join(tc.ExpectedViolations, "\n"))
				}

				if err == nil {
					Expect(len(tc.ExpectedViolations)).To(BeNumerically("==", 0))
				} else {
					Expect(err).To(Equal(errMsg))
				}
			},
			Entry("Valid parameter without duplicate",
				testCase{
					Input:              []string{"blahblah=1"},
					ExpectedViolations: []string{},
				}),
			Entry("Valid parameter with duplicate",
				testCase{
					Input:              []string{"blahblah=1", "blahblah=1"},
					ExpectedViolations: []string{"found duplicated customParameter: blahblah"},
				}),
			Entry("Protected parameter without duplicate",
				testCase{
					Input:              []string{"datadir=1"},
					ExpectedViolations: []string{"found protected customParameter: datadir, please remove this parameter from the customParameters list"},
				}),
			Entry("Valid parameter with duplicate and protected parameter",
				testCase{
					Input:              []string{"blahblah=1", "blahblah=1", "datadir=1"},
					ExpectedViolations: []string{"found duplicated customParameter: blahblah", "found protected customParameter: datadir, please remove this parameter from the customParameters list"},
				}),
		)
	})

	Describe("NormalizeClusterSpec", func() {
		var spec *fdbtypes.FoundationDBClusterSpec
		var err error

		BeforeEach(func() {
			spec = &fdbtypes.FoundationDBClusterSpec{
				Version: fdbtypes.Versions.Default.String(),
			}
		})

		Describe("deprecations", func() {
			JustBeforeEach(func() {
				err := NormalizeClusterSpec(spec, DeprecationOptions{})
				Expect(err).NotTo(HaveOccurred())
			})

			Context("with a custom value for the Spec.PodTemplate field", func() {
				BeforeEach(func() {
					spec.PodTemplate = &corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"fdb-label": "value2",
							},
						},
						Spec: corev1.PodSpec{
							Volumes: []corev1.Volume{{
								Name: "test-secrets",
								VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
									SecretName: "test-secrets",
								}},
							}},
							Containers: []corev1.Container{
								{
									Name: "foundationdb",
									VolumeMounts: []corev1.VolumeMount{{
										Name:      "test-secrets",
										MountPath: "/var/secrets",
									}},
								},
							},
						},
					}
				})

				It("should add the labels to the metadata", func() {
					metadata := spec.Processes[fdbtypes.ProcessClassGeneral].PodTemplate.ObjectMeta
					Expect(metadata.Labels).To(Equal(map[string]string{
						"fdb-label": "value2",
					}))
				})

				It("adds volumes to the process settings", func() {
					podSpec := spec.Processes[fdbtypes.ProcessClassGeneral].PodTemplate.Spec

					mainContainer := podSpec.Containers[0]
					Expect(mainContainer.Name).To(Equal("foundationdb"))
					Expect(mainContainer.VolumeMounts).To(Equal([]corev1.VolumeMount{
						{Name: "test-secrets", MountPath: "/var/secrets"},
					}))

					Expect(len(podSpec.Volumes)).To(Equal(1))
					Expect(podSpec.Volumes[0]).To(Equal(corev1.Volume{
						Name: "test-secrets",
						VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
							SecretName: "test-secrets",
						}},
					}))
				})
			})

			Context("with a custom value for the SidecarVersion field", func() {
				BeforeEach(func() {
					spec.SidecarVersion = 2
				})

				It("puts the value in the SidecarVersions", func() {
					Expect(spec.SidecarVersion).To(Equal(0))
					Expect(spec.SidecarVersions).To(Equal(map[string]int{
						fdbtypes.Versions.Default.String(): 2,
					}))
				})
			})

			Context("with a custom value for the PodLabels field", func() {
				BeforeEach(func() {
					spec.PodLabels = map[string]string{
						"test-label": "test-value",
					}
				})

				It("puts the labels in the pod settings", func() {
					Expect(spec.PodLabels).To(BeNil())
					Expect(spec.Processes[fdbtypes.ProcessClassGeneral].PodTemplate.ObjectMeta.Labels).To(Equal(map[string]string{
						"test-label": "test-value",
					}))
				})

				It("puts the labels in the volume claim settings", func() {
					Expect(spec.PodLabels).To(BeNil())
					Expect(spec.Processes[fdbtypes.ProcessClassGeneral].VolumeClaimTemplate.ObjectMeta.Labels).To(Equal(map[string]string{
						"test-label": "test-value",
					}))
				})

				It("puts the labels in the config map settings", func() {
					Expect(spec.PodLabels).To(BeNil())
					Expect(spec.ConfigMap.ObjectMeta.Labels).To(Equal(map[string]string{
						"test-label": "test-value",
					}))
				})
			})

			Context("with a custom value for the resources field", func() {
				BeforeEach(func() {
					spec.Resources = &corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							"cpu":    resource.MustParse("2"),
							"memory": resource.MustParse("8Gi"),
						},
						Limits: corev1.ResourceList{
							"cpu":    resource.MustParse("4"),
							"memory": resource.MustParse("16Gi"),
						},
					}
				})

				It("should set the resources on the main container", func() {
					mainContainer := spec.Processes[fdbtypes.ProcessClassGeneral].PodTemplate.Spec.Containers[0]
					Expect(mainContainer.Name).To(Equal("foundationdb"))
					Expect(*mainContainer.Resources.Limits.Cpu()).To(Equal(resource.MustParse("4")))
					Expect(*mainContainer.Resources.Limits.Memory()).To(Equal(resource.MustParse("16Gi")))
					Expect(*mainContainer.Resources.Requests.Cpu()).To(Equal(resource.MustParse("2")))
					Expect(*mainContainer.Resources.Requests.Memory()).To(Equal(resource.MustParse("8Gi")))

					Expect(spec.Resources).To(BeNil())
				})
			})

			Context("with a custom value for the InitContainers field", func() {
				BeforeEach(func() {
					spec.InitContainers = []corev1.Container{{
						Name:  "test-container",
						Image: "test-container:latest",
					}}
				})

				It("should add the init container to the process settings", func() {
					initContainers := spec.Processes[fdbtypes.ProcessClassGeneral].PodTemplate.Spec.InitContainers
					Expect(len(initContainers)).To(Equal(2))
					Expect(initContainers[0].Name).To(Equal("test-container"))
					Expect(initContainers[0].Image).To(Equal("test-container:latest"))
					Expect(initContainers[1].Name).To(Equal("foundationdb-kubernetes-init"))
					Expect(initContainers[1].Image).To(Equal(""))

					Expect(spec.InitContainers).To(BeNil())
				})
			})

			Context("with a custom value for the Containers field", func() {
				BeforeEach(func() {
					spec.Containers = []corev1.Container{{
						Name:  "test-container",
						Image: "test-container:latest",
					}}
				})

				It("should add the container to the process settings", func() {
					containers := spec.Processes[fdbtypes.ProcessClassGeneral].PodTemplate.Spec.Containers
					Expect(len(containers)).To(Equal(3))
					Expect(containers[0].Name).To(Equal("foundationdb"))
					Expect(containers[0].Image).To(Equal(""))
					Expect(containers[1].Name).To(Equal("test-container"))
					Expect(containers[1].Image).To(Equal("test-container:latest"))
					Expect(containers[2].Name).To(Equal("foundationdb-kubernetes-sidecar"))
					Expect(containers[2].Image).To(Equal(""))

					Expect(spec.Containers).To(BeNil())
				})
			})

			Context("with a custom value for the Volumes field", func() {
				BeforeEach(func() {
					spec.Volumes = []corev1.Volume{{
						Name: "test-volume",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					}}
				})

				It("should add the volume to the process settings", func() {
					volumes := spec.Processes[fdbtypes.ProcessClassGeneral].PodTemplate.Spec.Volumes
					Expect(len(volumes)).To(Equal(1))
					Expect(volumes[0].Name).To(Equal("test-volume"))
					Expect(volumes[0].EmptyDir).NotTo(BeNil())

					Expect(spec.Volumes).To(BeNil())
				})
			})

			Context("with a custom value for the PodSecurityContext field", func() {
				BeforeEach(func() {
					var group int64 = 0xFDB
					spec.PodSecurityContext = &corev1.PodSecurityContext{
						RunAsGroup: &group,
					}
				})

				It("should add the security context to the process settings", func() {
					podTemplate := spec.Processes[fdbtypes.ProcessClassGeneral].PodTemplate
					Expect(podTemplate.Spec.SecurityContext).NotTo(BeNil())
					Expect(*podTemplate.Spec.SecurityContext.RunAsGroup).To(Equal(int64(0xFDB)))

					Expect(spec.PodSecurityContext).To(BeNil())
				})
			})

			Context("with a custom value for the VolumeClaim field", func() {
				BeforeEach(func() {
					spec.VolumeClaim = &corev1.PersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceStorage: resource.MustParse("256Gi"),
								},
							},
						},
					}
				})

				It("should put the volume claim template in the process settings", func() {
					volumeClaim := spec.Processes[fdbtypes.ProcessClassGeneral].VolumeClaimTemplate
					Expect(volumeClaim).NotTo(BeNil())
					Expect(volumeClaim.Spec.Resources.Requests[corev1.ResourceStorage]).To(Equal(resource.MustParse("256Gi")))
					Expect(spec.Processes[fdbtypes.ProcessClassGeneral].VolumeClaim).To(BeNil())
				})
			})

			Context("with a custom value for the VolumeClaim field in the process settings", func() {
				BeforeEach(func() {
					spec.Processes = map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{
						fdbtypes.ProcessClassGeneral: {
							VolumeClaim: &corev1.PersistentVolumeClaim{
								Spec: corev1.PersistentVolumeClaimSpec{
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceStorage: resource.MustParse("256Gi"),
										},
									},
								},
							},
						},
					}
				})

				It("should put the volume claim template in the process settings", func() {
					volumeClaim := spec.Processes[fdbtypes.ProcessClassGeneral].VolumeClaimTemplate
					Expect(volumeClaim).NotTo(BeNil())
					Expect(volumeClaim.Spec.Resources.Requests[corev1.ResourceStorage]).To(Equal(resource.MustParse("256Gi")))
					Expect(spec.VolumeClaim).To(BeNil())
				})
			})

			Context("with a custom value for the AutomountServiceAccountToken field", func() {
				BeforeEach(func() {
					var mount = false
					spec.AutomountServiceAccountToken = &mount
				})

				It("should put the volume claim template in the process settings", func() {
					template := spec.Processes[fdbtypes.ProcessClassGeneral].PodTemplate
					Expect(template).NotTo(BeNil())
					Expect(*template.Spec.AutomountServiceAccountToken).To(BeFalse())
					Expect(spec.AutomountServiceAccountToken).To(BeNil())
				})
			})

			Context("with a custom value for the NextInstanceID field", func() {
				BeforeEach(func() {
					spec.NextInstanceID = 10
				})

				It("clears the field", func() {
					Expect(spec.NextInstanceID).To(Equal(0))
				})
			})

			Context("with a custom value for the StorageClass field", func() {
				BeforeEach(func() {
					storageClass := "ebs"
					spec.StorageClass = &storageClass
				})

				It("sets the field in the process settings", func() {
					Expect(*spec.Processes[fdbtypes.ProcessClassGeneral].VolumeClaimTemplate.Spec.StorageClassName).To(Equal("ebs"))
					Expect(spec.StorageClass).To(BeNil())
				})
			})

			Context("with a custom value for the volume size", func() {
				BeforeEach(func() {
					spec.VolumeSize = "16Gi"
				})

				It("sets the field in the process settings", func() {
					Expect(spec.Processes[fdbtypes.ProcessClassGeneral].VolumeClaimTemplate.Spec.Resources.Requests[corev1.ResourceStorage]).To(Equal(resource.MustParse("16Gi")))
					Expect(spec.VolumeSize).To(Equal(""))
				})
			})

			Context("with a running version in the spec", func() {
				BeforeEach(func() {
					spec.RunningVersion = fdbtypes.Versions.Default.String()
				})

				It("clears the field in the spec", func() {
					Expect(spec.RunningVersion).To(Equal(""))
				})
			})

			Context("with a connection string in the spec", func() {
				BeforeEach(func() {
					spec.ConnectionString = "test:test"
				})

				It("clears the field in the spec", func() {
					Expect(spec.ConnectionString).To(Equal(""))
				})
			})

			Context("with custom parameters in the spec", func() {
				BeforeEach(func() {
					spec.CustomParameters = []string{
						"knob_disable_posix_kernel_aio = 1",
					}
				})

				It("sets the custom parameters in the process settings", func() {
					Expect(*spec.Processes[fdbtypes.ProcessClassGeneral].CustomParameters).To(Equal([]string{
						"knob_disable_posix_kernel_aio = 1",
					}))
					Expect(spec.CustomParameters).To(BeNil())
				})
			})
		})

		Describe("Validations", func() {
			Context("with duplicated custom parameters in the spec", func() {
				It("an error should be returned", func() {
					spec.CustomParameters = []string{
						"knob_disable_posix_kernel_aio = 1",
						"knob_disable_posix_kernel_aio = 1",
					}
					err := NormalizeClusterSpec(spec, DeprecationOptions{})
					Expect(err).To(HaveOccurred())
				})
			})

			Context("with a protected custom parameter in the spec", func() {
				It("an error should be returned", func() {
					spec.CustomParameters = []string{
						"datadir=1",
					}
					err := NormalizeClusterSpec(spec, DeprecationOptions{})
					Expect(err).To(HaveOccurred())
				})
			})

			Context("with a duplicated custom parameter in the ProcessSettings", func() {
				It("an error should be returned", func() {
					spec.Processes = map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{
						fdbtypes.ProcessClassGeneral: {
							CustomParameters: &[]string{
								"knob_disable_posix_kernel_aio = 1",
								"knob_disable_posix_kernel_aio = 1",
							},
						},
					}
					err := NormalizeClusterSpec(spec, DeprecationOptions{})
					Expect(err).To(HaveOccurred())
				})
			})

			Context("with a protected custom parameter in the ProcessSettings", func() {
				It("an error should be returned", func() {
					spec.Processes = map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{
						fdbtypes.ProcessClassGeneral: {
							CustomParameters: &[]string{
								"datadir=1",
							},
						},
					}
					err := NormalizeClusterSpec(spec, DeprecationOptions{})
					Expect(err).To(HaveOccurred())
				})
			})
		})

		Describe("defaults", func() {
			Context("with the current defaults", func() {
				JustBeforeEach(func() {
					err := NormalizeClusterSpec(spec, DeprecationOptions{UseFutureDefaults: false, OnlyShowChanges: false})
					Expect(err).NotTo(HaveOccurred())
				})

				It("should have both containers", func() {
					generalProcessConfig, present := spec.Processes[fdbtypes.ProcessClassGeneral]
					Expect(present).To(BeTrue())
					containers := generalProcessConfig.PodTemplate.Spec.Containers
					Expect(len(containers)).To(Equal(2))
				})

				It("should have a main container defined", func() {
					generalProcessConfig, present := spec.Processes[fdbtypes.ProcessClassGeneral]
					Expect(present).To(BeTrue())
					containers := generalProcessConfig.PodTemplate.Spec.Containers
					Expect(len(containers)).To(Equal(2))
					Expect(containers[0].Name).To(Equal("foundationdb"))
					Expect(containers[0].Resources.Requests).To(Equal(corev1.ResourceList{
						"cpu":    resource.MustParse("1"),
						"memory": resource.MustParse("1Gi"),
					}))
					Expect(containers[0].Resources.Limits).To(Equal(corev1.ResourceList{
						"cpu":    resource.MustParse("1"),
						"memory": resource.MustParse("1Gi"),
					}))
				})

				It("should have empty sidecar resource requirements", func() {
					generalProcessConfig, present := spec.Processes[fdbtypes.ProcessClassGeneral]
					Expect(present).To(BeTrue())
					containers := generalProcessConfig.PodTemplate.Spec.Containers
					Expect(len(containers)).To(Equal(2))
					Expect(containers[1].Name).To(Equal("foundationdb-kubernetes-sidecar"))
					Expect(containers[1].Resources.Requests).To(BeNil())
					Expect(containers[1].Resources.Limits).To(BeNil())
				})

				It("should have empty init container resource requirements", func() {
					generalProcessConfig, present := spec.Processes[fdbtypes.ProcessClassGeneral]
					Expect(present).To(BeTrue())
					containers := generalProcessConfig.PodTemplate.Spec.InitContainers
					Expect(len(containers)).To(Equal(1))
					Expect(containers[0].Name).To(Equal("foundationdb-kubernetes-init"))
					Expect(containers[0].Resources.Requests).To(BeNil())
					Expect(containers[0].Resources.Limits).To(BeNil())
				})

				Context("with explicit resource requests for the main container", func() {
					BeforeEach(func() {
						spec.Processes = map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{
							fdbtypes.ProcessClassGeneral: {
								PodTemplate: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{{
											Name: "foundationdb",
											Resources: corev1.ResourceRequirements{
												Requests: corev1.ResourceList{
													"cpu": resource.MustParse("1"),
												},
												Limits: corev1.ResourceList{
													"cpu": resource.MustParse("2"),
												},
											},
										}},
									},
								},
							},
						}
					})

					It("should respect the values given", func() {
						generalProcessConfig, present := spec.Processes[fdbtypes.ProcessClassGeneral]
						Expect(present).To(BeTrue())
						containers := generalProcessConfig.PodTemplate.Spec.Containers
						Expect(len(containers)).To(Equal(2))
						Expect(containers[0].Name).To(Equal("foundationdb"))
						Expect(containers[0].Resources.Requests).To(Equal(corev1.ResourceList{
							"cpu": resource.MustParse("1"),
						}))
						Expect(containers[0].Resources.Limits).To(Equal(corev1.ResourceList{
							"cpu": resource.MustParse("2"),
						}))
					})
				})

				Context("with explicit resource requests for the sidecar", func() {
					BeforeEach(func() {
						spec.Processes = map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{
							fdbtypes.ProcessClassGeneral: {
								PodTemplate: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{{
											Name: "foundationdb-kubernetes-sidecar",
											Resources: corev1.ResourceRequirements{
												Requests: corev1.ResourceList{
													"cpu": resource.MustParse("1"),
												},
												Limits: corev1.ResourceList{
													"cpu": resource.MustParse("2"),
												},
											},
										}},
									},
								},
							},
						}
					})

					It("should respect the values given", func() {
						generalProcessConfig, present := spec.Processes[fdbtypes.ProcessClassGeneral]
						Expect(present).To(BeTrue())
						containers := generalProcessConfig.PodTemplate.Spec.Containers
						Expect(len(containers)).To(Equal(2))
						Expect(containers[1].Name).To(Equal("foundationdb-kubernetes-sidecar"))
						Expect(containers[1].Resources.Requests).To(Equal(corev1.ResourceList{
							"cpu": resource.MustParse("1"),
						}))
						Expect(containers[1].Resources.Limits).To(Equal(corev1.ResourceList{
							"cpu": resource.MustParse("2"),
						}))
					})
				})

				It("should have the public IP source set to pod", func() {
					Expect(spec.Services.PublicIPSource).NotTo(BeNil())
					Expect(*spec.Services.PublicIPSource).To(Equal(fdbtypes.PublicIPSourcePod))
				})

				It("should have automatic replacements disabled", func() {
					Expect(spec.AutomationOptions.Replacements.Enabled).NotTo(BeNil())
					Expect(*spec.AutomationOptions.Replacements.Enabled).To(BeFalse())
					Expect(spec.AutomationOptions.Replacements.FailureDetectionTimeSeconds).NotTo(BeNil())
					Expect(*spec.AutomationOptions.Replacements.FailureDetectionTimeSeconds).To(Equal(1800))
				})
			})

			Context("with the current defaults, changes only", func() {
				JustBeforeEach(func() {
					err := NormalizeClusterSpec(spec, DeprecationOptions{UseFutureDefaults: false, OnlyShowChanges: true})
					Expect(err).NotTo(HaveOccurred())
				})

				It("should have a single container", func() {
					generalProcessConfig, present := spec.Processes[fdbtypes.ProcessClassGeneral]
					Expect(present).To(BeTrue())
					containers := generalProcessConfig.PodTemplate.Spec.Containers
					Expect(len(containers)).To(Equal(1))
				})

				It("should have empty sidecar resource requirements", func() {
					generalProcessConfig, present := spec.Processes[fdbtypes.ProcessClassGeneral]
					Expect(present).To(BeTrue())
					containers := generalProcessConfig.PodTemplate.Spec.Containers
					Expect(len(containers)).To(Equal(1))
					Expect(containers[0].Name).To(Equal("foundationdb-kubernetes-sidecar"))
					Expect(containers[0].Resources.Requests).To(Equal(corev1.ResourceList{
						"org.foundationdb/empty": resource.MustParse("0"),
					}))
					Expect(containers[0].Resources.Limits).To(Equal(corev1.ResourceList{
						"org.foundationdb/empty": resource.MustParse("0"),
					}))
				})

				It("should have no public IP source", func() {
					Expect(spec.Services.PublicIPSource).To(BeNil())
				})

				It("should have automatic replacements disabled", func() {
					Expect(spec.AutomationOptions.Replacements.Enabled).NotTo(BeNil())
					Expect(*spec.AutomationOptions.Replacements.Enabled).To(BeFalse())
				})

				It("should have no configuration for other automatic replacement options", func() {
					Expect(spec.AutomationOptions.Replacements.FailureDetectionTimeSeconds).To(BeNil())
				})
			})

			Context("with the future defaults", func() {
				JustBeforeEach(func() {
					err := NormalizeClusterSpec(spec, DeprecationOptions{UseFutureDefaults: true, OnlyShowChanges: false})
					Expect(err).NotTo(HaveOccurred())
				})

				It("should have default sidecar resource requirements", func() {
					generalProcessConfig, present := spec.Processes[fdbtypes.ProcessClassGeneral]
					Expect(present).To(BeTrue())
					containers := generalProcessConfig.PodTemplate.Spec.Containers
					Expect(len(containers)).To(Equal(2))
					Expect(containers[1].Name).To(Equal("foundationdb-kubernetes-sidecar"))
					Expect(containers[1].Resources.Requests).To(Equal(corev1.ResourceList{
						"cpu":    resource.MustParse("100m"),
						"memory": resource.MustParse("256Mi"),
					}))
					Expect(containers[1].Resources.Limits).To(Equal(corev1.ResourceList{
						"cpu":    resource.MustParse("100m"),
						"memory": resource.MustParse("256Mi"),
					}))
				})

				Context("with explicit resource requests", func() {
					BeforeEach(func() {
						spec.Processes = map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{
							fdbtypes.ProcessClassGeneral: {
								PodTemplate: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{{
											Name: "foundationdb-kubernetes-sidecar",
											Resources: corev1.ResourceRequirements{
												Requests: corev1.ResourceList{
													"cpu": resource.MustParse("1"),
												},
												Limits: corev1.ResourceList{
													"cpu": resource.MustParse("2"),
												},
											},
										}},
									},
								},
							},
						}
					})

					It("should respect the values given", func() {
						generalProcessConfig, present := spec.Processes[fdbtypes.ProcessClassGeneral]
						Expect(present).To(BeTrue())
						containers := generalProcessConfig.PodTemplate.Spec.Containers
						Expect(len(containers)).To(Equal(2))
						Expect(containers[1].Name).To(Equal("foundationdb-kubernetes-sidecar"))
						Expect(containers[1].Resources.Requests).To(Equal(corev1.ResourceList{
							"cpu": resource.MustParse("1"),
						}))
						Expect(containers[1].Resources.Limits).To(Equal(corev1.ResourceList{
							"cpu": resource.MustParse("2"),
						}))
					})
				})

				Context("with explicit resource requirements for requests only", func() {
					BeforeEach(func() {
						spec.Processes = map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{
							fdbtypes.ProcessClassGeneral: {
								PodTemplate: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{{
											Name: "foundationdb-kubernetes-sidecar",
											Resources: corev1.ResourceRequirements{
												Requests: corev1.ResourceList{
													"cpu": resource.MustParse("1"),
												},
											},
										}},
									},
								},
							},
						}
					})

					It("should set the default limits", func() {
						generalProcessConfig, present := spec.Processes[fdbtypes.ProcessClassGeneral]
						Expect(present).To(BeTrue())
						containers := generalProcessConfig.PodTemplate.Spec.Containers
						Expect(len(containers)).To(Equal(2))
						Expect(containers[1].Name).To(Equal("foundationdb-kubernetes-sidecar"))
						Expect(containers[1].Resources.Requests).To(Equal(corev1.ResourceList{
							"cpu": resource.MustParse("1"),
						}))
						Expect(containers[1].Resources.Limits).To(Equal(corev1.ResourceList{
							"cpu": resource.MustParse("1"),
						}))
					})
				})

				Context("with explicitly empty resource requirements", func() {
					BeforeEach(func() {
						spec.Processes = map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{
							fdbtypes.ProcessClassGeneral: {
								PodTemplate: &corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{
										Containers: []corev1.Container{{
											Name: "foundationdb-kubernetes-sidecar",
											Resources: corev1.ResourceRequirements{
												Requests: corev1.ResourceList{},
												Limits:   corev1.ResourceList{},
											},
										}},
									},
								},
							},
						}
					})

					It("should respect the values given", func() {
						generalProcessConfig, present := spec.Processes[fdbtypes.ProcessClassGeneral]
						Expect(present).To(BeTrue())
						containers := generalProcessConfig.PodTemplate.Spec.Containers
						Expect(len(containers)).To(Equal(2))
						Expect(containers[1].Name).To(Equal("foundationdb-kubernetes-sidecar"))
						Expect(containers[1].Resources.Requests).To(Equal(corev1.ResourceList{}))
						Expect(containers[1].Resources.Limits).To(Equal(corev1.ResourceList{}))
					})
				})

				It("should have the public IP source set to pod", func() {
					Expect(spec.Services.PublicIPSource).NotTo(BeNil())
					Expect(*spec.Services.PublicIPSource).To(Equal(fdbtypes.PublicIPSourcePod))
				})

				It("should have automatic replacements enabled", func() {
					Expect(spec.AutomationOptions.Replacements.Enabled).NotTo(BeNil())
					Expect(*spec.AutomationOptions.Replacements.Enabled).To(BeTrue())
					Expect(spec.AutomationOptions.Replacements.FailureDetectionTimeSeconds).NotTo(BeNil())
					Expect(*spec.AutomationOptions.Replacements.FailureDetectionTimeSeconds).To(Equal(1800))
				})
			})

			Context("with the future defaults, changes only", func() {
				JustBeforeEach(func() {
					err := NormalizeClusterSpec(spec, DeprecationOptions{UseFutureDefaults: true, OnlyShowChanges: true})
					Expect(err).NotTo(HaveOccurred())
				})

				It("should have default sidecar resource requirements", func() {
					generalProcessConfig, present := spec.Processes[fdbtypes.ProcessClassGeneral]
					Expect(present).To(BeTrue())
					containers := generalProcessConfig.PodTemplate.Spec.Containers
					Expect(len(containers)).To(Equal(1))
					Expect(containers[0].Name).To(Equal("foundationdb-kubernetes-sidecar"))
					Expect(containers[0].Resources.Requests).To(Equal(corev1.ResourceList{
						"cpu":    resource.MustParse("100m"),
						"memory": resource.MustParse("256Mi"),
					}))
					Expect(containers[0].Resources.Limits).To(Equal(corev1.ResourceList{
						"cpu":    resource.MustParse("100m"),
						"memory": resource.MustParse("256Mi"),
					}))
				})

				It("should have no public IP source", func() {
					Expect(spec.Services.PublicIPSource).To(BeNil())
				})

				It("should have automatic replacements enabled", func() {
					Expect(spec.AutomationOptions.Replacements.Enabled).NotTo(BeNil())
					Expect(*spec.AutomationOptions.Replacements.Enabled).To(BeTrue())
				})

				It("should have no configuration for other automatic replacement options", func() {
					Expect(spec.AutomationOptions.Replacements.FailureDetectionTimeSeconds).To(BeNil())
				})
			})

			Context("when applying future defaults on top of current explicit defaults", func() {
				var originalSpec *fdbtypes.FoundationDBClusterSpec

				BeforeEach(func() {
					err = NormalizeClusterSpec(spec, DeprecationOptions{UseFutureDefaults: false, OnlyShowChanges: true})
					Expect(err).NotTo(HaveOccurred())
					originalSpec = spec.DeepCopy()
				})

				JustBeforeEach(func() {
					err = NormalizeClusterSpec(spec, DeprecationOptions{UseFutureDefaults: true, OnlyShowChanges: true})
					Expect(err).NotTo(HaveOccurred())
				})

				It("should be equal to the version with the old explicit defaults", func() {
					Expect(equality.Semantic.DeepEqual(originalSpec, spec)).To(BeTrue())
				})
			})
		})
	})
})
