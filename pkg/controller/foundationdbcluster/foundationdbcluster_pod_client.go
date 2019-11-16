package foundationdbcluster

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

// FdbPodClient provides methods for working with a FoundationDB pod
type FdbPodClient interface {
	// GetCluster returns the cluster associated with a client
	GetCluster() *fdbtypes.FoundationDBCluster

	// GetPod returns the pod associated with a client
	GetPod() *corev1.Pod

	// GetPodIP gets the IP address for a pod.
	GetPodIP() string

	// IsPresent checks whether a file in the sidecar is present
	IsPresent(filename string, result chan bool, err chan error)

	// CheckHash checks whether a file in the sidecar has the expected contents.
	CheckHash(filename string, contents string, result chan bool, err chan error)

	// GenerateMonitorConf updates the monitor conf file for a pod
	GenerateMonitorConf(err chan error)

	// CopyFiles copies the files from the config map to the shared dynamic conf
	// volume
	CopyFiles(err chan error)

	// GetVariableSubstitutions gets the current keys and values that this
	// instance will substitute into its monitor conf.
	GetVariableSubstitutions() (map[string]string, error)
}

type realFdbPodClient struct {
	Cluster   *fdbtypes.FoundationDBCluster
	Pod       *corev1.Pod
	useTls    bool
	tlsConfig *tls.Config
}

// NewFdbPodClient builds a client for working with an FDB Pod
func NewFdbPodClient(cluster *fdbtypes.FoundationDBCluster, pod *corev1.Pod) (FdbPodClient, error) {
	if pod.Status.PodIP == "" {
		return nil, fdbPodClientErrorNoIP
	}
	for _, container := range pod.Status.ContainerStatuses {
		if !container.Ready {
			return nil, fdbPodClientErrorNotReady
		}
	}
	useTls := cluster.Spec.SidecarContainer.EnableTLS

	var tlsConfig = &tls.Config{}
	if useTls {
		cert, err := tls.LoadX509KeyPair(
			os.Getenv("FDB_TLS_CERTIFICATE_FILE"),
			os.Getenv("FDB_TLS_KEY_FILE"),
		)
		if err != nil {
			return nil, err
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
		if os.Getenv("DISABLE_SIDECAR_TLS_CHECK") == "1" {
			tlsConfig.InsecureSkipVerify = true
		}
		certPool := x509.NewCertPool()
		caList, err := ioutil.ReadFile(os.Getenv("FDB_TLS_CA_FILE"))
		if err != nil {
			return nil, err
		}
		certPool.AppendCertsFromPEM(caList)
		tlsConfig.RootCAs = certPool
	}

	return &realFdbPodClient{Cluster: cluster, Pod: pod, useTls: useTls, tlsConfig: tlsConfig}, nil
}

// GetCluster returns the cluster associated with a client
func (client *realFdbPodClient) GetCluster() *fdbtypes.FoundationDBCluster {
	return client.Cluster
}

// GetPod returns the pod associated with a client
func (client *realFdbPodClient) GetPod() *corev1.Pod {
	return client.Pod
}

// GetPodIP gets the IP address for a pod.
func (client *realFdbPodClient) GetPodIP() string {
	return client.Pod.Status.PodIP
}

func (client *realFdbPodClient) makeRequest(method string, path string) (string, error) {

	var protocol string
	if client.useTls {
		protocol = "https"
	} else {
		protocol = "http"
	}

	url := fmt.Sprintf("%s://%s:8080/%s", protocol, client.GetPodIP(), path)
	var resp *http.Response
	var err error

	httpClient := &http.Client{}
	if client.useTls {
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
func (client *realFdbPodClient) IsPresent(filename string, resultChan chan bool, errorChan chan error) {
	_, err := client.makeRequest("GET", fmt.Sprintf("check_hash/%s", filename))
	if err == nil {
		resultChan <- true
		return
	}

	response, isResponse := err.(failedResponse)
	if isResponse && response.response.StatusCode == 404 {
		resultChan <- false
	} else {
		errorChan <- err
	}
}

// CheckHash checks whether a file in the sidecar has the expected contents.
func (client *realFdbPodClient) CheckHash(filename string, contents string, resultChan chan bool, errorChan chan error) {
	response, err := client.makeRequest("GET", fmt.Sprintf("check_hash/%s", filename))
	if err != nil {
		errorChan <- err
		return
	}

	expectedHash := sha256.Sum256([]byte(contents))
	expectedHashString := hex.EncodeToString(expectedHash[:])
	resultChan <- strings.Compare(expectedHashString, response) == 0
}

// GenerateMonitorConf updates the monitor conf file for a pod
func (client *realFdbPodClient) GenerateMonitorConf(errorChan chan error) {
	_, err := client.makeRequest("POST", "copy_monitor_conf")
	errorChan <- err
}

// CopyFiles copies the files from the config map to the shared dynamic conf
// volume
func (client *realFdbPodClient) CopyFiles(errorChan chan error) {
	_, err := client.makeRequest("POST", "copy_files")
	errorChan <- err
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

// GetCluster returns the cluster associated with a client
func (client *mockFdbPodClient) GetCluster() *fdbtypes.FoundationDBCluster {
	return client.Cluster
}

// GetPod returns the pod associated with a client
func (client *mockFdbPodClient) GetPod() *corev1.Pod {
	return client.Pod
}

// GetPodIP gets the IP address for a pod.
func (client *mockFdbPodClient) GetPodIP() string {
	return mockPodIP(client.Pod)
}

// IsPresent checks whether a file in the sidecar is prsent.
func (client *mockFdbPodClient) IsPresent(filename string, result chan bool, err chan error) {
	result <- true
}

// CheckHash checks whether a file in the sidecar has the expected contents.
func (client *mockFdbPodClient) CheckHash(filename string, contents string, result chan bool, err chan error) {
	result <- true
}

// GenerateMonitorConf updates the monitor conf file for a pod
func (client *mockFdbPodClient) GenerateMonitorConf(err chan error) {
	err <- nil
}

// CopyFiles copies the files from the config map to the shared dynamic conf
// volume
func (client *mockFdbPodClient) CopyFiles(err chan error) {
	err <- nil
}

func mockPodIP(pod *corev1.Pod) string {
	return fmt.Sprintf("1.1.1.%s", pod.Labels["fdb-instance-id"])
}

// UpdateDynamicFiles updates the files in the dynamic conf volume until they
// match the expected contents.
func UpdateDynamicFiles(client FdbPodClient, filename string, contents string, signal chan error, updateFunc func(client FdbPodClient, err chan error)) {
	clientError := make(chan error)
	hashMatch := make(chan bool)

	match := false
	firstCheck := true
	var err error

	for !match {
		go client.CheckHash(filename, contents, hashMatch, clientError)
		select {
		case match = <-hashMatch:
			if !match {
				log.Info("Waiting for config update", "namespace", client.GetPod().Namespace, "pod", client.GetPod().Name, "file", filename)
				if firstCheck {
					firstCheck = false
				} else {
					time.Sleep(time.Second * 10)
				}
				go updateFunc(client, clientError)
				err = <-clientError
				if err != nil {
					signal <- err
					return
				}
			}
			break
		case err = <-clientError:
			signal <- err
			return
		}
	}

	signal <- nil
}

// CheckDynamicFilePresent waits for a file to be present in the dynamic conf
func CheckDynamicFilePresent(client FdbPodClient, filename string, signal chan error) {
	clientError := make(chan error)
	presentChan := make(chan bool)

	present := false
	var err error

	for !present {
		go client.IsPresent(filename, presentChan, clientError)
		select {
		case present = <-presentChan:
			if !present {
				log.Info("Waiting for file", "namespace", client.GetPod().Namespace, "pod", client.GetPod().Name, "file", filename)
				time.Sleep(time.Second * 10)
			}
			break
		case err = <-clientError:
			signal <- err
			return
		}
	}

	signal <- nil
}

// GetVariableSubstitutions gets the current keys and values that this
// instance will substitute into its monitor conf.
func (client *mockFdbPodClient) GetVariableSubstitutions() (map[string]string, error) {
	substitutions := map[string]string{}
	substitutions["FDB_PUBLIC_IP"] = client.Pod.Status.PodIP
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
	return substitutions, nil
}

type fdbPodClientError int

const (
	fdbPodClientErrorNoIP     fdbPodClientError = iota
	fdbPodClientErrorNotReady fdbPodClientError = iota
)

func (err fdbPodClientError) Error() string {
	switch err {
	case fdbPodClientErrorNoIP:
		return "Pod does not have an IP address"
	default:
		return fmt.Sprintf("Unknown error code %d", err)
	}
}

type failedResponse struct {
	response *http.Response
	body     string
}

func (response failedResponse) Error() string {
	return fmt.Sprintf("HTTP request failed. Status=%d; response=%s", response.response.StatusCode, response.body)
}
