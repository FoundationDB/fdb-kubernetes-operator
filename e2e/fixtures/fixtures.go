/*
 * fixtures.go
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
	"sync"
	"time"

	"github.com/onsi/gomega"
)

// ShutdownHooks allows fixtures to register a handler to be run at exit.
// Handlers run regardless of any preceding errors, and run in reverse order
// of registration.
type ShutdownHooks struct {
	handlers []func() error
}

// Defer execution of func until after the test completes.
func (shutdown *ShutdownHooks) Defer(f func() error) {
	shutdown.handlers = append(shutdown.handlers, f)
}

// ToJSON tries to convert any object to a string representing the struct as JSON.
func ToJSON(v interface{}) string {
	s, err := json.Marshal(v)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	return string(s)
}

// InvokeShutdownHandlers in reverse order of registration.
func (shutdown *ShutdownHooks) InvokeShutdownHandlers() {
	// Store the current handlers in a new variable, we have to do this in order to reset shutdown.handlers before
	// we are doing any checks. If the below Expect expression is false the further execution will be stopped and
	// the handler would never be reset.
	handler := shutdown.handlers
	// clear handlers so we can be reused.
	shutdown.handlers = []func() error{}
	// iterate over the array in reverse order.
	for i := len(handler); i != 0; i-- {
		gomega.Expect(handler[i-1]()).ShouldNot(gomega.HaveOccurred())
	}
}

// CheckInvariant will test the invariant method provided and will return an error if the invariant returns an error
func CheckInvariant(
	invariantName string,
	shutdownHooks *ShutdownHooks,
	threshold time.Duration,
	f func() error,
) error {
	var waitGroup sync.WaitGroup
	ticker := time.NewTicker(250 * time.Millisecond)
	var last error
	testFailed := false
	quit := make(chan struct{})
	waitGroup.Add(1)
	var failureStartTime time.Time
	var currentFailureDuration time.Duration
	var longestFailureDuration time.Duration

	go func() {
		defer waitGroup.Done()
		for {
			select {
			case <-ticker.C:
				err := f()
				if err != nil {
					if last == nil {
						log.Printf("invariant %s failed: %v", invariantName, err)
						failureStartTime = time.Now()
						last = err
					}

					currentFailureDuration = time.Since(failureStartTime)
					if currentFailureDuration >= threshold {
						log.Printf(
							"invariant %s failed after: %v",
							invariantName,
							currentFailureDuration.String(),
						)
						testFailed = true

						// If the current failure duration is longer than the longest failure duration
						// update the longest failure duration. The longest failure duration is used to report
						// the failure duration in cases where the cluster was unavailable longer than the
						// threshold. If we are not setting this value, we could see cases where the cluster was
						// unavailable longer than the threshold but the cluster recovered and therefore the
						// error message is reporting the incorrect failure duration.
						if currentFailureDuration > longestFailureDuration {
							longestFailureDuration = currentFailureDuration
						}
					}
					continue
				}

				if last != nil {
					log.Printf("invariant %s true again", invariantName)
					last = nil
				}
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()
	shutdownHooks.Defer(func() error {
		close(quit)
		waitGroup.Wait()
		if testFailed {
			return fmt.Errorf("invariant %s failed for %s", invariantName, longestFailureDuration.String())
		}
		return nil
	})

	return nil
}
