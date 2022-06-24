/*
 * pod_client.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2019 Apple Inc. and the FoundationDB project authors
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
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podclient"
	monitorapi "github.com/apple/foundationdb/fdbkubernetesmonitor/api"
	"github.com/go-logr/logr"
	"github.com/hashicorp/go-retryablehttp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"
)

// FDBImageType describes a type of image a pod or cluster is using.
type FDBImageType string

const (
	// MockUnreachableAnnotation defines if a Pod should be unreachable. This annotation
	// is currently only used for testing cases.
	MockUnreachableAnnotation = "foundationdb.org/mock-unreachable"

	// FDBImageTypeUnified indicates that a pod is using a unified image for the
	// main container and sidecar container.
	FDBImageTypeUnified FDBImageType = "unified"

	// FDBImageTypeSplit indicates that a pod is using a different image for the
	// main container and sidecar container.
	FDBImageTypeSplit FDBImageType = "split"

	// CurrentConfigurationAnnotation is the annotation we use to store the
	// latest configuration.
	CurrentConfigurationAnnotation = "foundationdb.org/launcher-current-configuration"

	// EnvironmentAnnotation is the annotation we use to store the environment
	// variables.
	EnvironmentAnnotation = "foundationdb.org/launcher-environment"
)

// realPodSidecarClient provides a client for use in real environments, using
// the Kubernetes sidecar.
type realFdbPodSidecarClient struct {
	// Cluster is the cluster we are connecting to.
	Cluster *fdbv1beta2.FoundationDBCluster

	// Pod is the pod we are connecting to.
	Pod *corev1.Pod

	// useTLS indicates whether this is using a TLS connection to the sidecar.
	useTLS bool

	// tlsConfig contains the TLS configuration for the connection to the
	// sidecar.
	tlsConfig *tls.Config

	// logger is used to add common fields to log messages.
	logger logr.Logger

	// getTimeout defines the timeout for get requests
	getTimeout time.Duration

	// postTimeout defines the timeout for post requests
	postTimeout time.Duration
}

// realPodSidecarClient provides a client for use in real environments, using
// the annotations from the unified Kubernetes image.
type realFdbPodAnnotationClient struct {
	// Cluster is the cluster we are connecting to.
	Cluster *fdbv1beta2.FoundationDBCluster

	// Pod is the pod we are connecting to.
	Pod *corev1.Pod

	// logger is used to add common fields to log messages.
	logger logr.Logger
}

// NewFdbPodClient builds a client for working with an FDB Pod
func NewFdbPodClient(cluster *fdbv1beta2.FoundationDBCluster, pod *corev1.Pod, log logr.Logger, getTimeout time.Duration, postTimeout time.Duration) (podclient.FdbPodClient, error) {
	if GetImageType(pod) == FDBImageTypeUnified {
		return &realFdbPodAnnotationClient{Cluster: cluster, Pod: pod, logger: log}, nil
	}

	if pod.Status.PodIP == "" {
		return nil, fmt.Errorf("waiting for pod %s/%s/%s to be assigned an IP", cluster.Namespace, cluster.Name, pod.Name)
	}
	for _, container := range pod.Status.ContainerStatuses {
		if container.Name == "foundationdb-kubernetes-sidecar" && !container.Ready {
			return nil, fmt.Errorf("waiting for pod %s/%s/%s to be ready", cluster.Namespace, cluster.Name, pod.Name)
		}
	}

	useTLS := podHasSidecarTLS(pod)

	var tlsConfig = &tls.Config{}
	if useTLS {
		certFile := os.Getenv("FDB_TLS_CERTIFICATE_FILE")
		keyFile := os.Getenv("FDB_TLS_KEY_FILE")
		caFile := os.Getenv("FDB_TLS_CA_FILE")

		if certFile == "" || keyFile == "" || caFile == "" {
			return nil, errors.New("missing one or more TLS env vars: FDB_TLS_CERTIFICATE_FILE, FDB_TLS_KEY_FILE or FDB_TLS_CA_FILE")
		}

		cert, err := tls.LoadX509KeyPair(
			certFile,
			keyFile,
		)
		if err != nil {
			return nil, err
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
		if os.Getenv("DISABLE_SIDECAR_TLS_CHECK") == "1" {
			tlsConfig.InsecureSkipVerify = true
		}
		certPool := x509.NewCertPool()
		caList, err := os.ReadFile(caFile)
		if err != nil {
			return nil, err
		}
		certPool.AppendCertsFromPEM(caList)
		tlsConfig.RootCAs = certPool
	}

	return &realFdbPodSidecarClient{Cluster: cluster, Pod: pod, useTLS: useTLS, tlsConfig: tlsConfig, logger: log, getTimeout: getTimeout, postTimeout: postTimeout}, nil
}

// getListenIP gets the IP address that a pod listens on.
func (client *realFdbPodSidecarClient) getListenIP() string {
	ips := GetPublicIPsForPod(client.Pod, client.logger)
	if len(ips) > 0 {
		return ips[0]
	}

	return ""
}

// makeRequest submits a request to the sidecar.
func (client *realFdbPodSidecarClient) makeRequest(method, path string) (string, int, error) {
	var resp *http.Response
	var err error

	protocol := "http"
	retryClient := retryablehttp.NewClient()
	retryClient.RetryMax = 2
	retryClient.RetryWaitMax = 1 * time.Second
	// Prevent logging
	retryClient.Logger = nil
	retryClient.CheckRetry = retryablehttp.ErrorPropagatedRetryPolicy

	if client.useTLS {
		retryClient.HTTPClient.Transport = &http.Transport{TLSClientConfig: client.tlsConfig}
		protocol = "https"
	}

	url := fmt.Sprintf("%s://%s:8080/%s", protocol, client.getListenIP(), path)
	switch method {
	case http.MethodGet:
		retryClient.HTTPClient.Timeout = client.getTimeout
		resp, err = retryClient.Get(url)
	case http.MethodPost:
		retryClient.HTTPClient.Timeout = client.postTimeout
		resp, err = retryClient.Post(url, "application/json", strings.NewReader(""))
	default:
		return "", 0, fmt.Errorf("unknown HTTP method %s", method)
	}

	if err != nil {
		return "", 0, err
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	bodyText := string(body)

	if err != nil {
		return "", resp.StatusCode, err
	}

	return bodyText, resp.StatusCode, nil
}

// IsPresent checks whether a file in the sidecar is present.
func (client *realFdbPodSidecarClient) IsPresent(filename string) (bool, error) {
	version, err := fdbv1beta2.ParseFdbVersion(client.Cluster.Spec.Version)
	if err != nil {
		return false, nil
	}

	path := "check_hash"
	// This endpoint was added in 7.1.4 and only checks if a file is present without calculating the hash of a file.
	// The benefit of this approach is that the resource requirements for the sidecar is reduced and for larger files
	// e.g. with debug symbols the response will be faster.
	if version.SupportsIsPresent() {
		path = "is_present"
	}

	_, code, err := client.makeRequest("GET", fmt.Sprintf("%s/%s", path, filename))
	if err != nil || code != http.StatusOK {
		client.logger.Info("Waiting for file", "file", filename, "response_code", code)
		return false, err
	}

	return code == http.StatusOK, nil
}

// CheckHash checks whether a file in the sidecar has the expected contents.
func (client *realFdbPodSidecarClient) checkHash(filename string, contents string) (bool, error) {
	response, _, err := client.makeRequest("GET", fmt.Sprintf("check_hash/%s", filename))
	if err != nil {
		return false, err
	}

	expectedHash := sha256.Sum256([]byte(contents))
	expectedHashString := hex.EncodeToString(expectedHash[:])
	return strings.Compare(expectedHashString, response) == 0, nil
}

// GenerateMonitorConf updates the monitor conf file for a pod
func (client *realFdbPodSidecarClient) generateMonitorConf() error {
	_, _, err := client.makeRequest("POST", "copy_monitor_conf")
	return err
}

// copyFiles copies the files from the config map to the shared dynamic conf
// volume
func (client *realFdbPodSidecarClient) copyFiles() error {
	_, _, err := client.makeRequest("POST", "copy_files")
	return err
}

// GetVariableSubstitutions gets the current keys and values that this
// process group will substitute into its monitor conf.
func (client *realFdbPodSidecarClient) GetVariableSubstitutions() (map[string]string, error) {
	contents, _, err := client.makeRequest("GET", "substitutions")
	if err != nil {
		return nil, err
	}
	substitutions := map[string]string{}
	err = json.Unmarshal([]byte(contents), &substitutions)
	if err != nil {
		client.logger.Error(err, "Error deserializing pod substitutions", "responseBody", contents)
	}
	return substitutions, err
}

// UpdateFile checks if a file is up-to-date and tries to update it.
func (client *realFdbPodSidecarClient) UpdateFile(name string, contents string) (bool, error) {
	if name == "fdbmonitor.conf" {
		return client.updateDynamicFiles(name, contents, func(client *realFdbPodSidecarClient) error { return client.generateMonitorConf() })
	}
	return client.updateDynamicFiles(name, contents, func(client *realFdbPodSidecarClient) error { return client.copyFiles() })
}

// updateDynamicFiles checks if the files in the dynamic conf volume match the
// expected contents, and tries to copy the latest files from the input volume
// if they do not.
func (client *realFdbPodSidecarClient) updateDynamicFiles(filename string, contents string, updateFunc func(client *realFdbPodSidecarClient) error) (bool, error) {
	match := false
	var err error

	match, err = client.checkHash(filename, contents)
	if err != nil {
		return false, err
	}

	if !match {
		err = updateFunc(client)
		if err != nil {
			return false, err
		}
		// We check this more or less instantly, maybe we should add some delay?
		match, err = client.checkHash(filename, contents)
		if !match {
			client.logger.Info("Waiting for config update", "file", filename)
		}

		return match, err
	}

	return true, nil
}

// GetVariableSubstitutions gets the current keys and values that this
// instance will substitute into its monitor conf.
func (client *realFdbPodAnnotationClient) GetVariableSubstitutions() (map[string]string, error) {
	environmentData, present := client.Pod.Annotations[EnvironmentAnnotation]
	if !present {
		client.logger.Info("Waiting for Kubernetes monitor to update annotations", "annotation", EnvironmentAnnotation)
		return nil, nil
	}
	environment := make(map[string]string)
	err := json.Unmarshal([]byte(environmentData), &environment)
	if err != nil {
		return nil, err
	}

	return environment, nil
}

// UpdateFile checks if a file is up-to-date and tries to update it.
func (client *realFdbPodAnnotationClient) UpdateFile(name string, contents string) (bool, error) {
	if name == "fdb.cluster" {
		// We can ignore cluster file updates in the unified image.
		return true, nil
	}
	if name == "fdbmonitor.conf" {
		desiredConfiguration := monitorapi.ProcessConfiguration{}
		err := json.Unmarshal([]byte(contents), &desiredConfiguration)
		if err != nil {
			client.logger.Error(err, "Error parsing desired process configuration", "input", contents)
			return false, err
		}
		currentConfiguration := monitorapi.ProcessConfiguration{}
		currentData, present := client.Pod.Annotations[CurrentConfigurationAnnotation]
		if !present {
			client.logger.Info("Waiting for Kubernetes monitor to update annotations", "annotation", currentConfiguration)
			return false, nil
		}
		err = json.Unmarshal([]byte(currentData), &currentConfiguration)
		if err != nil {
			client.logger.Error(err, "Error parsing current process configuration", "input", currentData)
			return false, err
		}
		match := reflect.DeepEqual(currentConfiguration, desiredConfiguration)
		if !match {
			client.logger.Info("Waiting for Kubernetes monitor config update",
				"desired", desiredConfiguration, "current", currentConfiguration)
		}
		return match, nil
	}
	return false, fmt.Errorf("unknown file %s", name)
}

// IsPresent checks whether a file in the sidecar is present.
// This implementation always returns true, because the unified image handles
// these checks internally.
func (client *realFdbPodAnnotationClient) IsPresent(_ string) (bool, error) {
	return true, nil
}

// MockFdbPodClient provides a mock connection to a pod
type mockFdbPodClient struct {
	Cluster *fdbv1beta2.FoundationDBCluster
	Pod     *corev1.Pod
	logger  logr.Logger
}

// NewMockFdbPodClient builds a mock client for working with an FDB pod
func NewMockFdbPodClient(cluster *fdbv1beta2.FoundationDBCluster, pod *corev1.Pod) (podclient.FdbPodClient, error) {
	return &mockFdbPodClient{Cluster: cluster, Pod: pod, logger: logr.New(logf.NullLogSink{})}, nil
}

// UpdateFile checks if a file is up-to-date and tries to update it.
func (client *mockFdbPodClient) UpdateFile(_ string, _ string) (bool, error) {
	return true, nil
}

// IsPresent checks whether a file in the sidecar is present.
func (client *mockFdbPodClient) IsPresent(_ string) (bool, error) {
	return true, nil
}

// GetVariableSubstitutions gets the current keys and values that this
// process group will substitute into its monitor conf.
func (client *mockFdbPodClient) GetVariableSubstitutions() (map[string]string, error) {
	substitutions := map[string]string{}

	if client.Pod.Annotations != nil {
		if _, ok := client.Pod.Annotations[MockUnreachableAnnotation]; ok {
			return substitutions, &net.OpError{Op: "mock", Err: fmt.Errorf("not reachable")}
		}
	}

	ipString := GetPublicIPsForPod(client.Pod, client.logger)[0]
	substitutions["FDB_PUBLIC_IP"] = ipString
	if ipString != "" {
		ip := net.ParseIP(ipString)
		if ip == nil {
			return nil, fmt.Errorf("failed to parse IP from pod: %s", ipString)
		}

		if ip.To4() == nil {
			substitutions["FDB_PUBLIC_IP"] = fmt.Sprintf("[%s]", ipString)
		}
	}
	substitutions["FDB_POD_IP"] = substitutions["FDB_PUBLIC_IP"]

	if client.Cluster.Spec.FaultDomain.Key == "foundationdb.org/none" {
		substitutions["FDB_MACHINE_ID"] = client.Pod.Name
		substitutions["FDB_ZONE_ID"] = client.Pod.Name
	} else if client.Cluster.Spec.FaultDomain.Key == "foundationdb.org/kubernetes-cluster" {
		substitutions["FDB_MACHINE_ID"] = client.Pod.Spec.NodeName
		substitutions["FDB_ZONE_ID"] = client.Cluster.Spec.FaultDomain.Value
	} else {
		faultDomainSource := client.Cluster.Spec.FaultDomain.ValueFrom
		if faultDomainSource == "" {
			faultDomainSource = "spec.nodeName"
		}
		substitutions["FDB_MACHINE_ID"] = client.Pod.Spec.NodeName

		if faultDomainSource == "spec.nodeName" {
			substitutions["FDB_ZONE_ID"] = client.Pod.Spec.NodeName
		} else {
			return nil, fmt.Errorf("unsupported fault domain source %s", faultDomainSource)
		}
	}

	substitutions["FDB_INSTANCE_ID"] = GetProcessGroupIDFromMeta(client.Cluster, client.Pod.ObjectMeta)

	if client.Cluster.IsBeingUpgraded() {
		substitutions["BINARY_DIR"] = fmt.Sprintf("/var/dynamic-conf/bin/%s", client.Cluster.Spec.Version)
	} else {
		substitutions["BINARY_DIR"] = "/usr/bin"
	}

	copyableSubstitutions := map[string]fdbv1beta2.None{
		"FDB_DNS_NAME":    {},
		"FDB_INSTANCE_ID": {},
	}
	for _, container := range client.Pod.Spec.Containers {
		for _, envVar := range container.Env {
			_, copyable := copyableSubstitutions[envVar.Name]
			if copyable {
				substitutions[envVar.Name] = envVar.Value
			}
		}
	}

	return substitutions, nil
}

// podHasSidecarTLS determines whether a pod currently has TLS enabled for the
// sidecar process.
func podHasSidecarTLS(pod *corev1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		if container.Name == "foundationdb-kubernetes-sidecar" {
			for _, arg := range container.Args {
				if arg == "--tls" {
					return true
				}
			}
		}
	}

	return false
}

// GetImageType determines whether a pod is using the unified or the split
// image.
func GetImageType(pod *corev1.Pod) FDBImageType {
	for _, container := range pod.Spec.Containers {
		if container.Name != "foundationdb" {
			continue
		}
		for _, envVar := range container.Env {
			if envVar.Name == "FDB_IMAGE_TYPE" {
				return FDBImageType(envVar.Value)
			}
		}
	}
	return FDBImageTypeSplit
}

// GetDesiredImageType determines whether a cluster is configured to use the
// unified or the split image.
func GetDesiredImageType(cluster *fdbv1beta2.FoundationDBCluster) FDBImageType {
	if pointer.BoolDeref(cluster.Spec.UseUnifiedImage, false) {
		return FDBImageTypeUnified
	}
	return FDBImageTypeSplit
}
