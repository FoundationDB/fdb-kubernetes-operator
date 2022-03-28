/*
 * configuration.go
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
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newConfigurationCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "configuration",
		Short: "Get the configuration string based on the database configuration of the cluster spec.",
		Long:  "Get the configuration string based on the database configuration of the cluster spec.",
		RunE: func(cmd *cobra.Command, args []string) error {
			failOver, err := cmd.Flags().GetBool("fail-over")
			if err != nil {
				return err
			}

			kubeClient, err := getKubeClient(o)
			if err != nil {
				return err
			}

			namespace, err := getNamespace(*o.configFlags.Namespace)
			if err != nil {
				return err
			}

			for _, clusterName := range args {
				configuration, err := getConfigurationString(kubeClient, clusterName, namespace, failOver)
				if err != nil {
					return err
				}

				cmd.Println(configuration)
			}

			return nil
		},
		Example: `
This command will give you the configuration string used to configure the cluster.
You can use "fdbcli configure <cmd output>" to configure that cluster or make changes to that configuration once.
If you manually change that configuration keep in mind that the operator will revert it to the desired state.

# Get the configuration string from cluster c1
kubectl fdb get configuration c1

# Get the configuration string from cluster c1 in the namespace default
kubectl fdb -n default get configuration c1

# Get the configuration string from cluster c1 and change the priority for an HA cluster
kubectl fdb get configuration --fail-over c1
`,
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	cmd.Flags().Bool("fail-over", false, "defines if the configuration should be changed to issue a fail over")
	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

// printConfiguration
func getConfigurationString(kubeClient client.Client, clusterName string, namespace string, failOver bool) (string, error) {
	cluster, err := loadCluster(kubeClient, namespace, clusterName)
	if err != nil {
		return "", err
	}

	config := cluster.Spec.DatabaseConfiguration
	if failOver {
		config = config.FailOver()
	}

	return config.GetConfigurationString(cluster.Spec.Version)
}
