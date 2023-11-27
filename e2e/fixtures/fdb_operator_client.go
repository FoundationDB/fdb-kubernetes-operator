/*
 * fdb_operator_client.go
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
	ctx "context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"strings"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	operatorDeploymentName     = "fdb-kubernetes-operator-controller-manager"
	foundationdbServiceAccount = "fdb-kubernetes"
	// operatorDeployment is a string that contains all the deployment settings used to deploy the operator.
	// Embedding it as a string make the test suite portable.
	operatorDeployment = `apiVersion: v1
kind: ServiceAccount
metadata:
  name: fdb-kubernetes-operator-controller-manager
  namespace: {{ .Namespace }}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: fdb-kubernetes-operator-manager-role
  namespace: {{ .Namespace }}
rules:
- apiGroups:
  - ""
  resources:
  - pods
  - configmaps
  - persistentvolumeclaims
  - events
  - secrets
  - services
  verbs:
  - get
  - watch
  - list
  - create
  - update
  - patch
  - delete
- apiGroups:
  - ""
  resources:
  - pods/exec
  verbs:
  - create
- apiGroups:
  - apps.foundationdb.org
  resources:
  - foundationdbclusters
  - foundationdbbackups
  - foundationdbrestores
  verbs:
  - get
  - list
  - watch
  - create
  - update
  - patch
  - delete
- apiGroups:
  - ""
  resources:
  - pods/exec
  verbs:
  - create
- apiGroups:
  - apps.foundationdb.org
  resources:
  - foundationdbclusters/status
  - foundationdbbackups/status
  - foundationdbrestores/status
  verbs:
  - get
  - update
  - patch
- apiGroups:
  - admissionregistration.k8s.io
  resources:
  - mutatingwebhookconfigurations
  - validatingwebhookconfigurations
  verbs:
  - get
  - list
  - watch
  - create
  - update
  - patch
  - delete
- apiGroups:
  - apps
  resources:
  - deployments
  verbs:
  - get
  - list
  - watch
  - create
  - update
  - patch
  - delete
- apiGroups:
  - coordination.k8s.io
  resources:
  - leases
  verbs:
  - get
  - list
  - watch
  - create
  - update
  - patch
  - delete
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: fdb-kubernetes-operator-manager-rolebinding
  namespace: {{ .Namespace }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: fdb-kubernetes-operator-manager-role
subjects:
- kind: ServiceAccount
  name: fdb-kubernetes-operator-controller-manager
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: {{ .Namespace }}-operator-manager-clusterrole
rules:
- apiGroups:
  - ""
  resources:
  - nodes
  verbs:
  - get
  - watch
  - list
---
  apiVersion: rbac.authorization.k8s.io/v1
  kind: ClusterRoleBinding
  metadata:
    name: {{ .Namespace }}-operator-manager-clusterrolebinding
  roleRef:
    apiGroup: rbac.authorization.k8s.io
    kind: ClusterRole
    name: {{ .Namespace }}-operator-manager-clusterrole
  subjects:
  - kind: ServiceAccount
    name: fdb-kubernetes-operator-controller-manager
    namespace: {{ .Namespace }}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: fdb-kubernetes-operator-controller-manager
    control-plane: controller-manager
  name: fdb-kubernetes-operator-controller-manager
  namespace: {{ .Namespace }}
spec:
  replicas: 2
  selector:
    matchLabels:
      app: fdb-kubernetes-operator-controller-manager
  template:
    metadata:
      labels:
        app: fdb-kubernetes-operator-controller-manager
        control-plane: controller-manager
      annotations:
        prometheus.io/scrape: 'true'
        prometheus.io/port: '8080'
    spec:
      initContainers:
        {{ range $index, $version := .SidecarVersions }}
        - name: foundationdb-kubernetes-init-{{ $index }}
          image: {{ .BaseImage }}:{{ .SidecarTag}}
          imagePullPolicy: {{ .ImagePullPolicy }}
          command:
            - /bin/bash
          # This is a workaround for a change of the version schema that was never tested/supported
          args:
            - -c
            - echo "{{ .FDBVersion.String }}" > /var/fdb/version && runuser -u fdb -g fdb -- /entrypoint.bash --copy-library {{ .FDBVersion.Compact }} --copy-binary fdbcli --copy-binary fdbbackup --copy-binary fdbrestore --output-dir /var/output-files/{{ .FDBVersion.String }} --init-mode
          volumeMounts:
            - name: fdb-binaries
              mountPath: /var/output-files
          securityContext:
            runAsUser: 0
            runAsGroup: 0
        # Install this library in a special location to force the operator to
        # use it as the primary library.
        {{ if eq .FDBVersion.Compact "7.1" }}
        - name: foundationdb-kubernetes-init-7-1-primary
          image: {{ .BaseImage }}:{{ .SidecarTag}}
          imagePullPolicy: {{ .ImagePullPolicy }}
          args:
            # Note that we are only copying a library, rather than copying any binaries. 
            - "--copy-library"
            - "{{ .FDBVersion.Compact }}"
            - "--output-dir"
            - "/var/output-files/primary" # Note that we use primary as the subdirectory rather than specifying the FoundationDB version like we did in the other examples.
            - "--init-mode"
          volumeMounts:
            - name: fdb-binaries
              mountPath: /var/output-files
        {{ end }}
        {{ end }}
      containers:
      - command:
        - /manager
        args:
        - --max-concurrent-reconciles=5
        - --zap-log-level=debug
        #- --server-side-apply
        image: {{ .OperatorImage }}
        name: manager
        imagePullPolicy: Always
        env:
          - name: LD_LIBRARY_PATH
            value: /usr/bin/fdb/primary/lib
          - name: WATCH_NAMESPACE
            valueFrom:
              fieldRef:
                fieldPath: metadata.namespace
          - name: FDB_TLS_CERTIFICATE_FILE
            value: /tmp/fdb-certs/tls.crt
          - name: FDB_TLS_CA_FILE
            value: /tmp/fdb-certs/ca.pem
          - name: FDB_TLS_KEY_FILE
            value: /tmp/fdb-certs/tls.key
          - name: FDB_NETWORK_OPTION_EXTERNAL_CLIENT_DIRECTORY
            value: /usr/bin/fdb
          # FDB 7.3 adds a check for loading external client library, which doesn't work with 6.3.
          # Consider remove this option once 6.3 is no longer being used.
          - name: FDB_NETWORK_OPTION_IGNORE_EXTERNAL_CLIENT_FAILURES
            value: ""
          - name: FDB_BLOB_CREDENTIALS
            value: /tmp/backup-credentials/credentials
          # TODO (johscheuer): once we can generate certificates per Pod remove this!
          - name: DISABLE_SIDECAR_TLS_CHECK
            value: "1"
          - name: FDB_NETWORK_OPTION_TRACE_ENABLE
            value: "/var/log/fdb"
          - name: FDB_NETWORK_OPTION_TRACE_FORMAT
            value: json
        ports:
          - name: metrics
            containerPort: 8080
        resources:
         requests:
           cpu: {{ .CPURequests }}
           memory: {{ .MemoryRequests }}
        securityContext:
          allowPrivilegeEscalation: false
          privileged: false
          readOnlyRootFilesystem: false
        volumeMounts:
        - mountPath: /tmp
          name: tmp
        - mountPath: /var/log/fdb
          name: fdb-trace-logs
        - name: fdb-certs
          mountPath: /tmp/fdb-certs
          readOnly: true
        - name: fdb-binaries
          mountPath: /usr/bin/fdb
        - name: backup-credentials
          mountPath: /tmp/backup-credentials
          readOnly: true
      securityContext:
        fsGroup: 4059
        runAsGroup: 4059
        runAsUser: 4059
      serviceAccountName: fdb-kubernetes-operator-controller-manager
      terminationGracePeriodSeconds: 10
      volumes:
      - emptyDir: {}
        name: tmp
      - emptyDir: {}
        name: fdb-trace-logs
      - name: backup-credentials
        secret:
          secretName: {{ .BackupSecretName }}
          optional: true
      - name: fdb-certs
        secret:
          secretName: {{ .SecretName }}
      - name: fdb-binaries
        emptyDir: {}`
)

// operatorConfig represents the configuration of the operator Deployment.
type operatorConfig struct {
	// OperatorImage represents the operator image that should be used in the Deployment.
	OperatorImage string
	// SecretName represents the Kubernetes secret that contains the certificates for communicating with the FoundationDB
	// cluster.
	SecretName string
	// BackupSecretName represents the secret that should be used to communicate with the backup blobstore.
	BackupSecretName string
	// SidecarVersions represents the sidecar configurations for different FoundationDB versions.
	SidecarVersions []SidecarConfig
	// Namespace represents the namespace for the Deployment and all associated resources
	Namespace string
	// ImagePullPolicy represents the pull policy for the operator container.
	ImagePullPolicy corev1.PullPolicy
	// CPURequests defined the CPU that should be requested.
	CPURequests string
	// MemoryRequests defined the Memory that should be requested.
	MemoryRequests string
}

// SidecarConfig represents the configuration for a sidecar. This can be used for templating.
type SidecarConfig struct {
	// BaseImage the image reference without a tag.
	BaseImage string
	// SidecarTag represents the image tag for this configuration.
	SidecarTag string
	// FDBVersion represents the FoundationDB version for this config.
	FDBVersion fdbv1beta2.Version
	// ImagePullPolicy represents the pull policy for the sidecar.
	ImagePullPolicy corev1.PullPolicy
}

// GetSidecarConfigs returns the sidecar configs. The sidecar configs can be used to template applications that will use
// all provided sidecar versions to inject FDB client libraries.
func (factory *Factory) GetSidecarConfigs() []SidecarConfig {
	additionalSidecarVersions := factory.GetAdditionalSidecarVersions()
	sidecarConfigs := make([]SidecarConfig, 0, len(additionalSidecarVersions)+1)

	sidecarConfigs = append(
		sidecarConfigs,
		getDefaultSidecarConfig(
			factory.GetSidecarImage(),
			factory.GetFDBVersion(),
			factory.getImagePullPolicy(),
		),
	)
	baseImage := sidecarConfigs[0].BaseImage

	// Add all other versions that are required e.g. for major or minor upgrades.
	for _, version := range additionalSidecarVersions {
		// Don't add the sidecar another time if we already added it
		if version.Equal(factory.GetFDBVersion()) {
			continue
		}

		sidecarConfigs = append(
			sidecarConfigs,
			getSidecarConfig(baseImage, "", version, factory.getImagePullPolicy()),
		)
	}

	return sidecarConfigs
}

func getDefaultSidecarConfig(sidecarImage string, version fdbv1beta2.Version, imagePullPolicy corev1.PullPolicy) SidecarConfig {
	defaultSidecarImage := strings.SplitN(sidecarImage, ":", 2)

	var tag string
	if len(defaultSidecarImage) > 1 {
		tag = defaultSidecarImage[1]
	}

	return getSidecarConfig(defaultSidecarImage[0], tag, version, imagePullPolicy)
}

func getSidecarConfig(baseImage string, tag string, version fdbv1beta2.Version, imagePullPolicy corev1.PullPolicy) SidecarConfig {
	if tag == "" {
		tag = fmt.Sprintf("%s-1", version)
	}

	return SidecarConfig{
		BaseImage:       baseImage,
		FDBVersion:      version,
		SidecarTag:      tag,
		ImagePullPolicy: imagePullPolicy,
	}
}

//nolint:revive
func (factory *Factory) getOperatorConfig(namespace string) *operatorConfig {
	cpuRequests := "500m"
	MemoryRequests := "1024Mi"

	if factory.options.cloudProvider == cloudProviderKind {
		cpuRequests = "0"
		MemoryRequests = "0"
	}

	return &operatorConfig{
		OperatorImage:    factory.GetOperatorImage(),
		SecretName:       factory.GetSecretName(),
		BackupSecretName: factory.GetBackupSecretName(),
		Namespace:        namespace,
		SidecarVersions:  factory.GetSidecarConfigs(),
		ImagePullPolicy:  factory.getImagePullPolicy(),
		CPURequests:      cpuRequests,
		MemoryRequests:   MemoryRequests,
	}
}

func (factory *Factory) ensureFDBOperatorExists(namespace string) error {
	// TODO: we also want to ensure that the CRDs are installed as an option
	return factory.CreateFDBOperatorIfAbsent(namespace)
}

// CreateFDBOperatorIfAbsent creates the operator Deployment based on the template.
func (factory *Factory) CreateFDBOperatorIfAbsent(namespace string) error {
	t, err := template.New("operatorDeployment").Parse(operatorDeployment)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	buf := bytes.Buffer{}
	gomega.Expect(t.Execute(&buf, factory.getOperatorConfig(namespace))).NotTo(gomega.HaveOccurred())
	decoder := yamlutil.NewYAMLOrJSONDecoder(&buf, 100000)

	for {
		var rawObj runtime.RawExtension
		err := decoder.Decode(&rawObj)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return err
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

	// Make sure the Operator Pods are running before moving forward.
	factory.WaitUntilOperatorPodsRunning(namespace)
	return nil
}

// GetOperatorPods returns the operator Pods in the provided namespace.
func (factory *Factory) GetOperatorPods(namespace string) *corev1.PodList {
	pods := &corev1.PodList{}
	gomega.Eventually(func() error {
		return factory.GetControllerRuntimeClient().
			List(ctx.TODO(), pods, client.InNamespace(namespace), client.MatchingLabels(map[string]string{"app": operatorDeploymentName}))
	}).WithTimeout(1 * time.Minute).WithPolling(1 * time.Second).ShouldNot(gomega.HaveOccurred())

	return pods
}

// WaitUntilOperatorPodsRunning waits until the Operator Pods are running.
func (factory *Factory) WaitUntilOperatorPodsRunning(namespace string) {
	deployment := &appsv1.Deployment{}
	gomega.Expect(
		factory.GetControllerRuntimeClient().
			Get(ctx.TODO(), client.ObjectKey{Name: operatorDeploymentName, Namespace: namespace}, deployment),
	).NotTo(gomega.HaveOccurred())

	expectedReplicas := int(pointer.Int32Deref(deployment.Spec.Replicas, 1))
	gomega.Eventually(func(g gomega.Gomega) int {
		pods := factory.GetOperatorPods(namespace)
		var runningReplicas int
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning && pod.DeletionTimestamp.IsZero() {
				runningReplicas++
				continue
			}

			// If the Pod is not running after 60 seconds we delete it and let the Deployment controller create a new Pod.
			if time.Since(pod.CreationTimestamp.Time).Seconds() > 120.0 {
				log.Println("operator Pod", pod.Name, "not running after 60 seconds, going to delete this Pod, status:", pod.Status)
				err := factory.GetControllerRuntimeClient().Delete(ctx.TODO(), &pod)
				if k8serrors.IsNotFound(err) {
					continue
				}

				g.Expect(err).NotTo(gomega.HaveOccurred())
			}
		}

		return runningReplicas
	}).WithTimeout(10 * time.Minute).WithPolling(2 * time.Second).Should(gomega.BeNumerically(">=", expectedReplicas))
}

// RecreateOperatorPods will recreate all operator Pods in the specified namespace and wait until the new Pods are
// up and running.
func (factory *Factory) RecreateOperatorPods(namespace string) {
	gomega.Expect(
		factory.GetControllerRuntimeClient().
			DeleteAllOf(ctx.TODO(), &corev1.Pod{}, client.InNamespace(namespace), client.MatchingLabels(map[string]string{"app": operatorDeploymentName})),
	).NotTo(gomega.HaveOccurred())

	factory.WaitUntilOperatorPodsRunning(namespace)
}
