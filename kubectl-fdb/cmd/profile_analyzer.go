/*
 * profile_analyzer.go
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
	"context"
	"fmt"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newProfileAnalyzerCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use: "analyze-profile",
		Short: "Analyze FDB shards to find the busiest team",
		Long: "Analyze FDB shards to find the busiest team",
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName, err := cmd.Root().Flags().GetString("cluster")
			if err != nil {
				return err
			}
			flags := cmd.Root().Flags().Args()
			kubeClient, err := getKubeClient(o)
			if err != nil {
				return err
			}
			namespace, err := getNamespace(*o.configFlags.Namespace)
			if err != nil {
				return err
			}

			return runProfileAnalyzer(kubeClient, namespace, clusterName, flags)
		},
		Example: `
# Run the profiler for cluster-1
kubectl fdb analyze-profile cluster-1 --cluster cluster-1 --star-time "01:01 20/07/2022 BST" --end-time "01:30 20/07/2022 BST"
`,
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	return cmd
}

func runProfileAnalyzer(kubeClient client.Client, namespace string, clusterName string, flags []string) error {
	cluster, err := loadCluster(kubeClient, namespace, clusterName)
	if err != nil {
		return err
	}
	cmd := []string{"python" , "./transaction_profiling_analyzer.py"}
	args := []string{"--cluster-file", "/var/fdb/fdb.cluster"}
	args = append(args, flags...)
	needsCreation := false
	deployment := &appsv1.Deployment{}
	if deployment == nil {
		return fmt.Errorf("could not get deployment")
	}
	err = kubeClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: "fdb-profile-analyzer"}, deployment)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			needsCreation = true
		} else {
			return err
		}
	}
	deployment.Spec.Template.ClusterName = clusterName
	deployment.Spec.Template.Spec.Containers[0].Command = cmd
	deployment.Spec.Template.Spec.Containers[0].Args = args
	if needsCreation {
		deployment, err = internal.GetProfileAnalyzer(namespace, cmd, args)
		if err != nil {
			return err
		}
		err = kubeClient.Create(context.TODO(), deployment)
		if err != nil {
			return err
		}
	} else {
		err = kubeClient.Update(context.TODO(), deployment)
		if err != nil {
			return err
		}
	}
	return nil
}
