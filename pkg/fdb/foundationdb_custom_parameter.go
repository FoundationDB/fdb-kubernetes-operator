/*
 * foundationdb_custom_parameters.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021-2022 Apple Inc. and the FoundationDB project authors
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

package fdb

import (
	"fmt"
	"strings"
)

// FoundationDBCustomParameter defines a single custom knob
// +kubebuilder:validation:MaxLength=100
type FoundationDBCustomParameter string

// FoundationDBCustomParameters defines a slice of custom knobs
// +kubebuilder:validation:MaxItems=100
type FoundationDBCustomParameters []FoundationDBCustomParameter

// GetKnobsForCLI returns the list of knobs that should be provided to the commandline when running
// an command over the admin client.
func (customParameters FoundationDBCustomParameters) GetKnobsForCLI() []string {
	args := make([]string, 0, len(customParameters))

	for _, arg := range customParameters {
		args = append(args, fmt.Sprintf("--%s", arg))
	}

	return args
}

// ValidateCustomParameters ensures that no duplicate values are set and that no
// protected/forbidden parameters are set. Theoretically we could also check if FDB
// supports the given parameter.
func (customParameters FoundationDBCustomParameters) ValidateCustomParameters() error {
	protectedParameters := map[string]None{"datadir": {}}
	parameters := make(map[string]None)
	violations := make([]string, 0)

	for _, parameter := range customParameters {
		parameterName := strings.Split(string(parameter), "=")[0]
		parameterName = strings.TrimSpace(parameterName)

		if _, ok := parameters[parameterName]; !ok {
			parameters[parameterName] = None{}
		} else {
			violations = append(violations, fmt.Sprintf("found duplicated customParameter: %v", parameterName))
		}

		if _, ok := protectedParameters[parameterName]; ok {
			violations = append(violations, fmt.Sprintf("found protected customParameter: %v, please remove this parameter from the customParameters list", parameterName))
		}
	}

	if len(violations) > 0 {
		return fmt.Errorf("found the following customParameters violations:\n%s", strings.Join(violations, "\n"))
	}

	return nil
}
