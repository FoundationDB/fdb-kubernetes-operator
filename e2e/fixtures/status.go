/*
 * status.go
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
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// getStatusFromOperatorPod returns fdb status queried through the operator Pod.
func (fdbCluster *FdbCluster) getStatusFromOperatorPod() *fdbv1beta2.FoundationDBStatus {
	status := &fdbv1beta2.FoundationDBStatus{}

	if fdbCluster.factory.shutdownInProgress {
		return status
	}

	gomega.Eventually(func() error {
		out, _, err := fdbCluster.RunFdbCliCommandInOperatorWithoutRetry("status json", false, 30)
		if err != nil {
			return err
		}

		status, err = parseStatusOutput(out)
		return err
	}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).ShouldNot(gomega.HaveOccurred())

	return status
}

// RunFdbCliCommandInOperator allows to run a command with fdbcli in the operator Pod.
func (fdbCluster *FdbCluster) RunFdbCliCommandInOperator(
	command string,
	printOutput bool,
	timeout int,
) (string, string) {
	var stdout, stderr string
	var err error

	gomega.Eventually(func() error {
		// Ensure we fetch everything if we have to retry it e.g. because the connection string has changed or
		// because the operator Pod was killed.
		stdout, stderr, err = fdbCluster.RunFdbCliCommandInOperatorWithoutRetry(
			command,
			printOutput,
			timeout,
		)

		if err != nil {
			return fmt.Errorf("could not run the command: \"%s\": got an error: %w, stderr: %s", command, err, stderr)
		}

		return nil
	}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).ShouldNot(gomega.HaveOccurred())

	return stdout, stderr
}

// RunFdbCliCommandInOperatorWithoutRetry allows to run a command with fdbcli in the operator Pod without doing any retries.
func (fdbCluster *FdbCluster) RunFdbCliCommandInOperatorWithoutRetry(
	command string,
	printOutput bool,
	timeout int,
) (string, string, error) {
	pod := fdbCluster.factory.ChooseRandomPod(fdbCluster.factory.GetOperatorPods(fdbCluster.Namespace()))
	cluster, err := fdbCluster.factory.getClusterStatus(
		fdbCluster.Name(),
		fdbCluster.Namespace(),
	)

	if err != nil {
		return "", "", err
	}

	runningVersion, err := fdbv1beta2.ParseFdbVersion(cluster.GetRunningVersion())
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	fdbCliPaths := []string{
		fmt.Sprintf("/usr/bin/fdb/%s/fdbcli", runningVersion.Compact()),
	}

	if cluster.IsBeingUpgraded() {
		desiredVersion, err := fdbv1beta2.ParseFdbVersion(cluster.Spec.Version)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		fdbCliPaths = append(
			fdbCliPaths,
			fmt.Sprintf("/usr/bin/fdb/%s/fdbcli", desiredVersion.Compact()),
		)
	}

	clusterFile := fmt.Sprintf("/tmp/%s", cluster.Name)

	var timeoutArgs string
	if timeout > 0 {
		timeoutArgs = fmt.Sprintf("--timeout %d", timeout)
	}

	var stdout, stderr string

	// If a cluster is currently updated the update of the FoundationDBCluster status is done aysnc. That means there is
	// a timespan where the cluster is already upgraded but the status.RunningVersion is still pointing to the old version.
	// In order to catch those cases we try first the fdbcli for the running version and if that doesn't work (an error
	// is returned) we will try the version for the spec. This should reduce some test flakiness.
	for _, fdbCliPath := range fdbCliPaths {
		stdout, stderr, err = fdbCluster.factory.ExecuteCmd(
			context.Background(),
			pod.Namespace,
			pod.Name,
			"manager",
			fmt.Sprintf(
				"unset %s && unset %s && unset %s && TIMEFORMAT='%%R' && echo '%s' > %s && time %s --log-dir \"/var/log/fdb\" --log --trace_format \"json\" %s -C %s --exec '%s'",
				fdbv1beta2.EnvNameClientThreadsPerVersion,
				fdbv1beta2.EnvNameFDBExternalClientDir,
				fdbv1beta2.EnvNameFDBIgnoreExternalClientFailures,
				cluster.Status.ConnectionString,
				clusterFile,
				fdbCliPath,
				timeoutArgs,
				clusterFile,
				command,
			),
			printOutput,
		)

		if err != nil {
			return stdout, stderr, err
		}

		// Only if we do an upgrade we have to check if we actually use the correct fdbcli version
		if !cluster.IsBeingUpgraded() {
			break
		}

		// Only try to parse the content to json if the command was "status json".
		if strings.Contains(command, "status json") {
			var parsedStatus *fdbv1beta2.FoundationDBStatus
			parsedStatus, err = parseStatusOutput(stdout)
			// If we cannot parse the status we probably have an error or timeout
			if err != nil {
				continue
			}

			// Quorum of coordinators are available, so we probably use the correct version
			if parsedStatus.Client.Coordinators.QuorumReachable {
				break
			}
		}
	}

	return stdout, stderr, err
}

// getStatusFromOperatorPod returns fdb status queried through this Pod.
func parseStatusOutput(rawStatus string) (*fdbv1beta2.FoundationDBStatus, error) {
	if strings.HasPrefix(rawStatus, "\r\nWARNING") {
		rawStatus = strings.TrimPrefix(
			rawStatus,
			"\r\nWARNING: Long delay (Ctrl-C to interrupt)\r\n",
		)
	}

	status := &fdbv1beta2.FoundationDBStatus{}
	err := json.Unmarshal([]byte(rawStatus), status)

	if err != nil {
		return nil, fmt.Errorf(
			"could not parse result of status json %w (unparseable JSON: %s)	",
			err,
			rawStatus,
		)
	}

	return status, nil
}

// IsAvailable returns true if the database is available.
func (fdbCluster *FdbCluster) IsAvailable() bool {
	return fdbCluster.GetStatus().Client.DatabaseStatus.Available
}

// WaitUntilAvailable waits until the cluster is available.
func (fdbCluster *FdbCluster) WaitUntilAvailable() error {
	return wait.PollImmediate(100*time.Millisecond, 60*time.Second, func() (bool, error) {
		return fdbCluster.IsAvailable(), nil
	})
}

// StatusInvariantChecker provides a way to check an invariant for the cluster status.
// nolint:nilerr
func (fdbCluster FdbCluster) StatusInvariantChecker(
	name string,
	threshold time.Duration,
	f func(status *fdbv1beta2.FoundationDBStatus) error,
) error {
	first := true
	return CheckInvariant(
		name,
		&fdbCluster.factory.invariantShutdownHooks,
		threshold,
		func() error {
			// Once we are in the shutdown mode ignore all checks
			if fdbCluster.factory.shutdownInProgress {
				return nil
			}

			cluster, err := fdbCluster.factory.getClusterStatus(
				fdbCluster.Name(),
				fdbCluster.Namespace(),
			)

			if err != nil {
				return nil
			}

			if !cluster.DeletionTimestamp.IsZero() {
				return nil
			}

			out, _, err := fdbCluster.RunFdbCliCommandInOperatorWithoutRetry(
				"status json",
				false,
				30,
			)
			if err != nil {
				log.Println("error in StatusInvariantChecker fetching status json:", err.Error())
				return nil
			}

			status, err := parseStatusOutput(out)
			if err != nil {
				return nil
			}

			err = f(status)
			if err != nil && first {
				log.Printf("invariant %s failed for the first time", name)
				first = false
			}

			return err
		},
	)
}

// checkAvailability returns nil if the cluster is reachable. If the cluster is unreachable an error will be returned.
func checkAvailability(status *fdbv1beta2.FoundationDBStatus) error {
	if !status.Client.DatabaseStatus.Available {
		log.Println("client messages", status.Client.Messages, "cluster messages", status.Cluster.Messages)
		return fmt.Errorf("cluster is not available")
	}

	return nil
}

// InvariantClusterStatusAvailableWithThreshold checks if the database is at a maximum unavailable for the provided threshold.
func (fdbCluster FdbCluster) InvariantClusterStatusAvailableWithThreshold(
	availabilityThreshold time.Duration,
) error {
	return fdbCluster.StatusInvariantChecker(
		"InvariantClusterStatusAvailableWithThreshold",
		availabilityThreshold,
		checkAvailability,
	)
}

// InvariantClusterStatusAvailable checks if the cluster is available the whole test.
func (fdbCluster FdbCluster) InvariantClusterStatusAvailable() error {
	return fdbCluster.StatusInvariantChecker(
		"InvariantClusterStatusAvailable",
		// Per default we allow 15 seconds unavailability. Otherwise we could get a few test failures when we do operations
		// like a replacement on a transaction system Pod and the recovery takes longer.
		15*time.Second,
		checkAvailability,
	)
}

// GetProcessCount returns the number of processes having the specified role
func (fdbCluster *FdbCluster) GetProcessCount(targetRole fdbv1beta2.ProcessRole) int {
	pCounter := 0
	status := fdbCluster.GetStatus()

	for _, process := range status.Cluster.Processes {
		for _, role := range process.Roles {
			if role.Role == string(targetRole) {
				pCounter++
			}
		}
	}

	return pCounter
}

// GetProcessCountByProcessClass returns the number of processes based on process class
func (fdbCluster *FdbCluster) GetProcessCountByProcessClass(pClass fdbv1beta2.ProcessClass) int {
	pCounter := 0
	status := fdbCluster.GetStatus()

	for _, process := range status.Cluster.Processes {
		if process.ProcessClass == pClass {
			pCounter++
		}
	}

	return pCounter
}

// HasTLSEnabled returns true if the cluster is running with TLS enabled.
func (fdbCluster *FdbCluster) HasTLSEnabled() bool {
	status := fdbCluster.GetStatus()

	if len(status.Cluster.Processes) == 0 {
		return false
	}

	tlsCnt := 0
	noTLSCnt := 0
	for _, process := range status.Cluster.Processes {
		if _, ok := process.Address.Flags["tls"]; ok {
			tlsCnt++
			continue
		}
		noTLSCnt++
	}
	// We assume that the cluster either listens on TLS or not but not both.
	// A process would only listen during the transition on both addresses but we wait
	// until the cluster is reconciled.
	if tlsCnt == len(status.Cluster.Processes) {
		return true
	}

	if noTLSCnt == len(status.Cluster.Processes) {
		return false
	}

	ginkgo.Fail(fmt.Sprintf(
		"expected that all processes either have tls enabled or disabled but got %d with TLS and %d without tls",
		tlsCnt,
		noTLSCnt,
	))

	return false
}

// GetCoordinators returns the Pods of the FoundationDBCluster that are having the coordinator role.
func (fdbCluster *FdbCluster) GetCoordinators() []corev1.Pod {
	return fdbCluster.GetPodsWithRole(fdbv1beta2.ProcessRoleCoordinator)
}

// GetStatus returns fdb status queried from a random operator Pod in this clusters namespace.
func (fdbCluster FdbCluster) GetStatus() *fdbv1beta2.FoundationDBStatus {
	return fdbCluster.getStatusFromOperatorPod()
}

// RoleInfo stores information for one particular worker role.
type RoleInfo struct {
	Role string
	ID   string
}

// GetPodRoleMap returns a map with the process group ID as key and all associated roles.
func (fdbCluster *FdbCluster) GetPodRoleMap() map[fdbv1beta2.ProcessGroupID][]RoleInfo {
	ret := make(map[fdbv1beta2.ProcessGroupID][]RoleInfo)
	status := fdbCluster.GetStatus()

	for _, process := range status.Cluster.Processes {
		podName := fdbv1beta2.ProcessGroupID(process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey])

		for _, role := range process.Roles {
			ret[podName] = append(ret[podName], RoleInfo{role.Role, role.ID})
		}
	}

	return ret
}

// GetPodsWithRole returns all Pods that have the provided role.
func (fdbCluster *FdbCluster) GetPodsWithRole(role fdbv1beta2.ProcessRole) []corev1.Pod {
	roleMap := fdbCluster.GetPodRoleMap()
	pods := fdbCluster.GetPods()

	var matches []corev1.Pod
	for _, p := range pods.Items {
		roles := roleMap[GetProcessGroupID(p)]
		rolesAdded := map[string]fdbv1beta2.None{}
		for _, r := range roles {
			if r.Role == string(role) {
				_, ok := rolesAdded[r.Role]
				if !ok {
					rolesAdded[r.Role] = fdbv1beta2.None{}
					matches = append(matches, p)
				}
			}
		}
	}
	return matches
}

// GetCommandlineForProcessesPerClass fetches the commandline args for all processes except of the specified class.
func (fdbCluster FdbCluster) GetCommandlineForProcessesPerClass() map[fdbv1beta2.ProcessClass][]string {
	status := fdbCluster.GetStatus()

	knobs := map[fdbv1beta2.ProcessClass][]string{}
	for _, process := range status.Cluster.Processes {
		if _, ok := knobs[process.ProcessClass]; !ok {
			knobs[process.ProcessClass] = []string{process.CommandLine}
			continue
		}

		knobs[process.ProcessClass] = append(knobs[process.ProcessClass], process.CommandLine)
	}

	return knobs
}

// FdbPrintable copied from foundationdb bindings/go/src/fdb/fdb.go func Printable(d []byte) string
// Printable returns a human readable version of a byte array. The bytes that correspond with
// ASCII printable characters [32-127) are passed through. Other bytes are
// replaced with \x followed by a two character zero-padded hex code for byte.
func FdbPrintable(d []byte) string {
	buf := new(bytes.Buffer)
	for _, b := range d {
		if b >= 32 && b < 127 && b != '\\' {
			buf.WriteByte(b)
			continue
		}
		if b == '\\' {
			buf.WriteString("\\\\")
			continue
		}
		buf.WriteString(fmt.Sprintf("\\x%02x", b))
	}

	return buf.String()
}

// FdbStrinc returns the first key that would sort outside the range prefixed by
// // prefix, or an error if prefix is empty or contains only 0xFF bytes.
// Copied from foundationdb bindings/go/src/fdb/range.go func Strinc(prefix []byte) ([]byte, error)
func FdbStrinc(prefix []byte) ([]byte, error) {
	for i := len(prefix) - 1; i >= 0; i-- {
		if prefix[i] != 0xFF {
			ret := make([]byte, i+1)
			copy(ret, prefix[:i+1])
			ret[i]++
			return ret, nil
		}
	}
	return nil, fmt.Errorf("key must contain at least one byte not equal to 0xFF")
}

// Unprintable adapted from foundationdb fdbclient/NativeAPI.actor.cpp std::string unprintable(std::string const& val).
func Unprintable(val string) ([]byte, error) {
	s := new(bytes.Buffer)
	for i := 0; i < len(val); i++ {
		c := val[i]
		if c == '\\' {
			i++
			if i == len(val) {
				return nil, fmt.Errorf("end after one \\ when unprint [%s]", val)
			}
			switch val[i] {
			case '\\':
				{
					s.WriteByte('\\')
				}
			case 'x':
				{
					if i+2 >= len(val) {
						return nil, fmt.Errorf("not have two chars after \\x when unprint [%s]", val)
					}
					d1, err := unhex(val[i+1])
					if err != nil {
						return nil, err
					}
					d2, err := unhex(val[i+2])
					if err != nil {
						return nil, err
					}
					s.WriteByte(byte((d1 << 4) + d2))
					i += 2
				}
			default:
				{
					return nil, fmt.Errorf("after \\ it's neither \\ nor x when unprint %s", val)
				}
			}
		} else {
			s.WriteByte(c)
		}
	}
	return s.Bytes(), nil
}

// unhex adapted from foundationdb fdbclient/NativeAPI.actor.cpp std::string int unhex(char c).
func unhex(c byte) (int, error) {
	if c >= '0' && c <= '9' {
		return int(c - '0'), nil
	}
	if c >= 'a' && c <= 'f' {
		return int(c - 'a' + 10), nil
	}
	if c >= 'A' && c <= 'F' {
		return int(c - 'A' + 10), nil
	}

	return -1, fmt.Errorf("failed to unhex %x", c)
}
