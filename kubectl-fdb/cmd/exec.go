/*
 * exec.go
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
	"log"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	kubeHelper "github.com/FoundationDB/fdb-kubernetes-operator/v2/internal/kubernetes"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newExecCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "exec",
		Short: "Runs a command on a container in an FDB cluster",
		Long:  "Runs a command on a container in an FDB cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName, err := cmd.Flags().GetString("fdb-cluster")
			if err != nil {
				return err
			}

			kubeClient, err := getKubeClient(cmd.Context(), o)
			if err != nil {
				return err
			}

			namespace, err := getNamespace(*o.configFlags.Namespace)
			if err != nil {
				return err
			}

			cluster, err := loadCluster(kubeClient, namespace, clusterName)
			if err != nil {
				return err
			}

			config, err := o.configFlags.ToRESTConfig()
			if err != nil {
				return err
			}

			return runExec(cmd, kubeClient, cluster, config, args)
		},
		Example: `
 # Open a shell.
 kubectl fdb exec -c cluster

 # Run a status command.
 kubectl fdb exec -c cluster -- fdbcli --exec "status minimal"
 `,
	}

	cmd.Flags().StringP("fdb-cluster", "c", "", "exec into the provided cluster.")
	err := cmd.MarkFlagRequired("fdb-cluster")
	if err != nil {
		log.Fatal(err)
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

func runExec(cmd *cobra.Command, kubeClient client.Client, cluster *fdbv1beta2.FoundationDBCluster, config *rest.Config, commandArgs []string) error {
	pods, err := getRunningPodsForCluster(cmd.Context(), kubeClient, cluster)
	if err != nil {
		return err
	}

	clientPod, err := kubeHelper.PickRandomPod(pods)
	if err != nil {
		return err
	}

	return kubeHelper.ExecuteCommandRaw(cmd.Context(), kubeClient, config, clientPod.Namespace, clientPod.Name, fdbv1beta2.MainContainerName, commandArgs, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.OutOrStderr(), true)
}
