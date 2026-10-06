/*
 * setup_test.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2026 Apple Inc. and the FoundationDB project authors
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

package setup

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"sync/atomic"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/controllers"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/podmanager"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/pflag"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("setup", func() {
	var options Options

	It("serves liveness independently of API discovery and metrics", func() {
		registry := metrics.Registry
		metrics.Registry = prometheus.NewRegistry()
		DeferCleanup(func() { metrics.Registry = registry })

		apiRequested := make(chan struct{}, 1)
		apiBlocked := make(chan struct{}, 1)
		var discoveryRequests atomic.Int32
		var blockAPI atomic.Bool
		blockedCtx, unblockAPI := context.WithCancel(context.Background())
		DeferCleanup(unblockAPI)
		apiServer := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/api" || request.URL.Path == "/apis" {
					discoveryRequests.Add(1)
				}
				select {
				case apiRequested <- struct{}{}:
				default:
				}
				if blockAPI.Load() {
					select {
					case apiBlocked <- struct{}{}:
					default:
					}
					select {
					case <-blockedCtx.Done():
					case <-request.Context().Done():
					}
				}
				http.Error(w, "API unavailable", http.StatusServiceUnavailable)
			}),
		)
		DeferCleanup(apiServer.Close)

		tempDir := GinkgoT().TempDir()
		kubeconfig := path.Join(tempDir, "kubeconfig")
		Expect(os.WriteFile(kubeconfig, fmt.Appendf(nil, `apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: %s
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
users:
- name: test
  user: {}
`, apiServer.URL), 0600)).To(Succeed())
		GinkgoT().Setenv("KUBECONFIG", kubeconfig)
		GinkgoT().Setenv("FDB_BINARY_DIR", tempDir)
		GinkgoT().Setenv(fdbv1beta2.EnvNameFDBExternalClientDir, tempDir)

		availableAddress := func() string {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			Expect(err).NotTo(HaveOccurred())
			address := listener.Addr().String()
			Expect(listener.Close()).To(Succeed())
			return address
		}
		healthAddress := availableAddress()
		options.BindFlags(pflag.NewFlagSet("test", pflag.ContinueOnError))
		options.EnableLeaderElection = false
		options.CleanUpOldLogFile = false
		options.WatchNamespace = "operator-test"
		options.MetricsAddr = availableAddress()
		options.HealthProbeBindAddress = healthAddress
		scheme := runtime.NewScheme()
		Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
		Expect(fdbv1beta2.AddToScheme(scheme)).To(Succeed())
		// A setup failure in StartManager exits the process instead of failing this spec
		mgr, _ := StartManager(scheme, options, zap.Options{},
			controllers.NewFoundationDBClusterReconciler(&podmanager.StandardPodLifecycleManager{}),
			&controllers.FoundationDBBackupReconciler{},
			&controllers.FoundationDBRestoreReconciler{}, ctrl.Log)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- mgr.Start(ctx) }()
		DeferCleanup(func() {
			defer unblockAPI()
			cancel()
			Eventually(done, 5*time.Second).Should(Receive(Succeed()))
		})

		Eventually(apiRequested, 5*time.Second).Should(Receive())
		healthClient := &http.Client{Timeout: time.Second}
		checkHealth := func() {
			response, err := healthClient.Get("http://" + healthAddress + "/healthz")
			Expect(err).NotTo(HaveOccurred())
			defer response.Body.Close()
			Expect(response.StatusCode).To(Equal(http.StatusOK))
			body, err := io.ReadAll(response.Body)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(body)).To(Equal("ok"))
		}
		checkHealth()

		blockAPI.Store(true)
		metricsDone := make(chan error, 3)
		metricsClient := &http.Client{Timeout: 15 * time.Second}
		for range cap(metricsDone) {
			go func() {
				response, err := metricsClient.Get("http://" + options.MetricsAddr + "/metrics")
				if response != nil {
					if response.StatusCode != http.StatusOK {
						err = fmt.Errorf("metrics returned HTTP %d", response.StatusCode)
					}
					response.Body.Close()
				}
				metricsDone <- err
			}()
		}
		Eventually(apiBlocked, 5*time.Second).Should(Receive())
		checkHealth()
		for range cap(metricsDone) {
			Eventually(metricsDone, 15*time.Second).Should(Receive(Succeed()))
		}
		checkHealth()
		Expect(discoveryRequests.Load()).To(BeZero())
	})

	It("discovers mappings for additional resource types", func() {
		apiServer := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch request.URL.Path {
				case "/api":
					_, _ = io.WriteString(
						w,
						`{"kind":"APIVersions","apiVersion":"v1","versions":["v1"]}`,
					)
				case "/apis":
					_, _ = io.WriteString(
						w,
						`{"kind":"APIGroupList","apiVersion":"v1","groups":[{"name":"apps","versions":[{"groupVersion":"apps/v1","version":"v1"}],"preferredVersion":{"groupVersion":"apps/v1","version":"v1"}}]}`,
					)
				case "/apis/apps/v1":
					_, _ = io.WriteString(
						w,
						`{"kind":"APIResourceList","apiVersion":"v1","groupVersion":"apps/v1","resources":[{"name":"statefulsets","singularName":"statefulset","namespaced":true,"kind":"StatefulSet","verbs":["get"]}]}`,
					)
				case "/apis/apps/v1/namespaces/operator-test/statefulsets/example":
					_, _ = io.WriteString(
						w,
						`{"kind":"StatefulSet","apiVersion":"apps/v1","metadata":{"name":"example","namespace":"operator-test"}}`,
					)
				default:
					http.NotFound(w, request)
				}
			}),
		)
		DeferCleanup(apiServer.Close)
		config := &rest.Config{Host: apiServer.URL}
		mapper, err := newRESTMapper(config, apiServer.Client())
		Expect(err).NotTo(HaveOccurred())
		kubeClient, err := client.New(config, client.Options{Mapper: mapper})
		Expect(err).NotTo(HaveOccurred())
		statefulSet := &appsv1.StatefulSet{}
		Expect(kubeClient.Get(context.Background(), client.ObjectKey{
			Namespace: "operator-test", Name: "example",
		}, statefulSet)).To(Succeed())
		Expect(statefulSet.Name).To(Equal("example"))
	})

	When("no log output file is defined", func() {
		It("should return stdout as writer", func() {
			writer, err := setupLogger(options)
			Expect(err).NotTo(HaveOccurred())
			Expect(writer).To(BeIdenticalTo(os.Stdout))
		})
	})

	When("a log output file is defined", func() {
		var tmpDir, logFile string

		BeforeEach(func() {
			tmpDir = GinkgoT().TempDir()
			logFile = path.Join(tmpDir, "operator.logs")

			options = Options{
				LogFile: logFile,
			}
		})

		It("should create the log file with the right permissions", func() {
			writer, err := setupLogger(options)
			Expect(err).NotTo(HaveOccurred())

			_, err = writer.Write([]byte("Hello World!"))
			Expect(err).NotTo(HaveOccurred())

			resultFile, err := os.Stat(logFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(logFile).To(Equal(path.Join(tmpDir, resultFile.Name())))
			// Default file mode is 0644
			Expect(resultFile.Mode()).To(Equal(fs.FileMode(0644)))
			Expect(resultFile.Size()).To(BeNumerically(">", 0))
		})

		When("the log file already exists with the wrong permissions", func() {
			BeforeEach(func() {
				Expect(os.WriteFile(logFile, nil, 0600)).NotTo(HaveOccurred())
			})

			It("should correct the permission", func() {
				writer, err := setupLogger(options)
				Expect(err).NotTo(HaveOccurred())

				_, err = writer.Write([]byte("Hello World!"))
				Expect(err).NotTo(HaveOccurred())

				resultFile, err := os.Stat(logFile)
				Expect(err).NotTo(HaveOccurred())
				Expect(logFile).To(Equal(path.Join(tmpDir, resultFile.Name())))
				// Default file mode is 0644
				Expect(resultFile.Mode()).To(Equal(fs.FileMode(0644)))
				Expect(resultFile.Size()).To(BeNumerically(">", 0))
			})
		})

		When("file permissions are specified", func() {
			BeforeEach(func() {
				options.LogFilePermission = "0600"
			})

			It("should correct the permission", func() {
				writer, err := setupLogger(options)
				Expect(err).NotTo(HaveOccurred())

				_, err = writer.Write([]byte("Hello World!"))
				Expect(err).NotTo(HaveOccurred())

				resultFile, err := os.Stat(logFile)
				Expect(err).NotTo(HaveOccurred())
				Expect(logFile).To(Equal(path.Join(tmpDir, resultFile.Name())))
				Expect(resultFile.Mode()).To(Equal(fs.FileMode(0600)))
				Expect(resultFile.Size()).To(BeNumerically(">", 0))
			})
		})
	})
})
