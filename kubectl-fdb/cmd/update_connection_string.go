/*
 * update_connection_string.go
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
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newUpdateConnectionStringCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "connection-string",
		Short: "Updates the connection string in the FoundationDBCluster resource status with the provided connection string",
		Long:  "Updates the connection string in the FoundationDBCluster resource status with the provided connection string",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName, err := cmd.Flags().GetString("fdb-cluster")
			if err != nil {
				return err
			}
			namespace, err := getNamespace(*o.configFlags.Namespace)
			if err != nil {
				return err
			}

			kubeClient, err := getKubeClient(cmd.Context(), o)
			if err != nil {
				return err
			}

			cluster := &fdbv1beta2.FoundationDBCluster{}
			err = kubeClient.Get(cmd.Context(), client.ObjectKey{Name: clusterName, Namespace: namespace}, cluster)
			if err != nil {
				return err
			}

			return updateConnectionStringCmd(cmd, kubeClient, cluster, args[0])
		},
		Example: `
# Updates the connection string for a cluster in the current namespace
kubectl fdb update connection-string -c cluster test:cluster@192.168.0.1:4500

# Updates the connection string for a cluster in the namespace default
kubectl fdb -n default update connection-string -c cluster test:cluster@192.168.0.1:4500`,
	}

	cmd.Flags().StringP("fdb-cluster", "c", "", "Selects process groups from the provided cluster. "+
		"Required if not passing cluster-label.")
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)
	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

// updateConnectionStringCmd will update the connection string if the current connection string is outdated in the fdbv1beta2.FoundationDBCluster status.
func updateConnectionStringCmd(cmd *cobra.Command, kubeClient client.Client, cluster *fdbv1beta2.FoundationDBCluster, connectionString string) error {
	if cluster.Status.ConnectionString == connectionString {
		cmd.Printf("cluster %s/%s already has connections string \"%s\" set\n", cluster.Namespace, cluster.Name, connectionString)
		return nil
	}

	_, err := fdbv1beta2.ParseConnectionString(connectionString)
	if err != nil {
		return err
	}

	return updateConnectionString(cmd.Context(), kubeClient, cluster, connectionString)
}
