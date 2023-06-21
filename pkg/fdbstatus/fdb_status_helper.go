/*
 * fdb_status_helper.go
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

package fdbstatus

import (
	"fmt"
	"strings"
)

// RemoveWarningsInJSON removes any warning messages that might appear in the status output from the fdbcli and returns
// the JSON output without the warning message.
func RemoveWarningsInJSON(jsonString string) ([]byte, error) {
	idx := strings.Index(jsonString, "{")
	if idx == -1 {
		return nil, fmt.Errorf("the JSON string doesn't contain a starting '{'")
	}

	return []byte(strings.TrimSpace(jsonString[idx:])), nil
}
