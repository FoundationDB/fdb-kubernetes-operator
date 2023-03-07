/*
 * images.go
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
	"strings"
)

// prependRegistry if a registry is defined the registry will be prepended.
func prependRegistry(registry string, container string) string {
	if registry == "" {
		return container
	}

	return registry + "/" + container
}

// GetBaseImageAndTag returns the base image and if present the tag.
func GetBaseImageAndTag(image string) (string, string) {
	parts := strings.Split(image, ":")

	if len(parts) == 1 {
		return parts[0], ""
	}

	return parts[0], parts[1]
}

// GetDebugImage returns the debugging image if enabled.
func GetDebugImage(debug bool, image string) string {
	if debug && !strings.Contains(image, "-debug") {
		return image + "-debug"
	}

	return image
}
