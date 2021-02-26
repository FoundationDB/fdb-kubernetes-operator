/*
 * remove_instances.go
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

	k8serrors "k8s.io/apimachinery/pkg/api/errors"

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

func newRemoveInstancesCmd(streams genericclioptions.IOStreams, rootCmd *cobra.Command) *cobra.Command {
	o := NewFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "instances",
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
			withExclusion, err := cmd.Flags().GetBool("exclusion")
			if err != nil {
				return err
			}
			withShrink, err := cmd.Flags().GetBool("shrink")
			if err != nil {
				return err
			}
			useInstanceID, err := cmd.Flags().GetBool("use-instance-id")
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

			instances := args
			if !useInstanceID {
				instances, err = getInstanceIDsFromPod(kubeClient, cluster, instances, namespace)
				if err != nil {
					return err
				}
			}

			return removeInstances(kubeClient, cluster, instances, namespace, withExclusion, withShrink, force)
		},
		Example: `
# Remove instances for a cluster in the current namespace
kubectl fdb remove instances -c cluster pod-1 -i pod-2

# Remove instances for a cluster in the namespace default
kubectl fdb -n default remove instances -c cluster pod-1 pod-2

# Remove instances for a cluster with the instance ID.
# The instance ID of a Pod can be fetched with "kubectl get po -L fdb-instance-id"
kubectl fdb -n default remove instances --use-instance-id -c cluster storage-1 storage-2
`,
	}

	cmd.Flags().StringP("fdb-cluster", "c", "", "remove instance(s) from the provided cluster.")
	cmd.Flags().BoolP("exclusion", "e", true, "define if the instances should be removed with exclusion.")
	cmd.Flags().Bool("use-instance-id", false, "if set the operator will use the instance ID to remove it rather than the Pod(s) name.")
	cmd.Flags().Bool("shrink", false, "define if the removed instances should not be replaced.")
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

func getInstanceIDsFromPod(kubeClient client.Client, clusterName string, podNames []string, namespace string) ([]string, error) {
	instances := make([]string, 0, len(podNames))
	// Build a map to filter faster
	podNameMap := map[string]bool{}
	for _, instance := range podNames {
		podNameMap[instance] = true
	}

	pods, err := getPodsForCluster(kubeClient, clusterName, namespace)
	if err != nil {
		return instances, err
	}

	for _, pod := range pods.Items {
		if _, ok := podNameMap[pod.Name]; !ok {
			continue
		}

		instances = append(instances, pod.Labels[controllers.FDBInstanceIDLabel])
	}

	return instances, nil
}

// removeInstances adds instances to the instancesToRemove field
func removeInstances(kubeClient client.Client, clusterName string, instances []string, namespace string, withExclusion bool, withShrink bool, force bool) error {
	var cluster fdbtypes.FoundationDBCluster
	err := kubeClient.Get(ctx.Background(), client.ObjectKey{
		Namespace: namespace,
		Name:      clusterName,
	}, &cluster)

	if err != nil {
		if k8serrors.IsNotFound(err) {
			return fmt.Errorf("could not get cluster: %s/%s", namespace, clusterName)
		}
		return err
	}

	if len(instances) == 0 {
		return nil
	}

	shrinkMap := make(map[fdbtypes.ProcessClass]int)

	if withShrink {
		var pods corev1.PodList
		err := kubeClient.List(ctx.Background(), &pods,
			client.InNamespace(namespace),
			client.MatchingLabels(map[string]string{
				controllers.FDBClusterLabel: clusterName,
			}))

		if err != nil {
			return err
		}

		for _, pod := range pods.Items {
			class := controllers.GetProcessClassFromMeta(pod.ObjectMeta)
			shrinkMap[class]++
		}
	}

	for class, amount := range shrinkMap {
		cluster.Spec.ProcessCounts.DecreaseCount(class, amount)
	}

	if !force {
		confirmed := confirmAction(fmt.Sprintf("Remove %v from cluster %s/%s with exclude: %t and shrink: %t", instances, namespace, clusterName, withExclusion, withShrink))
		if !confirmed {
			return fmt.Errorf("user aborted the removal")
		}
	}

	if withExclusion {
		cluster.Spec.InstancesToRemove = append(cluster.Spec.InstancesToRemove, instances...)
	} else {
		cluster.Spec.InstancesToRemoveWithoutExclusion = append(cluster.Spec.InstancesToRemoveWithoutExclusion, instances...)
	}

	return kubeClient.Update(ctx.TODO(), &cluster)
}
