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
	"log"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newRestartCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Restarts process(es) in a given FDB cluster.",
		Long:  "Restarts process(es) in a given FDB cluster.",
		RunE: func(cmd *cobra.Command, args []string) error {
			force, err := cmd.Root().Flags().GetBool("force")
			if err != nil {
				return err
			}
			clusterName, err := cmd.Flags().GetString("fdb-cluster")
			if err != nil {
				return err
			}
			allProcesses, err := cmd.Flags().GetBool("all-processes")
			if err != nil {
				return err
			}
			processConditions, err := cmd.Flags().GetStringArray("process-condition")
			if err != nil {
				return err
			}
			conditions, err := convertConditions(processConditions)
			if err != nil {
				return err
			}

			if len(args) == 0 && !allProcesses && len(conditions) == 0 {
				return cmd.Help()
			}

			config, err := o.configFlags.ToRESTConfig()
			if err != nil {
				return err
			}

			scheme := runtime.NewScheme()
			_ = clientgoscheme.AddToScheme(scheme)
			_ = fdbtypes.AddToScheme(scheme)
			clientSet, err := kubernetes.NewForConfig(config)
			if err != nil {
				return err
			}

			kubeClient, err := client.New(config, client.Options{Scheme: scheme})
			if err != nil {
				return err
			}

			namespace, err := getNamespace(*o.configFlags.Namespace)
			if err != nil {
				return err
			}

			var processes []string
			if allProcesses {
				pods, err := getPodsForCluster(kubeClient, clusterName, namespace)
				if err != nil {
					return err
				}

				for _, pod := range pods.Items {
					processes = append(processes, pod.Name)
				}
			} else if len(conditions) > 0 {
				processes, err = getAllPodsFromClusterWithCondition(kubeClient, clusterName, namespace, conditions)
				if err != nil {
					return err
				}
			} else {
				processes = args
			}

			return restartProcesses(cmd, config, clientSet, processes, namespace, clusterName, force)
		},
		Example: `
# Restart processes for a cluster in the current namespace
kubectl fdb restart -c cluster pod-1 -i pod-2

# Restart processes for a cluster in the namespace default
kubectl fdb -n default restart -c cluster pod-1 pod-2

# Restart all processes for a cluster
kubectl fdb restart -c cluster --all-processes

# Restart all processes for a cluster that have the given condition
kubectl fdb restart -c cluster --process-condition=MissingProcesses
`,
	}

	cmd.Flags().StringP("fdb-cluster", "c", "", "restart processes(s) from the provided cluster.")
	cmd.Flags().Bool("all-processes", false, "restart all processes of this cluster.")
	cmd.Flags().StringArray("process-condition", []string{}, "restart all processes with the given process conditions.")
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

func convertConditions(inputConditions []string) ([]fdbtypes.ProcessGroupConditionType, error) {
	res := make([]fdbtypes.ProcessGroupConditionType, 0, len(inputConditions))

	for _, inputCondition := range inputConditions {
		cond, err := fdbtypes.GetProcessGroupConditionType(inputCondition)
		if err != nil {
			return res, err
		}

		res = append(res, cond)
	}

	return res, nil
}

//nolint:interfacer // golint has a false-positive here -> `cmd` can be `github.com/hashicorp/go-retryablehttp.Logger`
func restartProcesses(cmd *cobra.Command, restConfig *rest.Config, kubeClient *kubernetes.Clientset, processes []string, namespace string, clusterName string, force bool) error {
	if !force {
		confirmed := confirmAction(fmt.Sprintf("Restart %v in cluster %s/%s", processes, namespace, clusterName))
		if !confirmed {
			return fmt.Errorf("user aborted the removal")
		}
	}

	for _, process := range processes {
		cmd.Printf("Restart process: %s\n", process)
		_, _, err := executeCmd(restConfig, kubeClient, process, namespace, "pkill fdbserver")
		if err != nil {
			return err
		}
	}

	return nil
}
