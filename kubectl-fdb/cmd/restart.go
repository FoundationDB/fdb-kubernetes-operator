/*
 * restart.go
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
	"fmt"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	kubeHelper "github.com/FoundationDB/fdb-kubernetes-operator/v2/internal/kubernetes"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newRestartCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Restarts process(es) in a given FDB cluster.",
		Long:  "Restarts process(es) in a given FDB cluster.",
		RunE: func(cmd *cobra.Command, args []string) error {
			wait, err := cmd.Root().Flags().GetBool("wait")
			if err != nil {
				return err
			}
			sleep, err := cmd.Root().Flags().GetUint16("sleep")
			if err != nil {
				return err
			}
			allProcesses, err := cmd.Flags().GetBool("all-processes")
			if err != nil {
				return err
			}
			processGroupSelectionOpts, err := getProcessSelectionOptsFromFlags(cmd, o, args)
			if err != nil {
				return err
			}
			// TODO(nic): consider putting "allProcesses" into the process selection functions to avoid having these checks outside for more sensitive commands
			if len(args) == 0 && !allProcesses && len(processGroupSelectionOpts.conditions) == 0 && len(processGroupSelectionOpts.matchLabels) == 0 && processGroupSelectionOpts.processClass == "" {
				return cmd.Help()
			}

			config, err := o.configFlags.ToRESTConfig()
			if err != nil {
				return err
			}

			kubeClient, err := getKubeClient(cmd.Context(), o)
			if err != nil {
				return err
			}

			podsByCluster, err := getPodNamesByCluster(cmd, kubeClient, processGroupSelectionOpts)
			if err != nil {
				return err
			}
			for cluster, podNames := range podsByCluster {
				err := restartProcesses(cmd, config, kubeClient, podNames, processGroupSelectionOpts.namespace, cluster.Name, wait, sleep)
				if err != nil {
					return err
				}
			}

			return nil
		},
		Example: `
# Restart processes for a cluster in the current namespace
kubectl fdb restart -c cluster pod-1 -i pod-2

# Restart processes for a cluster in the namespace default
kubectl fdb -n default restart -c cluster pod-1 pod-2

# Restart processes for a cluster in the namespace default
kubectl fdb -n default restart pod-1-cluster-A pod-2-cluster-B -l your-cluster-label

# Restart all processes for a cluster
kubectl fdb restart -c cluster --all-processes

# Restart all processes for a cluster that have the given condition
kubectl fdb restart -c cluster --process-condition=MissingProcesses

See help for even more process group selection options, such as by processClass, and processGroupID!
`,
	}
	addProcessSelectionFlags(cmd)
	cmd.Flags().Bool("all-processes", false, "restart all processes of this cluster.")
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

func convertConditions(inputConditions []string) ([]fdbv1beta2.ProcessGroupConditionType, error) {
	res := make([]fdbv1beta2.ProcessGroupConditionType, 0, len(inputConditions))

	for _, inputCondition := range inputConditions {
		cond, err := fdbv1beta2.GetProcessGroupConditionType(inputCondition)
		if err != nil {
			return res, err
		}

		res = append(res, cond)
	}

	return res, nil
}

//nolint:interfacer // golint has a false-positive here -> `cmd` can be `github.com/hashicorp/go-retryablehttp.Logger`
func restartProcesses(cmd *cobra.Command, restConfig *rest.Config, kubeClient client.Client, podNames []string, namespace, clusterName string, wait bool, sleep uint16) error {
	if wait {
		if !confirmAction(fmt.Sprintf("Restart %v in cluster %s/%s", podNames, namespace, clusterName)) {
			return fmt.Errorf("user aborted the removal")
		}
	}

	for _, pod := range podNames {
		cmd.Printf("Restart process: %s\n", podNames)
		_, _, err := kubeHelper.ExecuteCommand(cmd.Context(), kubeClient, restConfig, pod, namespace, fdbv1beta2.MainContainerName, "pkill fdbserver || true", false)
		if err != nil {
			return err
		}
		time.Sleep(time.Duration(sleep) * time.Second)
	}

	return nil
}
