/*
 * exclude.go
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

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ctx "context"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

// excludeCmd represents the exclude command of the fdb cli
var excludeCmd = &cobra.Command{
	Use:   "exclude",
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
		withoutExclusion, err := rootCmd.Flags().GetBool("without-exclusion")
		if err != nil {
			log.Fatal(err)
		}

		excludeInstances(cluster, instances, namespace, withoutExclusion)
	},
	Example: `
# Remove instances for a cluster in the current namespace
kubectl fdb exclude -c cluster -i instance-1 -i instance-2

# Remove instances for a cluster in the namespace default
kubectl fdb -n default exclude -c cluster -i instance-1 -i instance-2
`,
}

// excludeInstances adds instances to the instancesToRemove field
func excludeInstances(clusterName string, instances []string, namespace string, withoutExclusion bool) {
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

	var cluster fdbtypes.FoundationDBCluster
	err = kubeClient.Get(ctx.Background(), client.ObjectKey{
		Namespace: namespace,
		Name:      clusterName,
	}, &cluster)

	if err != nil {
		log.Fatal(err)
	}

	// TODO add confirmation?
	if !withoutExclusion {
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
	excludeCmd.Flags().StringP("cluster", "c", "", "exclude instance(s) from the provided cluster.")
	excludeCmd.Flags().StringSliceP("instances", "i", []string{}, "instances to be excluded.")
	excludeCmd.Flags().BoolP("without-exclusion", "w", false, "define if the instances should be remove without exclusion, normally you don't want this.")
	err := excludeCmd.MarkFlagRequired("cluster")

	if err != nil {
		log.Fatal(err)
	}

	err = excludeCmd.MarkFlagRequired("instances")
	if err != nil {
		log.Fatal(err)
	}

	rootCmd.AddCommand(excludeCmd)
}
