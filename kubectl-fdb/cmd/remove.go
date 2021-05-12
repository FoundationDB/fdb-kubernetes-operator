/*
 * remove.go
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
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/spf13/cobra"
)

func newRemoveCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Subcommand to remove instances from a given cluster",
		Long:  "Subcommand to remove instances from a given cluster",
		RunE: func(c *cobra.Command, args []string) error {
			return c.Help()
		},
		Example: `
# Remove instances for a cluster in the current namespace
kubectl fdb remove instances -c cluster pod-1 -i pod-2

# Remove instances for a cluster in the namespace default
kubectl fdb -n default remove instances -c cluster pod-1 pod-2

# Remove instances for a cluster with the instance ID.
# The instance ID of a Pod can be fetched with "kubectl get po -L fdb-instance-id"
kubectl fdb -n default remove instances --use-instance-id -c cluster storage-1 storage-2
`,
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	cmd.AddCommand(newRemoveInstancesCmd(streams))
	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}
