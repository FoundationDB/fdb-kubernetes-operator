package foundationdbcluster

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

// FdbPodClient provides methods for working with the a FoundationDB pod
type FdbPodClient interface {
	// GetCluster returns the cluster associated with a client
	GetCluster() *fdbtypes.FoundationDBCluster

	// GetPod returns the pod associated with a client
	GetPod() *corev1.Pod

	// GetPodIP gets the IP address for a pod.
	GetPodIP() string

	// CheckHash checks whether a file in the sidecar has the expected contents.
	CheckHash(filename string, contents string, result chan bool, err chan error)

	// GenerateMonitorConf updates the monitor conf file for a pod
	GenerateMonitorConf(err chan error)

	// CopyFiles copies the files from the config map to the shared dynamic conf
	// volume
	CopyFiles(err chan error)
}

type realFdbPodClient struct {
	Cluster *fdbtypes.FoundationDBCluster
	Pod     *corev1.Pod
}

// NewFdbPodClient builds a client for working with an FDB Pod
func NewFdbPodClient(cluster *fdbtypes.FoundationDBCluster, pod *corev1.Pod) (FdbPodClient, error) {
	if pod.Status.PodIP == "" {
		return nil, fdbPodClientErrorNoIP
	}
	return &realFdbPodClient{Cluster: cluster, Pod: pod}, nil
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
	url := fmt.Sprintf("http://%s:8080/%s", client.GetPodIP(), path)
	var resp *http.Response
	var err error
	switch method {
	case "GET":
		resp, err = http.Get(url)
	case "POST":
		resp, err = http.Post(url, "application/json", strings.NewReader(""))
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
		return "", fmt.Errorf("HTTP request failed. Status=%d; response=%s", resp.StatusCode, bodyText)
	}
	return bodyText, nil
}

// CheckHash checks whether a file in the sidecar has the expected contents.
func (client *realFdbPodClient) CheckHash(filename string, contents string, resultChan chan bool, errorChan chan error) {
	response, err := client.makeRequest("GET", fmt.Sprintf("/check_hash/%s", filename))
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
	_, err := client.makeRequest("POST", "/copy_monitor_conf")
	errorChan <- err
}

// CopyFiles copies the files from the config map to the shared dynamic conf
// volume
func (client *realFdbPodClient) CopyFiles(errorChan chan error) {
	_, err := client.makeRequest("POST", "/copy_files")
	errorChan <- err
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

type fdbPodClientError int

const (
	fdbPodClientErrorNoIP fdbPodClientError = iota
)

func (err fdbPodClientError) Error() string {
	switch err {
	case fdbPodClientErrorNoIP:
		return "Pod does not have an IP address"
	default:
		return fmt.Sprintf("Unknown error code %d", err)
	}
}
