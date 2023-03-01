/*
 * buggify.go
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

package cmd

import (
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/spf13/cobra"
)

func newBuggifyCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "buggify",
		Short: "Subcommand to add process groups to buggify list for a given cluster",
		Long: "Subcommand to add process groups to buggify list for a given cluster. " +
			"Supported options: crash-loop, no-schedule, empty-monitor-conf.",
		RunE: func(c *cobra.Command, args []string) error {
			return c.Help()
		},
		Example: `
kubectl fdb -n <namespace> buggify <option> -c <cluster> pod-1 pod-2

# Add process groups into crash loop state for a cluster in the current namespace with container name
kubectl fdb buggify crash-loop -c cluster --container-name container-name pod-1 pod-2

# Remove process groups from crash loop state from a cluster in the current namespace with container name
kubectl fdb buggify crash-loop --clear -c cluster --container-name container-name pod-1 pod-2

# Clean crash loop list of a cluster in the current namespace with container name
kubectl fdb buggify crash-loop --clean -c cluster --container-name container-name

# Add process groups into no-schedule state for a cluster in the current namespace
kubectl fdb buggify no-schedule -c cluster pod-1 pod-2

# Remove process groups from no-schedule state from a cluster in the current namespace
kubectl fdb buggify no-schedule --clear -c cluster pod-1 pod-2

# Clean no-schedule list of a cluster in the current namespace
kubectl fdb buggify no-schedule  --clean -c cluster

# Setting empty-monitor-conf to true
kubectl fdb buggify empty-monitor-conf -c cluster

# Setting empty-monitor-conf to false
kubectl fdb buggify empty-monitor-conf --unset -c cluster
`,
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	cmd.AddCommand(
		newBuggifyCrashLoop(streams),
		newBuggifyNoSchedule(streams),
		newBuggifyEmptyMonitorConf(streams),
	)
	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}
