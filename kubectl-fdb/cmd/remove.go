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
	"fmt"
	"log"

	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/FoundationDB/fdb-kubernetes-operator/controllers"
	corev1 "k8s.io/api/core/v1"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ctx "context"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

func newRemoveCmd(streams genericclioptions.IOStreams, rootCmd *cobra.Command) *cobra.Command {
	o := NewFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Adds an instance (or multiple) to the remove list of the given cluster",
		Long:  "Adds an instance (or multiple) to the remove list field of the given cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			force, err := rootCmd.Flags().GetBool("force")
			if err != nil {
				return err
			}
			cluster, err := cmd.Flags().GetString("fdb-cluster")
			if err != nil {
				return err
			}
			instances, err := cmd.Flags().GetStringSlice("instances")
			if err != nil {
				return err
			}
			withExclusion, err := cmd.Flags().GetBool("exclusion")
			if err != nil {
				return err
			}
			withShrink, err := cmd.Flags().GetBool("shrink")
			if err != nil {
				return err
			}

			config, err := o.configFlags.ToRESTConfig()
			if err != nil {
				return err
			}

			scheme := runtime.NewScheme()
			_ = clientgoscheme.AddToScheme(scheme)
			_ = fdbtypes.AddToScheme(scheme)

			kubeClient, err := client.New(config, client.Options{Scheme: scheme})
			if err != nil {
				return err
			}

			namespace, err := getNamespace(*o.configFlags.Namespace)
			if err != nil {
				return err
			}

			removeInstances(kubeClient, cluster, instances, namespace, withExclusion, withShrink, force)

			return nil
		},
		Example: `
# Remove instances for a cluster in the current namespace
kubectl fdb remove -c cluster -i instance-1 -i instance-2

# Remove instances for a cluster in the namespace default
kubectl fdb -n default remove -c cluster -i instance-1 -i instance-2
`,
	}

	cmd.Flags().StringP("fdb-cluster", "c", "", "remove instance(s) from the provided cluster.")
	cmd.Flags().StringSliceP("instances", "i", []string{}, "instances to be removed.")
	cmd.Flags().BoolP("exclusion", "e", true, "define if the instances should be remove with exclusion.")
	cmd.Flags().Bool("shrink", false, "define if the removed instances should not be replaced.")
	err := cmd.MarkFlagRequired("fdb-cluster")
	if err != nil {
		log.Fatal(err)
	}

	err = cmd.MarkFlagRequired("instances")
	if err != nil {
		log.Fatal(err)
	}

	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

// removeInstances adds instances to the instancesToRemove field
func removeInstances(kubeClient client.Client, clusterName string, instances []string, namespace string, withExclusion bool, withShrink bool, force bool) {
	if len(instances) == 0 {
		return
	}

	shrinkMap := make(map[string]int)

	if withShrink {
		var pods corev1.PodList
		err := kubeClient.List(ctx.Background(), &pods,
			client.InNamespace(namespace),
			client.MatchingLabels(map[string]string{
				"fdb-cluster-name": clusterName,
			}))

		if err != nil {
			log.Fatal(err)
		}

		for _, pod := range pods.Items {
			class := controllers.GetProcessClassFromMeta(pod.ObjectMeta)
			shrinkMap[class]++
		}
	}

	var cluster fdbtypes.FoundationDBCluster
	err := kubeClient.Get(ctx.Background(), client.ObjectKey{
		Namespace: namespace,
		Name:      clusterName,
	}, &cluster)

	if err != nil {
		log.Fatal(err)
	}

	for class, amount := range shrinkMap {
		cluster.Spec.ProcessCounts.DecreaseCount(class, amount)
	}

	if !force {
		confirmed := confirmAction(fmt.Sprintf("Remove %v from cluster %s/%s with exclude: %t and shrink: %t", instances, namespace, clusterName, withExclusion, withShrink))
		if !confirmed {
			print("Abort")
			return
		}
	}

	if withExclusion {
		cluster.Spec.InstancesToRemove = append(cluster.Spec.InstancesToRemove, instances...)
	} else {
		cluster.Spec.InstancesToRemoveWithoutExclusion = append(cluster.Spec.InstancesToRemoveWithoutExclusion, instances...)
	}

	err = kubeClient.Update(ctx.TODO(), &cluster)
	if err != nil {
		log.Fatal(err)
	}
}
