/*
 * fdb_data_loader.go
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
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"text/template"
	"time"

	"github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// The name of the data loader Job.
	dataLoaderName = "fdb-data-loader"

	// We load 2GB into the cluster, we can increase this later if we want.
	dataLoaderJob = `apiVersion: batch/v1
kind: Job
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
  labels:
    app: {{ .Name }}
spec:
  backoffLimit: 2
  completions: 2
  parallelism: 2
  template:
    spec:
      containers:
      - image: {{ .Image }}
        imagePullPolicy: Always
        name: {{ .Name }}
        # This configuration will load ~1GB per data loader.
        args:
        - --keys={{ .Config.Keys }}
        - --batch-size={{ .Config.BatchSize }}
        - --value-size={{ .Config.ValueSize }}
        - --cluster-file-directory=/var/dynamic/fdb
        - --read-values={{ .Config.ReadValues }}
        env:
          - name: FDB_CLUSTER_FILE
            value: /var/dynamic/fdb/fdb.cluster
          - name: FDB_TLS_CERTIFICATE_FILE
            value: /tmp/fdb-certs/tls.crt
          - name: FDB_TLS_CA_FILE
            value: /tmp/fdb-certs/ca.pem
          - name: FDB_TLS_KEY_FILE
            value: /tmp/fdb-certs/tls.key
          # FDB 7.3 adds a check for loading external client library, which doesn't work with 6.3.
          # Consider remove this option once 6.3 is no longer being used.
          - name: FDB_NETWORK_OPTION_IGNORE_EXTERNAL_CLIENT_FAILURES
            value: ""
          - name: LD_LIBRARY_PATH
            value: /var/dynamic/fdb/primary/lib
          - name: FDB_NETWORK_OPTION_TRACE_LOG_GROUP
            value: {{ .Name }}
          - name: FDB_NETWORK_OPTION_TRACE_ENABLE
            value: "/tmp/fdb-trace-logs"
          - name: FDB_NETWORK_OPTION_EXTERNAL_CLIENT_DIRECTORY
            value: /var/dynamic/fdb/libs
          - name: FDB_NETWORK_OPTION_TRACE_FORMAT
            value: json
          - name: FDB_NETWORK_OPTION_CLIENT_THREADS_PER_VERSION
            value: "10"
        volumeMounts:
          - name: fdb-libs
            mountPath: /var/dynamic/fdb
          - name: fdb-certs
            mountPath: /tmp/fdb-certs
            readOnly: true
          - name: fdb-logs
            mountPath: /tmp/fdb-trace-logs
        resources:
         requests:
           cpu: "1"
           memory: 4Gi
      initContainers:
        {{ range $index, $version := .SidecarVersions }}
        - name: foundationdb-kubernetes-init-{{ $index }}
          image: {{ .Image }}
          imagePullPolicy: Always
          command:
            - /bin/bash
          # This is a workaround for a change of the version schema that was never tested/supported
          args:
            - -c
            - echo "{{ .FDBVersion.String }}" > /var/fdb/version && runuser -u fdb -g fdb -- /entrypoint.bash --copy-library {{ .FDBVersion.Compact }} --output-dir /var/output-files/{{ .FDBVersion.Compact }} --init-mode
          volumeMounts:
            - name: fdb-libs
              mountPath: /var/output-files
          securityContext:
            runAsUser: 0
            runAsGroup: 0
        # Install this library in a special location to force the operator to use it as the primary library.
        {{ if .CopyAsPrimary }}
        - name: foundationdb-kubernetes-init-primary
          image: {{ .Image }}
          imagePullPolicy: Always
          args:
            # Note that we are only copying a library, rather than copying any binaries. 
            - "--copy-library"
            - "{{ .FDBVersion.Compact }}"
            - "--output-dir"
            - "/var/output-files/primary" # Note that we use primary as the subdirectory rather than specifying the FoundationDB version like we did in the other examples.
            - "--init-mode"
          volumeMounts:
            - name: fdb-libs
              mountPath: /var/output-files
        {{ end }}
        {{ end }}
        - image: {{ .Image }}
          imagePullPolicy: Always
          name: fdb-lib-copy
          command:
            - /bin/bash
          args:
            - -c
            - mkdir -p /var/dynamic/fdb/libs && {{ range $index, $version := .SidecarVersions -}} cp /var/dynamic/fdb/{{ .FDBVersion.Compact }}/lib/libfdb_c.so /var/dynamic/fdb/libs/libfdb_{{ .FDBVersion.Compact }}_c.so && {{ end }} cp /var/dynamic-conf/*.cluster /var/dynamic/fdb/
          volumeMounts:
          - name: cluster-files
            mountPath: /var/dynamic-conf
          - name: fdb-libs
            mountPath: /var/dynamic/fdb
          - name: fdb-certs
            mountPath: /tmp/fdb-certs
            readOnly: true
      restartPolicy: Never
      volumes:
        - name: cluster-files
          projected:
            sources:
{{- range $index, $clusterName := .ClusterNames }}
              - name: {{ $clusterName }}-config
                configMap:
                  name: {{ $clusterName }}-config
                  items:
                    - key: cluster-file
                      path: {{ $clusterName }}.cluster
{{- end }}
        - name: fdb-libs
          emptyDir: {}
        - name: fdb-logs
          emptyDir: {}
        - name: fdb-certs
          secret:
            secretName: {{ .SecretName }}`

	// For now, we only load 2GB into the cluster, we can increase this later if we want.
	dataLoaderJobUnifiedImage = `apiVersion: batch/v1
kind: Job
metadata:
  name: {{ .Name }}
  namespace: {{ .Namespace }}
  labels:
    app: {{ .Name }}
spec:
  backoffLimit: 2
  completions: 2
  parallelism: 2
  template:
    spec:
      containers:
      - image: {{ .Image }}
        imagePullPolicy: Always
        name: {{ .Name }}
        # This configuration will load ~1GB per data loader.
        args:
        - --keys={{ .Config.Keys }}
        - --batch-size={{ .Config.BatchSize }}
        - --value-size={{ .Config.ValueSize }}
        - --cluster-file-directory=/var/dynamic/fdb
        - --read-values={{ .Config.ReadValues }}
        env:
          - name: FDB_CLUSTER_FILE
            value: /var/dynamic/fdb/fdb.cluster
          - name: FDB_TLS_CERTIFICATE_FILE
            value: /tmp/fdb-certs/tls.crt
          - name: FDB_TLS_CA_FILE
            value: /tmp/fdb-certs/ca.pem
          - name: FDB_TLS_KEY_FILE
            value: /tmp/fdb-certs/tls.key
          # FDB 7.3 adds a check for loading external client library, which doesn't work with 6.3.
          # Consider remove this option once 6.3 is no longer being used.
          - name: FDB_NETWORK_OPTION_IGNORE_EXTERNAL_CLIENT_FAILURES
            value: ""
          - name: FDB_NETWORK_OPTION_TRACE_ENABLE
            value: "/tmp/fdb-trace-logs"
          - name: LD_LIBRARY_PATH
            value: /var/dynamic/fdb
          - name: FDB_NETWORK_OPTION_TRACE_LOG_GROUP
            value: {{ .Name }}
          - name: FDB_NETWORK_OPTION_EXTERNAL_CLIENT_DIRECTORY
            value: /var/dynamic/fdb
          - name: FDB_NETWORK_OPTION_TRACE_FORMAT
            value: json
          - name: FDB_NETWORK_OPTION_CLIENT_THREADS_PER_VERSION
            value: "10"
        volumeMounts:
          - name: fdb-libs
            mountPath: /var/dynamic/fdb
          - name: fdb-certs
            mountPath: /tmp/fdb-certs
            readOnly: true
          - name: fdb-logs
            mountPath: /tmp/fdb-trace-logs
        resources:
         requests:
           cpu: "1"
           memory: 4Gi
      initContainers:
{{- range $index, $version := .SidecarVersions }}
        - name: foundationdb-kubernetes-init-{{ $index }}
          image: {{ .Image }}
          imagePullPolicy: Always
          args:
            - --mode
            - init
            - --output-dir
            - /var/output-files
            - --copy-library
            - "{{ .FDBVersion.Compact }}"
{{- if .CopyAsPrimary }}
            - --copy-primary-library
            - "{{ .FDBVersion.Compact }}"
{{- end }}
          volumeMounts:
            - name: fdb-libs
              mountPath: /var/output-files
          securityContext:
            runAsUser: 0
            runAsGroup: 0
{{- if .CopyAsPrimary }}
        - name: foundationdb-kubernetes-init-cluster-file
          image: {{ .Image }}
          imagePullPolicy: Always
          args:
            - --mode
            - init
            - --input-dir
            - /var/dynamic-conf
            - --output-dir
            - /var/output-files
{{- range $index, $clusterName := $.ClusterNames }}
            - --copy-file
            - {{ $clusterName }}.cluster
            - --require-not-empty
            - {{ $clusterName }}.cluster
{{- end }}
          volumeMounts:
            - name: fdb-libs
              mountPath: /var/output-files
            - name: cluster-files
              mountPath: /var/dynamic-conf/
          securityContext:
            runAsUser: 0
            runAsGroup: 0
{{- end }}
{{- end }}
      restartPolicy: Never
      volumes:
        - name: cluster-files
          projected:
            sources:
{{- range $index, $clusterName := .ClusterNames }}
              - name: {{ $clusterName }}-config
                configMap:
                  name: {{ $clusterName }}-config
                  items:
                    - key: cluster-file
                      path: {{ $clusterName }}.cluster
{{- end }}
        - name: fdb-libs
          emptyDir: {}
        - name: fdb-logs
          emptyDir: {}
        - name: fdb-certs
          secret:
            secretName: {{ .SecretName }}`
)

// dataLoaderConfig represents the configuration of the data-loader Job.
type dataLoaderConfig struct {
	// Name of the data loader Job.
	Name string
	// Image represents the data loader image that should be used in the Job.
	Image string
	// SidecarVersions represents the sidecar configurations for different FoundationDB versions.
	SidecarVersions []SidecarConfig
	// Namespace represents the namespace for the Deployment and all associated resources
	Namespace string
	// ClusterNames the names of the clusters to load data into.
	ClusterNames []string
	// SecretName represents the Kubernetes secret that contains the certificates for communicating with the FoundationDB
	// cluster.
	SecretName string
	// Config defines the workload configuration.
	Config *WorkloadConfig
}

// WorkloadConfig defines the workload configuration.
type WorkloadConfig struct {
	// Keys defines how many keys should be written by the data loader.
	Keys int
	// BatchSize defines how many keys should be inserted per batch (transaction).
	BatchSize int
	// ValueSize defines the value size in bytes per key-value pair.
	ValueSize int
	// ReadValues defines if the data loader should be reading the written values again to add some read load.
	ReadValues bool
}

func (config *WorkloadConfig) setDefaults() {
	if config.Keys == 0 {
		config.Keys = 1000000
	}

	if config.BatchSize == 0 {
		config.BatchSize = 1000
	}

	if config.ValueSize == 0 {
		config.ValueSize = 1000
	}
}

func (factory *Factory) getDataLoaderConfig(clusters []*FdbCluster, config *WorkloadConfig) *dataLoaderConfig {
	if config == nil {
		config = &WorkloadConfig{}
	}

	config.setDefaults()

	clusterNames := make([]string, 0, len(clusters))
	for _, cluster := range clusters {
		clusterNames = append(clusterNames, cluster.Name())
	}
	return &dataLoaderConfig{
		Name:            dataLoaderName,
		Image:           factory.GetDataLoaderImage(),
		Namespace:       clusters[0].Namespace(),
		SidecarVersions: factory.GetSidecarConfigs(),
		ClusterNames:    clusterNames,
		SecretName:      factory.GetSecretName(),
		Config:          config,
	}
}

// CreateDataLoaderIfAbsent will create the data loader for the provided cluster and load some random data into the cluster.
func (factory *Factory) CreateDataLoaderIfAbsent(cluster *FdbCluster) {
	factory.CreateDataLoaderIfAbsentWithWait(cluster, nil, true)
}

// CreateDataLoaderIfAbsentWithWaitForMultipleClusters will create a data loader configuration that loads data into multiple
// FoundationDB clusters.
func (factory *Factory) CreateDataLoaderIfAbsentWithWaitForMultipleClusters(clusters []*FdbCluster, config *WorkloadConfig, wait bool) {
	if !factory.options.enableDataLoading {
		return
	}

	dataLoaderJobTemplate := dataLoaderJob
	if factory.UseUnifiedImage() {
		dataLoaderJobTemplate = dataLoaderJobUnifiedImage
	}
	t, err := template.New("dataLoaderJob").Parse(dataLoaderJobTemplate)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	buf := bytes.Buffer{}
	gomega.Expect(t.Execute(&buf, factory.getDataLoaderConfig(clusters, config))).NotTo(gomega.HaveOccurred())
	decoder := yamlutil.NewYAMLOrJSONDecoder(&buf, 100000)
	for {
		var rawObj runtime.RawExtension
		err := decoder.Decode(&rawObj)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}

		obj, _, err := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme).
			Decode(rawObj.Raw, nil, nil)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		unstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		unstructuredObj := &unstructured.Unstructured{Object: unstructuredMap}

		gomega.Expect(
			factory.CreateIfAbsent(unstructuredObj),
		).NotTo(gomega.HaveOccurred())
	}

	if !wait {
		return
	}

	factory.WaitUntilDataLoaderIsDone(clusters[0])
	factory.DeleteDataLoader(clusters[0])
}

// CreateDataLoaderIfAbsentWithWait will create the data loader for the provided cluster and load some random data into the cluster.
// If wait is true, the method will wait until the data loader has finished.
func (factory *Factory) CreateDataLoaderIfAbsentWithWait(cluster *FdbCluster, config *WorkloadConfig, wait bool) {
	factory.CreateDataLoaderIfAbsentWithWaitForMultipleClusters([]*FdbCluster{cluster}, config, wait)
}

// DeleteDataLoader will delete the data loader job
func (factory *Factory) DeleteDataLoader(cluster *FdbCluster) {
	// Remove data loader Pods again, as the loading was done.
	err := factory.controllerRuntimeClient.Delete(context.Background(), &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dataLoaderName,
			Namespace: cluster.Namespace(),
		},
	})

	if err != nil && !k8serrors.IsNotFound(err) {
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}

	gomega.Expect(factory.controllerRuntimeClient.DeleteAllOf(context.Background(), &corev1.Pod{},
		client.InNamespace(cluster.Namespace()),
		client.MatchingLabels(map[string]string{"job-name": dataLoaderName}),
	)).NotTo(gomega.HaveOccurred())
}

// WaitUntilDataLoaderIsDone will wait until the data loader Job has finished.
func (factory *Factory) WaitUntilDataLoaderIsDone(cluster *FdbCluster) {
	printTime := time.Now()
	gomega.Eventually(func(g gomega.Gomega) int {
		pods := &corev1.PodList{}
		g.Expect(
			factory.controllerRuntimeClient.List(
				context.Background(),
				pods,
				client.InNamespace(cluster.Namespace()),
				client.MatchingLabels(map[string]string{"job-name": dataLoaderName}),
			),
		).NotTo(gomega.HaveOccurred())

		shouldPrint := time.Since(printTime) > 1*time.Minute
		if shouldPrint {
			log.Println("Pods:", len(pods.Items))

			job := &batchv1.Job{}
			g.Expect(
				factory.controllerRuntimeClient.Get(
					context.Background(),
					client.ObjectKey{
						Namespace: cluster.Namespace(),
						Name:      dataLoaderName,
					},
					job),
			).NotTo(gomega.HaveOccurred())

			for _, condition := range job.Status.Conditions {
				log.Println("Type:", condition.Type, "Reason", condition.Reason, "Message", condition.Message)
			}
		}

		var runningPods int
		for _, pod := range pods.Items {
			if shouldPrint {
				log.Println("Pod:", pod.Name, "Phase:", pod.Status.Phase, "Messages:", pod.Status.Message)
			}

			if pod.Status.Phase == corev1.PodRunning {
				runningPods++
			}
		}

		if shouldPrint {
			printTime = time.Now()
		}
		return runningPods
	}).WithTimeout(10 * time.Minute).WithPolling(5 * time.Second).Should(gomega.BeNumerically(">", 0))

	// Wait for at most 15 minutes to let the data load complete.
	gomega.Eventually(func(g gomega.Gomega) corev1.ConditionStatus {
		job := &batchv1.Job{}
		g.Expect(
			factory.controllerRuntimeClient.Get(
				context.Background(),
				client.ObjectKey{
					Namespace: cluster.Namespace(),
					Name:      dataLoaderName,
				},
				job),
		).NotTo(gomega.HaveOccurred())

		for _, condition := range job.Status.Conditions {
			if condition.Type == batchv1.JobComplete {
				return condition.Status
			}
		}

		return corev1.ConditionUnknown
	}).WithTimeout(15 * time.Minute).WithPolling(5 * time.Second).Should(gomega.Equal(corev1.ConditionTrue))
}
