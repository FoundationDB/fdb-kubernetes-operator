/*
 * update.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2025 Apple Inc. and the FoundationDB project authors
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

func newUpdateCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Subcommand to update the configuration for a given cluster",
		Long:  "Subcommand to update the configuration for a given cluster",
		RunE: func(c *cobra.Command, _ []string) error {
			return c.Help()
		},
		Example: `
# Updates the connection string for a cluster in the current namespace
kubectl fdb update connection-string -c cluster test:cluster@192.168.0.1:4500

# Updates the connection string for a cluster in the namespace default
kubectl fdb -n default update connection-string -c cluster test:cluster@192.168.0.1:4500
`,
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	cmd.AddCommand(newUpdateConnectionStringCmd(streams))
	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}
