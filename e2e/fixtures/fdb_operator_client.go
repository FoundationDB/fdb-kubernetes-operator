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
	"html/template"
	"io"
	"log"
	"time"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	foundationdbNodeRole       = "fdb-kubernetes-node-watcher"
	// The configuration for the RBAC setup for the operator deployment
	operatorRBAC = `apiVersion: v1
kind: ServiceAccount
metadata:
  name: fdb-kubernetes-operator-controller-manager
  namespace: {{ .Namespace }}
  labels:
     foundationdb.org/testing: chaos
     foundationdb.org/user: {{ .User }}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: fdb-kubernetes-operator-manager-role
  namespace: {{ .Namespace }}
  labels:
     foundationdb.org/testing: chaos
     foundationdb.org/user: {{ .User }}
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
  - get
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
  labels:
     foundationdb.org/testing: chaos
     foundationdb.org/user: {{ .User }}
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
  labels:
     foundationdb.org/testing: chaos
     foundationdb.org/user: {{ .User }}
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
  labels:
     foundationdb.org/testing: chaos
     foundationdb.org/user: {{ .User }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: {{ .Namespace }}-operator-manager-clusterrole
subjects:
  - kind: ServiceAccount
    name: fdb-kubernetes-operator-controller-manager
    namespace: {{ .Namespace }}`
	// operatorDeployment is a string that contains all the deployment settings used to deploy the operator.
	// Embedding it as a string make the test suite portable.
	operatorDeployment = `
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
          image: {{ .Image }}
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
        {{ if .CopyAsPrimary }}
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
            - name: fdb-binaries
              mountPath: /var/output-files
        {{ end }}
        {{ end }}
      containers:
      - command:
        - /manager
        args:` + operatorArgs +
		`
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
	// operatorDeploymentUnifiedImage ...
	operatorDeploymentUnifiedImage = `
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
          image: {{ .Image }}
          imagePullPolicy: {{ .ImagePullPolicy }}
          args:
            - --mode
            - init
            - --output-dir
            - /var/output-files
            - --copy-binary
            - fdbcli
            - --copy-binary
            - fdbbackup
            - --copy-binary
            - fdbrestore
            - --copy-library
            - "{{ .FDBVersion.Compact }}"
{{ if .CopyAsPrimary }}
            - --copy-primary-library
            - "{{ .FDBVersion.Compact }}"
{{ end }}
          volumeMounts:
            - name: fdb-binaries
              mountPath: /var/output-files
          securityContext:
            runAsUser: 0
            runAsGroup: 0
        {{ end }}
      containers:
      - command:
        - /manager
        args:` + operatorArgs +
		`
        image: {{ .OperatorImage }}
        name: manager
        imagePullPolicy: Always
        env:
          - name: LD_LIBRARY_PATH
            value: /usr/bin/fdb
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
          - name: FDB_NETWORK_OPTION_CLIENT_THREADS_PER_VERSION
            value: "10"
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
	operatorArgs = `
          - --max-concurrent-reconciles=5
          - --zap-log-level=debug
          - --minimum-required-uptime-for-cc-bounce=60s
{{ if .EnableServerSideApply }}
          - --server-side-apply
{{ end }}
          # We are setting low values here as the e2e test are taking down processes multiple times
          # and having a high wait time between recoveries will increase the reliability of the cluster but also
          # increase the time our e2e test take.
          - --minimum-recovery-time-for-inclusion=1.0
          - --minimum-recovery-time-for-exclusion=1.0
          - --cluster-label-key-for-node-trigger=foundationdb.org/fdb-cluster-name
          - --enable-node-index
          - --replace-on-security-context-change
`
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
	// Defines the user that runs the current e2e tests.
	User string
	// EnableServerSideApply if true, the operator will make use of server side apply.
	EnableServerSideApply bool
}

// SidecarConfig represents the configuration for a sidecar. This can be used for templating.
type SidecarConfig struct {
	// Image the image reference with the tag.
	Image string
	// FDBVersion represents the FoundationDB version for this config.
	FDBVersion fdbv1beta2.Version
	// ImagePullPolicy represents the pull policy for the sidecar.
	ImagePullPolicy corev1.PullPolicy
	// CopyAsPrimary if true the version should be copied as primary library.
	CopyAsPrimary bool
}

// getSidecarConfigs returns the sidecar config based on the provided imageConfigs.
func (factory *Factory) getSidecarConfigs(imageConfigs []fdbv1beta2.ImageConfig) []SidecarConfig {
	var hasCopyPrimarySet bool
	additionalSidecarVersions := factory.GetAdditionalSidecarVersions()
	sidecarConfigs := make([]SidecarConfig, 0, len(additionalSidecarVersions)+1)
	pullPolicy := factory.getImagePullPolicy()

	defaultConfig := SidecarConfig{
		Image:           fdbv1beta2.SelectImageConfig(imageConfigs, factory.GetFDBVersionAsString()).Image(),
		FDBVersion:      factory.GetFDBVersion(),
		ImagePullPolicy: pullPolicy,
	}

	if factory.GetFDBVersion().SupportsDNSInClusterFile() {
		defaultConfig.CopyAsPrimary = true
		hasCopyPrimarySet = true
	}

	sidecarConfigs = append(
		sidecarConfigs,
		defaultConfig,
	)

	// Add all other versions that are required e.g. for major or minor upgrades.
	for _, version := range additionalSidecarVersions {
		// Don't add the sidecar another time if we already added a protocol compatible version.
		if version.IsProtocolCompatible(factory.GetFDBVersion()) {
			continue
		}

		sidecarConfig := SidecarConfig{
			Image:           fdbv1beta2.SelectImageConfig(imageConfigs, version.String()).Image(),
			FDBVersion:      version,
			ImagePullPolicy: pullPolicy,
		}

		if !hasCopyPrimarySet && version.SupportsDNSInClusterFile() {
			sidecarConfig.CopyAsPrimary = true
			hasCopyPrimarySet = true
		}

		sidecarConfigs = append(
			sidecarConfigs,
			sidecarConfig,
		)
	}

	return sidecarConfigs
}

// GetSidecarConfigs returns the sidecar configs. The sidecar configs can be used to template applications that will use
// all provided sidecar versions to inject FDB client libraries.
func (factory *Factory) GetSidecarConfigs() []SidecarConfig {
	if factory.UseUnifiedImage() {
		return factory.getSidecarConfigs(factory.GetMainContainerOverrides(false, true).ImageConfigs)
	}

	return factory.getSidecarConfigs(factory.GetSidecarContainerOverrides(false).ImageConfigs)
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
		OperatorImage:         factory.GetOperatorImage(),
		SecretName:            factory.GetSecretName(),
		BackupSecretName:      factory.GetBackupSecretName(),
		Namespace:             namespace,
		SidecarVersions:       factory.GetSidecarConfigs(),
		ImagePullPolicy:       factory.getImagePullPolicy(),
		CPURequests:           cpuRequests,
		MemoryRequests:        MemoryRequests,
		User:                  factory.options.username,
		EnableServerSideApply: factory.options.featureOperatorServerSideApply,
	}
}

func (factory *Factory) ensureFDBOperatorExists(namespace string) error {
	// TODO: we also want to ensure that the CRDs are installed as an option
	return factory.CreateFDBOperatorIfAbsent(namespace)
}

// CreateFDBOperatorIfAbsent creates the operator Deployment based on the template.
func (factory *Factory) CreateFDBOperatorIfAbsent(namespace string) error {
	operatorRBACTemplate, err := template.New("operatorRBAC").Parse(operatorRBAC)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	buf := bytes.Buffer{}
	gomega.Expect(operatorRBACTemplate.Execute(&buf, factory.getOperatorConfig(namespace))).NotTo(gomega.HaveOccurred())
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

	// Make sure we delete the cluster scoped objects.
	factory.AddShutdownHook(func() error {
		factory.Delete(&rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:      namespace + "-operator-manager-clusterrole",
				Namespace: namespace,
			},
		})

		factory.Delete(&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      namespace + "-operator-manager-clusterrolebinding",
				Namespace: namespace,
			},
		})

		return nil
	})

	deploymentTemplate := operatorDeployment
	if factory.UseUnifiedImage() {
		deploymentTemplate = operatorDeploymentUnifiedImage
	}

	operatorDeploymentTemplate, err := template.New("operatorDeployment").Parse(deploymentTemplate)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	buf = bytes.Buffer{}
	gomega.Expect(operatorDeploymentTemplate.Execute(&buf, factory.getOperatorConfig(namespace))).NotTo(gomega.HaveOccurred())
	decoder = yamlutil.NewYAMLOrJSONDecoder(&buf, 100000)

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

			// If the Pod is not running after 120 seconds we delete it and let the Deployment controller create a new Pod.
			if time.Since(pod.CreationTimestamp.Time).Seconds() > 120.0 {
				log.Println("operator Pod", pod.Name, "not running after 120 seconds, going to delete this Pod, status:", pod.Status)
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
