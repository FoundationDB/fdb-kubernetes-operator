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
	ctx "context"
	"log"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/FoundationDB/fdb-kubernetes-operator/controllers"
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
			clusterName, err := cmd.Flags().GetString("fdb-cluster")
			if err != nil {
				return err
			}
			allProcesses, err := cmd.Flags().GetBool("all-processes")
			if err != nil {
				return err
			}
			allProcessesIncorrectCMD, err := cmd.Flags().GetBool("all-processes-with-incorrect-cmd")
			if err != nil {
				return err
			}

			if len(args) == 0 && !allProcesses && !allProcessesIncorrectCMD {
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
			} else if allProcessesIncorrectCMD {
				processes, err = getAllPodsFromCluster(kubeClient, clusterName, namespace)
				if err != nil {
					return err
				}
			} else {
				processes = args
			}

			return restartProcesses(cmd, config, clientSet, processes, namespace)
		},
		Example: `
# Restart processes for a cluster in the current namespace
kubectl fdb restart -c cluster pod-1 -i pod-2

# Restart processes for a cluster in the namespace default
kubectl fdb -n default restart -c cluster pod-1 pod-2

# Restart all processes for a cluster
kubectl fdb restart-c cluster --all-processes

# Restart all processes for a cluster that have the condition IncorrectCommandLine
kubectl fdb restart-c cluster --all-processes-with-incorrect-cmd
`,
	}

	cmd.Flags().StringP("fdb-cluster", "c", "", "restart processes(s) from the provided cluster.")
	cmd.Flags().Bool("all-processes", false, "restart all processes of this cluster.")
	cmd.Flags().Bool("all-processes-with-incorrect-cmd", false, "restart all processes of this cluster with the condition \"IncorrectCommandLine\".")
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

func getAllPodsFromCluster(kubeClient client.Client, clusterName string, namespace string) ([]string, error) {
	var cluster fdbtypes.FoundationDBCluster
	err := kubeClient.Get(ctx.Background(), client.ObjectKey{
		Namespace: namespace,
		Name:      clusterName,
	}, &cluster)
	if err != nil {
		return []string{}, err
	}

	incorrectProcesses := fdbtypes.FilterByCondition(cluster.Status.ProcessGroups, fdbtypes.IncorrectCommandLine)
	processes := make([]string, 0, len(incorrectProcesses))

	pods, err := getPodsForCluster(kubeClient, clusterName, namespace)
	if err != nil {
		return processes, err
	}

	for _, pod := range pods.Items {
		found := false
		for _, incorrectProcess := range incorrectProcesses {
			if pod.Labels[controllers.FDBInstanceIDLabel] == incorrectProcess {
				found = true
				break
			}
		}

		if !found {
			continue
		}

		processes = append(processes, pod.Name)
	}

	return processes, nil
}

func restartProcesses(cmd *cobra.Command, restConfig *rest.Config, kubeClient *kubernetes.Clientset, processes []string, namespace string) error {
	for _, process := range processes {
		cmd.Printf("Restart process: %s\n", process)
		_, _, err := executeCmd(restConfig, kubeClient, process, namespace, "pkill fdbserver")
		if err != nil {
			return err
		}
	}

	return nil
}
