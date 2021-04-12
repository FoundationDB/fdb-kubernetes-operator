/*
 * restart_test.go
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

package cmd

import (
	"bytes"
	"testing"

	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func TestRestartCmdFlags(t *testing.T) {
	// We use these buffers to check the input/output
	outBuffer := bytes.Buffer{}
	errBuffer := bytes.Buffer{}
	inBuffer := bytes.Buffer{}

	rootCmd := NewRootCmd(genericclioptions.IOStreams{In: &inBuffer, Out: &outBuffer, ErrOut: &errBuffer})

	args := []string{"restart", "-c", "sample"}
	rootCmd.SetArgs(args)

	err := rootCmd.Execute()
	if err != nil {
		t.Error(err)
	}
}
