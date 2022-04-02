/*
 * get.go
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
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/spf13/cobra"
)

func newGetCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "get",
		Short: "Subcommand to get resources from a given cluster",
		Long:  "Subcommand to get resources from a given cluster",
		RunE: func(c *cobra.Command, args []string) error {
			return c.Help()
		},
		Example: `
# Get the configuration string from cluster c1
kubectl fdb get configuration c1

# Get the configuration string from cluster c1 in the namespace default
kubectl fdb -n default get configuration c1
`,
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	cmd.AddCommand(newConfigurationCmd(streams))
	cmd.AddCommand(newExclusionStatusCmd(streams))
	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}
