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
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
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

	// TODO remove the gabs dependency in this part and make use of the struct
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

		return err
	}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).ShouldNot(gomega.HaveOccurred())

	return stdout, stderr
}

// RunFdbCliCommandInOperatorWithoutRetry allows to run a command with fdbcli in the operator Pod without doing any retries.
func (fdbCluster *FdbCluster) RunFdbCliCommandInOperatorWithoutRetry(
	command string,
	printOutput bool,
	timeout int,
) (string, string, error) {
	pod := ChooseRandomPod(fdbCluster.factory.GetOperatorPods(fdbCluster.Namespace()))
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
			pod.Namespace,
			pod.Name,
			"manager",
			fmt.Sprintf(
				"export TIMEFORMAT='%%R' && echo '%s' > %s && time %s --log-dir \"/var/log/fdb\" --log --trace_format \"json\" %s -C %s --exec '%s'",
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

// InvariantClusterStatusAvailableWithThreshold checks if the database is at a maximum unavailable for the provided threshold.
func (fdbCluster FdbCluster) InvariantClusterStatusAvailableWithThreshold(
	availabilityThreshold time.Duration,
) error {
	return fdbCluster.StatusInvariantChecker(
		"InvariantClusterStatusAvailableWithThreshold",
		availabilityThreshold,
		func(status *fdbv1beta2.FoundationDBStatus) error {
			if !status.Client.DatabaseStatus.Available {
				return fmt.Errorf("cluster is not available")
			}

			return nil
		},
	)
}

// InvariantClusterStatusAvailable checks if the cluster is available the whole test.
func (fdbCluster FdbCluster) InvariantClusterStatusAvailable() error {
	return fdbCluster.StatusInvariantChecker(
		"InvariantClusterStatusAvailable",
		0,
		func(status *fdbv1beta2.FoundationDBStatus) error {
			if !status.Client.DatabaseStatus.Available {
				return fmt.Errorf("cluster.database_available=false")
			}

			return nil
		},
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
		for _, r := range roles {
			if r.Role == string(role) {
				matches = append(matches, p)
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
