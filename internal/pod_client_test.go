/*
 * pod_client_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2019 Apple Inc. and the FoundationDB project authors
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
	"net/http"
	"net/url"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/hashicorp/go-retryablehttp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("pod_client", func() {
	var cluster *fdbv1beta2.FoundationDBCluster

	BeforeEach(func() {
		cluster = CreateDefaultCluster()
		Expect(NormalizeClusterSpec(cluster, DeprecationOptions{})).To(Succeed())
	})

	Context("with TLS disabled", func() {
		BeforeEach(func() {
			cluster.Spec.SidecarContainer.EnableTLS = false
		})

		It("should not have TLS sidecar TLS", func() {
			pod, err := GetPod(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
			Expect(err).NotTo(HaveOccurred())
			Expect(PodHasSidecarTLS(pod)).To(BeFalse())
		})
	})

	Context("with TLS enabled", func() {
		BeforeEach(func() {
			cluster.Spec.SidecarContainer.EnableTLS = true
		})

		It("should have TLS sidecar TLS", func() {
			pod, err := GetPod(cluster, GetProcessGroup(cluster, fdbv1beta2.ProcessClassStorage, 1))
			Expect(err).NotTo(HaveOccurred())
			Expect(PodHasSidecarTLS(pod)).To(BeTrue())
		})
	})

	When("generating a request", func() {
		var retryClient *retryablehttp.Client
		var target url.URL
		getTimeout := 1 * time.Second
		postTimeout := 10 * time.Second

		BeforeEach(func() {
			retryClient = retryablehttp.NewClient()
			target = url.URL{
				Scheme: "http",
				Host:   "127.0.0.1:8080",
				Path:   "test",
			}
		})

		When("generating a http get request", func() {
			It("should generate the request", func() {
				req, err := generateRequest(
					retryClient,
					target.String(),
					http.MethodGet,
					getTimeout,
					postTimeout,
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(req.Method).To(Equal(http.MethodGet))
				Expect(retryClient.HTTPClient.Timeout).To(Equal(getTimeout))
			})
		})

		When("generating a http post request", func() {
			It("should generate the request", func() {
				req, err := generateRequest(
					retryClient,
					target.String(),
					http.MethodPost,
					getTimeout,
					postTimeout,
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(req.Method).To(Equal(http.MethodPost))
				Expect(retryClient.HTTPClient.Timeout).To(Equal(postTimeout))
				Expect(
					req.Header,
				).To(HaveKeyWithValue("Content-Type", []string{"application/json"}))
			})
		})

		When("generating a http delete request", func() {
			It("should generate the request", func() {
				req, err := generateRequest(
					retryClient,
					target.String(),
					http.MethodDelete,
					getTimeout,
					postTimeout,
				)
				Expect(err).To(HaveOccurred())
				Expect(req).To(BeNil())
			})
		})
	})
})
