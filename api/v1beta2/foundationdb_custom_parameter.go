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

package v1beta2

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

// GetKnobsForBackupRestoreCLI returns the list of knobs that should be provided to the fdbbackup and fdbrestore
// when running them over the admin client. It filters out the incompatible ones.
func (customParameters FoundationDBCustomParameters) GetKnobsForBackupRestoreCLI() []string {
	args := make([]string, 0, len(customParameters))

	for _, arg := range customParameters {
		parameterName := strings.Split(string(arg), "=")[0]
		parameterName = strings.TrimSpace(parameterName)

		// Ignore all `locality` parameters, those can be used by the backup_agent but not the fabbackup command
		if strings.HasPrefix(parameterName, "locality_") {
			continue
		}

		args = append(args, fmt.Sprintf("--%s", arg))
	}
	return args
}

var (
	protectedParameters = map[string]None{
		"datadir": {},
	}
	// See: https://apple.github.io/foundationdb/configuration.html#general-section
	fdbMonitorGeneralParameters = map[string]None{
		"cluster-file":                 {},
		"delete-envvars":               {},
		"kill-on-configuration-change": {},
		"disable-lifecycle-logging":    {},
		"restart-delay":                {},
		"initial-restart-delay":        {},
		"restart-backoff":              {},
		"restart-delay-reset-interval": {},
		"user":                         {},
		"group":                        {},
	}
)

// ValidateCustomParameters ensures that no duplicate values are set and that no
// protected/forbidden parameters are set. Theoretically we could also check if FDB
// supports the given parameter.
func (customParameters FoundationDBCustomParameters) ValidateCustomParameters() error {
	return customParameters.ValidateCustomParametersWithProtectedParameters(protectedParameters)
}

// ValidateCustomParametersWithProtectedParameters ensures that no duplicate values are set and that no
// protected/forbidden parameters are set. Theoretically we could also check if FDB supports the given parameter.
func (customParameters FoundationDBCustomParameters) ValidateCustomParametersWithProtectedParameters(
	protected map[string]None,
) error {
	parameters := make(map[string]None)
	violations := make([]string, 0)

	for _, parameter := range customParameters {
		parameterName := strings.Split(string(parameter), "=")[0]
		parameterName = strings.TrimSpace(parameterName)

		if _, ok := parameters[parameterName]; !ok {
			parameters[parameterName] = None{}
		} else {
			violations = append(
				violations,
				fmt.Sprintf("found duplicated customParameter: %s", parameterName),
			)
		}

		if _, ok := protected[parameterName]; ok {
			violations = append(
				violations,
				fmt.Sprintf(
					"found protected customParameter: %s, please remove this parameter from the customParameters list",
					parameterName,
				),
			)
		}

		if _, ok := fdbMonitorGeneralParameters[parameterName]; ok {
			violations = append(
				violations,
				fmt.Sprintf(
					"found general or fdbmonitor customParameter: %s, please remove this parameter from the customParameters list as they are not supported",
					parameterName,
				),
			)
		}
	}

	if len(violations) > 0 {
		return fmt.Errorf(
			"found the following customParameters violations:\n%s",
			strings.Join(violations, "\n"),
		)
	}

	return nil
}
