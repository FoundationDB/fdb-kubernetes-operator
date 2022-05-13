/*
 * foundationdb_constants.go
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

const (
	// SidecarContainer is the name of the sidecar container
	SidecarContainer string = "foundationdb-kubernetes-sidecar"
	// MainContainer is the name of main container that runs FoundationDB
	MainContainer string = "foundationdb"
	// InitContainer is the name of the init container
	InitContainer string = "foundationdb-kubernetes-init"
	// SidecarDefaultImage is the name of the default sidecar and init container
	SidecarDefaultImage string = "foundationdb/foundationdb-kubernetes-sidecar"
)
