/*
 * log_file_cleaner.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021 Apple Inc. and the FoundationDB project authors
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

package internal

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
)

func shouldRemoveLogFile(info os.FileInfo, now time.Time, minFileAge time.Duration) (bool, error) {
	if info.IsDir() {
		return false, nil
	}

	if !strings.HasPrefix(info.Name(), "trace") {
		return false, nil
	}

	// If the file is newer than minFileAge we skip it.
	if info.ModTime().Add(minFileAge).After(now) {
		return false, nil
	}

	// Files from the lib will have the format:
	// trace.$IP.1.&timestamp...json (or xml)
	// Example for trace files from the lib (ignored): trace.10.1.14.36.1.1625057172.rmuWOn.0.1.xml
	// Example for trace files from the cli (removed): trace.10.1.14.36.1337.1625057172.rmuWOn.0.1.xml
	isLibFile, err := regexp.Compile(`\.1\.\d{10,}`)
	if err != nil {
		return false, err
	}

	return !isLibFile.MatchString(info.Name()), nil
}

// CleanupOldCliLogs removes old fdbcli log files.
func CleanupOldCliLogs(minFileAge time.Duration) {
	var log = ctrl.Log.WithName("log_file_cleaner")

	logDir := os.Getenv("FDB_NETWORK_OPTION_TRACE_ENABLE")
	if logDir == "" {
		return
	}

	deletedCnt := 0

	log.V(1).Info("Attempt to clean up old CLI log files", "logDir", logDir)
	err := filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		remove, err := shouldRemoveLogFile(info, time.Now(), minFileAge)
		if err != nil {
			return err
		}

		if !remove {
			return nil
		}

		err = os.Remove(path)
		// If the file doesn't exist move on.
		// we could hit this case when multiple controller routines are running.
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}

		deletedCnt++
		return nil
	})

	log.V(1).Info("Cleanup old CLI log files", "deleted files", deletedCnt, "error", err)
}
