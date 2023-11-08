/*
 * foundationdb_process_class.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2022 Apple Inc. and the FoundationDB project authors
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

package v1beta2

import (
	"strings"
)

// ProcessClass models the class of a pod
type ProcessClass string

const (
	// ProcessClassStorage model for FDB class storage
	ProcessClassStorage ProcessClass = "storage"
	// ProcessClassLog model for FDB class log
	ProcessClassLog ProcessClass = "log"
	// ProcessClassTransaction model for FDB class transaction
	ProcessClassTransaction ProcessClass = "transaction"
	// ProcessClassStateless model for FDB stateless processes
	ProcessClassStateless ProcessClass = "stateless"
	// ProcessClassGeneral model for FDB general processes
	ProcessClassGeneral ProcessClass = "general"
	// ProcessClassClusterController model for FDB class cluster_controller
	ProcessClassClusterController ProcessClass = "cluster_controller"
	// ProcessClassTest model for FDB class test
	ProcessClassTest ProcessClass = "test"
	// ProcessClassCoordinator model for FDB class coordinator
	ProcessClassCoordinator ProcessClass = "coordinator"
	// ProcessClassProxy model for FDB proxy processes
	ProcessClassProxy ProcessClass = "proxy"
	// ProcessClassCommitProxy model for FDB commit_proxy processes
	ProcessClassCommitProxy ProcessClass = "commit_proxy"
	// ProcessClassGrvProxy model for FDB grv_proxy processes
	ProcessClassGrvProxy ProcessClass = "grv_proxy"
)

// IsStateful determines whether a process class should store data.
func (pClass ProcessClass) IsStateful() bool {
	return pClass == ProcessClassStorage || pClass.IsLogProcess() || pClass == ProcessClassCoordinator
}

// IsTransaction determines whether a process class could be part of the transaction system.
func (pClass ProcessClass) IsTransaction() bool {
	return pClass != ProcessClassStorage && pClass != ProcessClassGeneral
}

// SupportsMultipleLogServers determines whether a process class supports multiple log servers. This includes the log
// class and the transaction class.
func (pClass ProcessClass) SupportsMultipleLogServers() bool {
	return pClass.IsLogProcess()
}

// IsLogProcess returns true if the process class is either log or transaction.
func (pClass ProcessClass) IsLogProcess() bool {
	return pClass == ProcessClassLog || pClass == ProcessClassTransaction
}

// GetServersPerPodEnvName returns the environment variable name for the servers per Pod.
// TODO (johscheuer): Revisit this decision: Shouldn't this be an annotation?
func (pClass ProcessClass) GetServersPerPodEnvName() string {
	return strings.ToUpper(string(pClass)) + "_SERVERS_PER_POD"
}

// GetProcessClassForPodName will return the process class name where the underscore is replaced with a hyphen, as
// underscores are not allowed in Kubernetes resources names.
func (pClass ProcessClass) GetProcessClassForPodName() string {
	return strings.ReplaceAll(string(pClass), "_", "-")
}
