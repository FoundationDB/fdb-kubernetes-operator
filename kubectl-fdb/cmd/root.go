/*
 * root.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2020 Apple Inc. and the FoundationDB project authors
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
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spf13/viper"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/spf13/cobra"
)

// FDBOptions provides information required to run different
// actions on FDB
type FDBOptions struct {
	configFlags *genericclioptions.ConfigFlags
	genericclioptions.IOStreams
}

// NewFDBOptions provides an instance of FDBOptions with default values
func NewFDBOptions(streams genericclioptions.IOStreams) *FDBOptions {
	return &FDBOptions{
		configFlags: genericclioptions.NewConfigFlags(true),
		IOStreams:   streams,
	}
}

// NewRootCmd provides a cobra command wrapping FDB actions
func NewRootCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewFDBOptions(streams)

	cmd := &cobra.Command{
		Use:          "kubectl-fdb",
		Short:        "kubectl plugin for the FoundationDB operator.",
		Long:         `kubectl fdb plugin for the interaction with the FoundationDB operator.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	viper.SetDefault("license", "apache 2")
	cmd.PersistentFlags().StringP("operator-name", "o", "fdb-kubernetes-operator-controller-manager", "Name of the Deployment for the operator.")
	cmd.PersistentFlags().BoolP("force", "f", false, "Suppress the confirmation dialog")
	o.configFlags.AddFlags(cmd.Flags())

	cmd.AddCommand(
		newVersionCmd(streams, cmd),
		newRemoveCmd(streams, cmd),
		newExecCmd(streams),
		newCordonCmd(streams, cmd),
	)

	return cmd
}

// confirmAction requests a user to confirm it's action
func confirmAction(action string) bool {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Printf("%s [y/n]: ", action)

		resp, err := reader.ReadString('\n')
		if err != nil {
			log.Fatal(err)
		}

		resp = strings.ToLower(strings.TrimSpace(resp))

		if resp == "y" || resp == "yes" {
			return true
		}

		if resp == "n" || resp == "no" {
			return false
		}
	}
}
