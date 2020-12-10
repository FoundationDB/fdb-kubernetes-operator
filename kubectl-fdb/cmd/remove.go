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

	"github.com/FoundationDB/fdb-kubernetes-operator/controllers"
	corev1 "k8s.io/api/core/v1"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ctx "context"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// removeCmd represents the removal of one or multiple instances in a given cluster
var removeCmd = &cobra.Command{
	Use:   "remove",
	Short: "Adds an instance (or multiple) to the remove list of the given cluster",
	Long:  "Adds an instance (or multiple) to the remove list field of the given cluster",
	Run: func(cmd *cobra.Command, args []string) {
		namespace, err := rootCmd.Flags().GetString("namespace")
		if err != nil {
			log.Fatal(err)
		}
		kubeconfig, err := rootCmd.Flags().GetString("kubeconfig")
		if err != nil {
			log.Fatal(err)
		}
		force, err := rootCmd.Flags().GetBool("force")
		if err != nil {
			log.Fatal(err)
		}

		cluster, err := cmd.Flags().GetString("cluster")
		if err != nil {
			log.Fatal(err)
		}
		instances, err := cmd.Flags().GetStringSlice("instances")
		if err != nil {
			log.Fatal(err)
		}
		withExclusion, err := cmd.Flags().GetBool("exclusion")
		if err != nil {
			log.Fatal(err)
		}
		withShrink, err := cmd.Flags().GetBool("shrink")
		if err != nil {
			log.Fatal(err)
		}


		removeInstances(kubeconfig, cluster, instances, namespace, withExclusion, withShrink, force)
	},
	Example: `
# Remove instances for a cluster in the current namespace
kubectl fdb remove -c cluster -i instance-1 -i instance-2

# Remove instances for a cluster in the namespace default
kubectl fdb -n default remove -c cluster -i instance-1 -i instance-2
`,
}

// removeInstances adds instances to the instancesToRemove field
func removeInstances(kubeconfig string, clusterName string, instances []string, namespace string, withExclusion bool, withShrink bool, force bool) {
	if len(instances) == 0 {
		return
	}
	config := getConfig(kubeconfig)
	_ = config

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = fdbtypes.AddToScheme(scheme)

	kubeClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		log.Fatal(err)
	}

	shrinkMap := make(map[string]int)

	if withShrink {
		var pods corev1.PodList
		err = kubeClient.List(ctx.Background(), &pods,
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
	err = kubeClient.Get(ctx.Background(), client.ObjectKey{
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
		confirmed := confirmAction(fmt.Sprintf("Remove %v from cluster %s/%s with exlcude: %t and schrink: %t", instances, namespace, clusterName, withExclusion, withShrink))
		if ! confirmed{
			print("Abort")
			return
		}
	}

	if withExclusion {
		cluster.Spec.InstancesToRemove = append(cluster.Spec.InstancesToRemove, instances...)
	} else {
		cluster.Spec.InstancesToRemoveWithoutExclusion = append(cluster.Spec.InstancesToRemoveWithoutExclusion, instances...)
	}

	err = kubeClient.Update(ctx.Background(), &cluster)
	if err != nil {
		log.Fatal(err)
	}
}

func init() {
	removeCmd.Flags().StringP("cluster", "c", "", "remove instance(s) from the provided cluster.")
	removeCmd.Flags().StringSliceP("instances", "i", []string{}, "instances to be removed.")
	removeCmd.Flags().BoolP("exclusion", "e", true, "define if the instances should be remove with exclusion.")
	removeCmd.Flags().BoolP("shrink", "s", false, "define if the removed instances should not be replaced.")
	err := removeCmd.MarkFlagRequired("cluster")

	if err != nil {
		log.Fatal(err)
	}

	err = removeCmd.MarkFlagRequired("instances")
	if err != nil {
		log.Fatal(err)
	}

	rootCmd.AddCommand(removeCmd)
}
