package controllers

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("mergeLabelsInMetadata", func() {
	var metadata *metav1.ObjectMeta

	When("the target map is populated", func() {
		BeforeEach(func() {
			metadata = &metav1.ObjectMeta{}
			metadata.Labels = map[string]string{
				"existing-label": "existing-value",
			}
		})

		Context("and the desired map contains a new label", func() {
			var desired = metav1.ObjectMeta{}
			desired.Labels = map[string]string{
				"new-label": "new-value",
			}

			It("should add the new label to the target", func() {
				Expect(mergeLabelsInMetadata(metadata, desired)).To(Equal(true))
				Expect(metadata.Labels).To(Equal(map[string]string{
					"existing-label": "existing-value",
					"new-label":      "new-value",
				}))
			})
		})
	})

	When("the target map is nil", func() {
		BeforeEach(func() {
			metadata = &metav1.ObjectMeta{}
		})

		Context("and the desired map contains a new label", func() {
			var desired = metav1.ObjectMeta{}
			desired.Labels = map[string]string{
				"new-label": "new-value",
			}

			It("should add the new label to the target", func() {
				Expect(mergeLabelsInMetadata(metadata, desired)).To(Equal(true))
				Expect(metadata.Labels).To(Equal(map[string]string{
					"new-label": "new-value",
				}))
			})
		})
	})
})

var _ = Describe("mergeAnnotations", func() {
	var metadata *metav1.ObjectMeta

	When("the target map is populated", func() {
		BeforeEach(func() {
			metadata = &metav1.ObjectMeta{}
			metadata.Annotations = map[string]string{
				"existing-annotation": "existing-value",
			}
		})

		Context("and the desired map contains a new annotation", func() {
			var desired = metav1.ObjectMeta{}
			desired.Annotations = map[string]string{
				"new-annotation": "new-value",
			}

			It("should add the new annotation to the target", func() {
				Expect(mergeAnnotations(metadata, desired)).To(Equal(true))
				Expect(metadata.Annotations).To(Equal(map[string]string{
					"existing-annotation": "existing-value",
					"new-annotation":      "new-value",
				}))
			})
		})
	})

	When("the target map is nil", func() {
		BeforeEach(func() {
			metadata = &metav1.ObjectMeta{}
		})

		Context("and the desired map contains a new annotation", func() {
			var desired = metav1.ObjectMeta{}
			desired.Annotations = map[string]string{
				"new-annotation": "new-value",
			}

			It("should add the new annotation to the target", func() {
				Expect(mergeAnnotations(metadata, desired)).To(Equal(true))
				Expect(metadata.Annotations).To(Equal(map[string]string{
					"new-annotation": "new-value",
				}))
			})
		})
	})
})

var _ = Describe("mergeMap", func() {
	When("the target map is populated", func() {
		var target map[string]string

		BeforeEach(func() {
			target = map[string]string{
				"test-key": "test-value",
			}
		})

		Context("and the desired map is populated with a new key/value pair", func() {
			var desired = map[string]string{
				"new-key": "new-value",
			}

			It("should add the new value to the target map", func() {
				Expect(mergeMap(target, desired)).To(Equal(true))
				Expect(target).To(Equal(map[string]string{
					"test-key": "test-value",
					"new-key":  "new-value",
				}))
			})
		})

		Context("and the desired map is populated with a new value for an existing key", func() {
			var desired = map[string]string{
				"test-key": "new-value",
			}

			It("should add the new value to the target map", func() {
				Expect(mergeMap(target, desired)).To(Equal(true))
				Expect(target).To(Equal(map[string]string{
					"test-key": "new-value",
				}))
			})
		})

		Context("and the desired map is populated with the same value for an existing key", func() {
			var desired = map[string]string{
				"test-key": "test-value",
			}

			It("should not change the target map", func() {
				Expect(mergeMap(target, desired)).To(Equal(false))
				Expect(target).To(Equal(map[string]string{
					"test-key": "test-value",
				}))
			})
		})

		Context("and the desired map is empty", func() {
			var desired = map[string]string{}

			It("should not change the target map", func() {
				Expect(mergeMap(target, desired)).To(Equal(false))
				Expect(target).To(Equal(map[string]string{
					"test-key": "test-value",
				}))
			})
		})

		Context("and the desired map is nil", func() {
			It("should not change the target map", func() {
				Expect(mergeMap(target, nil)).To(Equal(false))
				Expect(target).To(Equal(map[string]string{
					"test-key": "test-value",
				}))
			})
		})
	})

	When("the target map is empty", func() {
		var target map[string]string

		BeforeEach(func() {
			target = map[string]string{}
		})

		Context("and the desired map is populated with a new key/value pair", func() {
			var desired = map[string]string{
				"new-key": "new-value",
			}

			It("should add the new value to the target map", func() {
				Expect(mergeMap(target, desired)).To(Equal(true))
				Expect(target).To(Equal(map[string]string{
					"new-key": "new-value",
				}))
			})
		})

		Context("and the desired map is empty", func() {
			var desired = map[string]string{}

			It("should not change the target map", func() {
				Expect(mergeMap(target, desired)).To(Equal(false))
				Expect(target).To(Equal(map[string]string{}))
			})
		})

		Context("and the desired map is nil", func() {
			It("should not change the target map", func() {
				Expect(mergeMap(target, nil)).To(Equal(false))
				Expect(target).To(Equal(map[string]string{}))
			})
		})
	})
})
