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

package controllers

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"sync"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

// FdbPodClient provides methods for working with a FoundationDB pod
type FdbPodClient interface {
	// GetCluster returns the cluster associated with a client
	GetCluster() *fdbtypes.FoundationDBCluster

	// GetPod returns the pod associated with a client
	GetPod() *corev1.Pod

	// IsPresent checks whether a file in the sidecar is present
	IsPresent(filename string) (bool, error)

	// CheckHash checks whether a file in the sidecar has the expected contents.
	CheckHash(filename string, contents string) (bool, error)

	// GenerateMonitorConf updates the monitor conf file for a pod
	GenerateMonitorConf() error

	// CopyFiles copies the files from the config map to the shared dynamic conf
	// volume
	CopyFiles() error

	// GetVariableSubstitutions gets the current keys and values that this
	// instance will substitute into its monitor conf.
	GetVariableSubstitutions() (map[string]string, error)
}

// realPodClient provides a client for use in real environments.
type realFdbPodClient struct {
	// Cluster is the cluster we are connecting to.
	Cluster *fdbtypes.FoundationDBCluster

	// Pod is the pod we are connecting to.
	Pod *corev1.Pod

	// useTLS indicates whether this is using a TLS connection to the sidecar.
	useTLS bool

	// tlsConfig contains the TLS configuration for the connection to the
	// sidecar.
	tlsConfig *tls.Config
}

// NewFdbPodClient builds a client for working with an FDB Pod
func NewFdbPodClient(cluster *fdbtypes.FoundationDBCluster, pod *corev1.Pod) (FdbPodClient, error) {
	if pod.Status.PodIP == "" {
		return nil, fdbPodClientErrorNoIP
	}
	for _, container := range pod.Status.ContainerStatuses {
		if container.Name == "foundationdb-kubernetes-sidecar" && !container.Ready {
			return nil, fdbPodClientErrorNotReady
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
		caList, err := ioutil.ReadFile(caFile)
		if err != nil {
			return nil, err
		}
		certPool.AppendCertsFromPEM(caList)
		tlsConfig.RootCAs = certPool
	}

	return &realFdbPodClient{Cluster: cluster, Pod: pod, useTLS: useTLS, tlsConfig: tlsConfig}, nil
}

// GetCluster returns the cluster associated with a client
func (client *realFdbPodClient) GetCluster() *fdbtypes.FoundationDBCluster {
	return client.Cluster
}

// GetPod returns the pod associated with a client
func (client *realFdbPodClient) GetPod() *corev1.Pod {
	return client.Pod
}

// getListenIP gets the IP address that a pod listens on.
func (client *realFdbPodClient) getListenIP() string {
	return client.Pod.Status.PodIP
}

// makeRequest submits a request to the sidecar.
func (client *realFdbPodClient) makeRequest(method string, path string) (string, error) {
	var protocol string
	if client.useTLS {
		protocol = "https"
	} else {
		protocol = "http"
	}

	url := fmt.Sprintf("%s://%s:8080/%s", protocol, client.getListenIP(), path)
	var resp *http.Response
	var err error

	httpClient := &http.Client{}
	if client.useTLS {
		httpClient.Transport = &http.Transport{TLSClientConfig: client.tlsConfig}
	}

	switch method {
	case "GET":
		resp, err = httpClient.Get(url)
	case "POST":
		resp, err = httpClient.Post(url, "application/json", strings.NewReader(""))
	default:
		return "", fmt.Errorf("Unknown HTTP method %s", method)
	}
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	bodyText := string(body)

	if err != nil {
		return "", err
	}

	if resp.StatusCode >= 400 {
		return "", failedResponse{response: resp, body: bodyText}
	}
	return bodyText, nil
}

// IsPresent checks whether a file in the sidecar is present.
func (client *realFdbPodClient) IsPresent(filename string) (bool, error) {
	_, err := client.makeRequest("GET", fmt.Sprintf("check_hash/%s", filename))
	if err == nil {
		return true, err
	}

	response, isResponse := err.(failedResponse)
	if isResponse && response.response.StatusCode == 404 {
		return false, nil
	}
	return false, err
}

// CheckHash checks whether a file in the sidecar has the expected contents.
func (client *realFdbPodClient) CheckHash(filename string, contents string) (bool, error) {
	response, err := client.makeRequest("GET", fmt.Sprintf("check_hash/%s", filename))
	if err != nil {
		return false, err
	}

	expectedHash := sha256.Sum256([]byte(contents))
	expectedHashString := hex.EncodeToString(expectedHash[:])
	return strings.Compare(expectedHashString, response) == 0, nil
}

// GenerateMonitorConf updates the monitor conf file for a pod
func (client *realFdbPodClient) GenerateMonitorConf() error {
	_, err := client.makeRequest("POST", "copy_monitor_conf")
	return err
}

// CopyFiles copies the files from the config map to the shared dynamic conf
// volume
func (client *realFdbPodClient) CopyFiles() error {
	_, err := client.makeRequest("POST", "copy_files")
	return err
}

// GetVariableSubstitutions gets the current keys and values that this
// instance will substitute into its monitor conf.
func (client *realFdbPodClient) GetVariableSubstitutions() (map[string]string, error) {
	contents, err := client.makeRequest("GET", "substitutions")
	if err != nil {
		return nil, err
	}
	substitutions := map[string]string{}
	err = json.Unmarshal([]byte(contents), &substitutions)
	return substitutions, err
}

// MockFdbPodClient provides a mock connection to a pod
type mockFdbPodClient struct {
	Cluster *fdbtypes.FoundationDBCluster
	Pod     *corev1.Pod
}

// NewMockFdbPodClient builds a mock client for working with an FDB pod
func NewMockFdbPodClient(cluster *fdbtypes.FoundationDBCluster, pod *corev1.Pod) (FdbPodClient, error) {
	return &mockFdbPodClient{Cluster: cluster, Pod: pod}, nil
}

var mockMissingPodIPs map[string]bool
var mockPodClientMutex sync.Mutex

// MockPodIP generates a mock IP for FDB pod
func MockPodIP(pod *corev1.Pod) string {
	mockPodClientMutex.Lock()
	defer mockPodClientMutex.Unlock()

	if mockMissingPodIPs != nil && mockMissingPodIPs[pod.ObjectMeta.Name] {
		return ""
	}
	components := strings.Split(GetInstanceIDFromMeta(pod.ObjectMeta), "-")
	for index, class := range fdbtypes.ProcessClasses {
		if string(class) == components[len(components)-2] {
			return fmt.Sprintf("1.1.%d.%s", index, components[len(components)-1])
		}
	}
	return "0.0.0.0"
}

// setMissingPodIPs sets the pod IPs that should be missing when fetching mock
// pod IPs.
func setMissingPodIPs(ips map[string]bool) {
	mockPodClientMutex.Lock()
	defer mockPodClientMutex.Unlock()
	mockMissingPodIPs = ips
}

// GetCluster returns the cluster associated with a client
func (client *mockFdbPodClient) GetCluster() *fdbtypes.FoundationDBCluster {
	return client.Cluster
}

// GetPod returns the pod associated with a client
func (client *mockFdbPodClient) GetPod() *corev1.Pod {
	return client.Pod
}

// IsPresent checks whether a file in the sidecar is prsent.
func (client *mockFdbPodClient) IsPresent(filename string) (bool, error) {
	return true, nil
}

// CheckHash checks whether a file in the sidecar has the expected contents.
func (client *mockFdbPodClient) CheckHash(filename string, contents string) (bool, error) {
	return true, nil
}

// GenerateMonitorConf updates the monitor conf file for a pod
func (client *mockFdbPodClient) GenerateMonitorConf() error {
	return nil
}

// CopyFiles copies the files from the config map to the shared dynamic conf
// volume
func (client *mockFdbPodClient) CopyFiles() error {
	return nil
}

// UpdateDynamicFiles checks if the files in the dynamic conf volume match the
// expected contents, and tries to copy the latest files from the input volume
// if they do not.
func UpdateDynamicFiles(client FdbPodClient, filename string, contents string, updateFunc func(client FdbPodClient) error) (bool, error) {
	match := false
	var err error

	match, err = client.CheckHash(filename, contents)
	if err != nil {
		return false, err
	}

	if !match {
		log.Info("Waiting for config update", "namespace", client.GetPod().Namespace, "pod", client.GetPod().Name, "file", filename)
		err = updateFunc(client)
		if err != nil {
			return false, err
		}

		return client.CheckHash(filename, contents)
	}

	return true, nil
}

// CheckDynamicFilePresent waits for a file to be present in the dynamic conf
func CheckDynamicFilePresent(client FdbPodClient, filename string) (bool, error) {
	present, err := client.IsPresent(filename)

	if !present {
		log.Info("Waiting for file", "namespace", client.GetPod().Namespace, "pod", client.GetPod().Name, "file", filename)
	}

	return present, err
}

// GetVariableSubstitutions gets the current keys and values that this
// instance will substitute into its monitor conf.
func (client *mockFdbPodClient) GetVariableSubstitutions() (map[string]string, error) {
	substitutions := map[string]string{}
	substitutions["FDB_PUBLIC_IP"] = MockPodIP(client.Pod)
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
			return nil, fmt.Errorf("Unsupported fault domain source %s", faultDomainSource)
		}
	}

	substitutions["FDB_INSTANCE_ID"] = GetInstanceIDFromMeta(client.Pod.ObjectMeta)

	version, err := fdbtypes.ParseFdbVersion(client.Cluster.Spec.Version)
	if err != nil {
		return nil, err
	}

	if version.SupportsUsingBinariesFromMainContainer() {
		if client.Cluster.IsBeingUpgraded() {
			substitutions["BINARY_DIR"] = fmt.Sprintf("/var/dynamic-conf/bin/%s", client.Cluster.Spec.Version)
		} else {
			substitutions["BINARY_DIR"] = "/usr/bin"
		}
	}

	return substitutions, nil
}

// fdbPodClient provides errors that are returned when talking to FDB pods.
type fdbPodClientError int

const (
	// fdbPodClientErrorNoIP is returned when the pod has not been assigned an
	// IP address.
	fdbPodClientErrorNoIP fdbPodClientError = iota

	// fdbPodClientErrorNotReady is returned when the pod is not ready to
	// receive requests.
	fdbPodClientErrorNotReady fdbPodClientError = iota
)

// Error generates an error message.
func (err fdbPodClientError) Error() string {
	switch err {
	case fdbPodClientErrorNoIP:
		return "Pod does not have an IP address"
	default:
		return fmt.Sprintf("Unknown error code %d", err)
	}
}

// failedResponse is an error thrown when a request to the sidecar fails.
type failedResponse struct {
	response *http.Response
	body     string
}

// Error generates an error message.
func (response failedResponse) Error() string {
	return fmt.Sprintf("HTTP request failed. Status=%d; response=%s", response.response.StatusCode, response.body)
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
