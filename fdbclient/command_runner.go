/*
 * command_runner.go
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

package fdbclient

import (
	"context"
	"github.com/go-logr/logr"
	"os/exec"
)

// commandRunner is an interface to run commands.
type commandRunner interface {
	// runCommand is a method to execute a binary with the given arguments.
	runCommand(ctx context.Context, name string, arg ...string) ([]byte, error)
}

// realCommandRunner is a struct that implements the commandRunner interface and executes commands with the exec package.
type realCommandRunner struct {
	log logr.Logger
}

func (runner *realCommandRunner) runCommand(ctx context.Context, name string, arg ...string) ([]byte, error) {
	execCommand := exec.CommandContext(ctx, name, arg...)

	runner.log.Info("Running command", "path", execCommand.Path, "args", execCommand.Args)
	return execCommand.CombinedOutput()
}

// mockCommandRunner is a mock implementation of commandRunner and can be used for unit testing.
type mockCommandRunner struct {
	// mockedOutput is the output returned by runCommand
	mockedOutput string
	// mockedError is the error returned by runCommand
	mockedError error
	// receivedBinary will be the binary that was used to call runCommand
	receivedBinary string
	// receivedArgs will be the args that were used to call runCommand
	receivedArgs []string
}

func (runner *mockCommandRunner) runCommand(_ context.Context, name string, arg ...string) ([]byte, error) {
	runner.receivedBinary = name
	runner.receivedArgs = arg

	return []byte(runner.mockedOutput), runner.mockedError
}
