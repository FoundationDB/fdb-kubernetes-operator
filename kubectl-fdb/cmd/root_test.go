/*
 * root_test.go
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
	"bufio"
	"bytes"
	"testing"

	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func TestRootCmd(t *testing.T) {
	// We use these buffers to theoretically check the input/output
	outBuffer := bytes.Buffer{}
	errBuffer := bytes.Buffer{}
	inBuffer := bytes.Buffer{}

	cmd := NewRootCmd(genericclioptions.IOStreams{In: bufio.NewReader(&inBuffer), Out: bufio.NewWriter(&outBuffer), ErrOut: bufio.NewWriter(&errBuffer)})
	err := cmd.Execute()
	if err != nil {
		t.Error(err)
	}
}
