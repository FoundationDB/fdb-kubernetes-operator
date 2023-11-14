package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/hashicorp/go-retryablehttp"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// KubectlFbReleaseURL is the public GitHub URL we read the latest version from
const KubectlFbReleaseURL = "https://api.github.com/repos/FoundationDB/fdb-kubernetes-operator/releases/latest"

// LocalTempVersionFileName is where we cache plugin version for 24 hours, so we don't call GitHub for every single command(also rate-limit issue on GitHub api calls)
const LocalTempVersionFileName = "latest"

// PluginVersionDetails Contains [partial] plugin version details from GitHub
type PluginVersionDetails struct {
	ID      int64  `json:"id"`
	Version string `json:"tag_name"`
	Name    string `json:"name"`
}

// It reads the latest fdb plugin version from local temp file,
// if not exists, gets it from GitHub and store it locally and then returns it
func getLatestPluginVersion() string {
	fileName := filepath.Join(os.TempDir(), LocalTempVersionFileName)
	_, err := os.Stat(fileName)
	if os.IsNotExist(err) {
		return updateLocalVersion(fileName)
	}
	versionFileIsRecent := isVersionFileCreatedToday(fileName)
	if versionFileIsRecent {
		return readVersionFromLocalFile(fileName)
	}
	_ = os.Remove(fileName)
	return updateLocalVersion(fileName)
}

// readVersionFromLocalFile reads the version value from the local temp file
func readVersionFromLocalFile(fileName string) string {
	file, _ := os.OpenFile(fileName, os.O_RDONLY, 0644)
	defer file.Close()
	content, _ := os.ReadFile(fileName)
	version := string(content)
	return version
}

// Check if the local temp version file is created within the last 24 hours
func isVersionFileCreatedToday(filename string) bool {
	fileInfo, err := os.Stat(filename)
	if err != nil {
		return false
	}
	now := time.Now().UnixNano()
	creationTime := fileInfo.ModTime().UnixNano()
	diff := now - creationTime
	hours := diff / int64(time.Hour)
	return hours <= 24
}

// write version value in local temp file
func writeVersionToLocalTempFile(fileName string, version string) bool {
	file, err := os.OpenFile(fileName, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return false
	}
	defer file.Close()
	_, err = file.WriteString(version)
	return err == nil
}

// read version from HitHub and update local version file
func updateLocalVersion(fileName string) string {
	latestVersion, err := readVersionFromGitHub()
	if err != nil {
		return ""
	}
	writeVersionToLocalTempFile(fileName, latestVersion)
	return latestVersion
}

// read the latest release version number from GitHub, due to GitHub api rate limit we don't do it for every command
func readVersionFromGitHub() (string, error) {
	retryClient := retryablehttp.NewClient()
	retryClient.RetryMax = 2
	retryClient.RetryWaitMax = 1 * time.Second
	retryClient.Logger = nil
	retryClient.CheckRetry = retryablehttp.ErrorPropagatedRetryPolicy

	retryClient.HTTPClient.Timeout = 1 * time.Second
	req, _ := retryablehttp.NewRequest(http.MethodGet, KubectlFbReleaseURL, nil)
	resp, err := retryClient.Do(req)
	if err != nil || resp == nil {
		fmt.Println("Failed to fetch kubectl-fdb version from GitHub")
		return "", err
	}
	if resp == nil {
		return "", nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)

	if err != nil {
		fmt.Println("Error in reading version from GitHub")
		return "", err
	}
	var result = PluginVersionDetails{}
	if !strings.Contains(strings.ToUpper(resp.Status), "OK") {
		fmt.Println("Failed to get parse version info from GitHub response")
		return "", err
	}
	err = json.Unmarshal(body, &result)
	if err != nil {
		fmt.Println("Failed to read version from GitHub response\n", err)
		return "", err
	}
	//removing 'v' from beginning of release version
	return result.Version[1:], nil
}
