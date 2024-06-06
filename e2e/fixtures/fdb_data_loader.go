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
	"github.com/onsi/gomega"
	"io"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"text/template"
	"time"
)

const (
	// The name of the data loader Job.
	dataLoaderName = "fdb-data-loader"

	// For now we only load 2GB into the cluster, we can increase this later if we want.
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
        - --keys=1000000
        - --batch-size=50
        - --value-size=1000
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
          - name: FDB_NETWORK_OPTION_EXTERNAL_CLIENT_DIRECTORY
            value: /var/dynamic/fdb/libs
          - name: PYTHONUNBUFFERED
            value: "on"
        volumeMounts:
          - name: config-map
            mountPath: /var/dynamic-conf
          - name: fdb-libs
            mountPath: /var/dynamic/fdb
          - name: fdb-certs
            mountPath: /tmp/fdb-certs
            readOnly: true
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
        {{ if eq .FDBVersion.Compact "7.1" }}
        - name: foundationdb-kubernetes-init-7-1-primary
          image: {{ .Image }}
          imagePullPolicy: {{ .ImagePullPolicy }}
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
            - mkdir -p /var/dynamic/fdb/libs && {{ range $index, $version := .SidecarVersions -}} cp /var/dynamic/fdb/{{ .FDBVersion.Compact }}/lib/libfdb_c.so /var/dynamic/fdb/libs/libfdb_{{ .FDBVersion.Compact }}_c.so && {{ end }} cp /var/dynamic-conf/fdb.cluster /var/dynamic/fdb/fdb.cluster
          volumeMounts:
          - name: config-map
            mountPath: /var/dynamic-conf
          - name: fdb-libs
            mountPath: /var/dynamic/fdb
          - name: fdb-certs
            mountPath: /tmp/fdb-certs
            readOnly: true
      restartPolicy: Never
      volumes:
        - name: config-map
          configMap:
            name: {{ .ClusterName }}-config
            items:
              - key: cluster-file
                path: fdb.cluster
        - name: fdb-libs
          emptyDir: {}
        - name: fdb-certs
          secret:
            secretName: {{ .SecretName }}`

	// For now we only load 2GB into the cluster, we can increase this later if we want.
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
        - --keys=1000000
        - --batch-size=50
        - --value-size=1000
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
            value: /var/dynamic/fdb
          - name: FDB_NETWORK_OPTION_TRACE_LOG_GROUP
            value: {{ .Name }}
          - name: FDB_NETWORK_OPTION_EXTERNAL_CLIENT_DIRECTORY
            value: /var/dynamic/fdb
          - name: PYTHONUNBUFFERED
            value: "on"
        volumeMounts:
          - name: config-map
            mountPath: /var/dynamic-conf
          - name: fdb-libs
            mountPath: /var/dynamic/fdb
          - name: fdb-certs
            mountPath: /tmp/fdb-certs
            readOnly: true
        resources:
         requests:
           cpu: "1"
           memory: 4Gi
      initContainers:
        {{ range $index, $version := .SidecarVersions }}
        - name: foundationdb-kubernetes-init-{{ $index }}
          image: {{ .Image }}
          imagePullPolicy: {{ .ImagePullPolicy }}
          args:
            - --mode
            - init
            - --output-dir
            - /var/output-files
            - --copy-library
            - "{{ .FDBVersion.Compact }}"
{{ if .CopyAsPrimary }}
            - --copy-primary-library
            - "{{ .FDBVersion.Compact }}"
{{ end }}
          volumeMounts:
            - name: fdb-libs
              mountPath: /var/output-files
          securityContext:
            runAsUser: 0
            runAsGroup: 0
{{ if .CopyAsPrimary }}
        - name: foundationdb-kubernetes-init-cluster-file
          image: {{ .Image }}
          imagePullPolicy: {{ .ImagePullPolicy }}
          args:
            - --mode
            - init
            - --input-dir
            - /var/dynamic-conf
            - --output-dir
            - /var/output-files
            - --copy-file
            - fdb.cluster
            - --require-not-empty
            - fdb.cluster
          volumeMounts:
            - name: fdb-libs
              mountPath: /var/output-files
            - name: config-map
              mountPath: /var/dynamic-conf
          securityContext:
            runAsUser: 0
            runAsGroup: 0
{{ end }}
        {{ end }}
      restartPolicy: Never
      volumes:
        - name: config-map
          configMap:
            name: {{ .ClusterName }}-config
            items:
              - key: cluster-file
                path: fdb.cluster
        - name: fdb-libs
          emptyDir: {}
        - name: fdb-certs
          secret:
            secretName: {{ .SecretName }}`
)

// dataLoaderConfig represents the configuration of the Dataloader Job.
type dataLoaderConfig struct {
	// Name of the data loader Job.
	Name string
	// Image represents the data loader image that should be used in the Job.
	Image string
	// SidecarVersions represents the sidecar configurations for different FoundationDB versions.
	SidecarVersions []SidecarConfig
	// Namespace represents the namespace for the Deployment and all associated resources
	Namespace string
	// ClusterName the name of the cluster to load data into.
	ClusterName string
	// SecretName represents the Kubernetes secret that contains the certificates for communicating with the FoundationDB
	// cluster.
	SecretName string
}

func (factory *Factory) getDataLoaderConfig(cluster *FdbCluster) *dataLoaderConfig {
	return &dataLoaderConfig{
		Name:            dataLoaderName,
		Image:           factory.GetDataLoaderImage(),
		Namespace:       cluster.Namespace(),
		SidecarVersions: factory.GetSidecarConfigs(),
		ClusterName:     cluster.Name(),
		SecretName:      factory.GetSecretName(),
	}
}

// CreateDataLoaderIfAbsent will create the data loader for the provided cluster and load some random data into the cluster.
func (factory *Factory) CreateDataLoaderIfAbsent(cluster *FdbCluster) {
	if !factory.options.enableDataLoading {
		return
	}

	dataLoaderJobTemplate := dataLoaderJob
	if factory.options.featureOperatorUnifiedImage {
		dataLoaderJobTemplate = dataLoaderJobUnifiedImage
	}
	t, err := template.New("dataLoaderJob").Parse(dataLoaderJobTemplate)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	buf := bytes.Buffer{}
	gomega.Expect(t.Execute(&buf, factory.getDataLoaderConfig(cluster))).NotTo(gomega.HaveOccurred())
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

	factory.WaitUntilDataLoaderIsDone(cluster)

	// Remove data loader Pods again, as the loading was done.
	gomega.Expect(factory.controllerRuntimeClient.Delete(context.Background(), &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dataLoaderName,
			Namespace: cluster.Namespace(),
		},
	})).NotTo(gomega.HaveOccurred())

	gomega.Expect(factory.controllerRuntimeClient.DeleteAllOf(context.Background(), &corev1.Pod{},
		client.InNamespace(cluster.Namespace()),
		client.MatchingLabels(map[string]string{"job-name": dataLoaderName}),
	)).NotTo(gomega.HaveOccurred())
}

// WaitUntilDataLoaderIsDone will wait until the data loader Job has finished.
func (factory *Factory) WaitUntilDataLoaderIsDone(cluster *FdbCluster) {
	gomega.Eventually(func() int {
		pods := &corev1.PodList{}
		gomega.Expect(
			factory.controllerRuntimeClient.List(
				context.Background(),
				pods,
				client.InNamespace(cluster.Namespace()),
				client.MatchingLabels(map[string]string{"job-name": dataLoaderName}),
			),
		).NotTo(gomega.HaveOccurred())

		var runningPods int
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning {
				runningPods++
			}
		}

		return runningPods
	}).WithTimeout(5 * time.Minute).WithPolling(5 * time.Second).Should(gomega.BeNumerically(">", 0))

	// Wait for at most 15 minutes to let the data load complete.
	gomega.Eventually(func() corev1.ConditionStatus {
		job := &batchv1.Job{}
		gomega.Expect(
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
