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
		cluster, err := cmd.Flags().GetString("cluster")
		if err != nil {
			log.Fatal(err)
		}
		instances, err := cmd.Flags().GetStringSlice("instances")
		if err != nil {
			log.Fatal(err)
		}
		namespace, err := rootCmd.Flags().GetString("namespace")
		if err != nil {
			log.Fatal(err)
		}
		withExclusion, err := rootCmd.Flags().GetBool("with-exclusion")
		if err != nil {
			log.Fatal(err)
		}
		withShrink, err := rootCmd.Flags().GetBool("with-shrink")
		if err != nil {
			log.Fatal(err)
		}

		removeInstances(cluster, instances, namespace, withExclusion, withShrink)
	},
	Example: `
# Remove instances for a cluster in the current namespace
kubectl fdb remove -c cluster -i instance-1 -i instance-2

# Remove instances for a cluster in the namespace default
kubectl fdb -n default remove -c cluster -i instance-1 -i instance-2
`,
}

// removeInstances adds instances to the instancesToRemove field
func removeInstances(clusterName string, instances []string, namespace string, withExclusion bool, withShrink bool) {
	if len(instances) == 0 {
		return
	}
	config := getConfig()
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
			// FoundationDBStatusClusterInfo
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

	// TODO: add confirmation?
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
	removeCmd.Flags().BoolP("with-exclusion", "we", true, "define if the instances should be remove with exclusion.")
	removeCmd.Flags().BoolP("with-shrink", "ws", false, "define if the removed instances should not be replaced.")
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
