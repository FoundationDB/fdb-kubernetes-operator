/*
 * profile_analyzer.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2022 Apple Inc. and the FoundationDB project authors
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
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"strconv"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"log"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func newProfileAnalyzerCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use: "analyze-profile",
		Short: "Analyze FDB shards to find the busiest team",
		Long: "Analyze FDB shards to find the busiest team",
		RunE: func(cmd *cobra.Command, args []string) error {
			startTime, err := cmd.Flags().GetString("start-time")

			if err != nil {
				return err
			}

			endTime, err := cmd.Flags().GetString("end-time")

			if err != nil {
				return err
			}
			filterRange, err := cmd.Flags().GetBool("filter-get-range")
			if err != nil {
				return err
			}
			topRequests, err := cmd.Flags().GetInt("top-requests")
			if err != nil {
				return err
			}
			kubeClient, err := getKubeClient(o)

			if err != nil {
				return err
			}

			namespace, err := getNamespace(*o.configFlags.Namespace)
			if err != nil {
				return err
			}

			return runProfileAnalyzer(kubeClient, namespace, *o.configFlags.ClusterName, startTime, endTime, filterRange, topRequests, args)
		},
		Example: `
# Run the profiler for cluster-1. We require --cluster option explicitly because analyze commands take lot many arguments.
kubectl fdb analyze-profile --cluster cluster-1 --star-time "01:01 20/07/2022 BST" --end-time "01:30 20/07/2022 BST "
`,
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	cmd.Flags().StringP("fdb-cluster", "c", "", "updates the crash-loop list in the provided cluster.")
	cmd.Flags().Bool("filter-get-range", false, "defines if the analyze tasks should try to fix the found issues (e.g. replace Pods).")
	cmd.Flags().String("start-time", "", "defines all clusters in the given namespace should be analyzed.")
	// We might want to move this into the root cmd if we need this in multiple places
	cmd.Flags().String("end-time", "", "Disable color output.")
	cmd.Flags().Int("top-requests", 100, "")
	err := cmd.MarkFlagRequired("fdb-cluster")
	if err != nil {
		log.Fatal(err)
	}

	o.configFlags.AddFlags(cmd.Flags())
	return cmd
}

func runProfileAnalyzer(kubeClient client.Client, namespace string, clusterName string, startTime string, endTime string, filterRange bool, topRequests int, flags []string) error {
	needsCreation := false
	log.Println(filterRange)
	log.Println(flags)

	job := &batchv1.Job{}
	err := kubeClient.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: "fdb-profile-analyzer"}, job)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			needsCreation = true
		} else {
			return fmt.Errorf("unable to get job from kubeclient %s", err)
		}
	}
	if needsCreation {
		job = &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName + "profile-analyzer",
				Namespace: namespace,
			},
			Spec: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						SecurityContext: &corev1.PodSecurityContext{
							RunAsUser: pointer.Int64(4059),
							RunAsGroup: pointer.Int64(4059),
							FSGroup: pointer.Int64(4059),
						},

						Containers: []corev1.Container{
							{
								Name:    "profile-analyzer",
								Image:   "fdb-profile-analyzer",
								Command: []string{"python3", "./transaction_profiling_analyzer.py"},
								Args: []string{"-C", "$FDB_CLUSTER_FILE", "--start-time", startTime, "--end-time", endTime,
									"--filter-get-range", "--top-requests", strconv.Itoa(topRequests),
								},
								Env: []corev1.EnvVar{
									{Name: "FDB_CLUSTER_FILE", Value: "/var/dynamic-conf/fdb.cluster"},
								},
								Resources: corev1.ResourceRequirements{
									Requests: corev1.ResourceList{
										"cpu":    resource.MustParse("1"),
										"memory": resource.MustParse("1Gi"),
									},
								},
							},
						},
						RestartPolicy: corev1.RestartPolicyNever,
						Volumes: []corev1.Volume{{
							Name: "config-map",
							VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "fdb-profile-analyzer"},
								Items: []corev1.KeyToPath{
									{Key: internal.ClusterFileKey, Path: "fdb.cluster"},
								},
							}},
						}},
					},
				},
				TTLSecondsAfterFinished: pointer.Int32(10800),
			},
		}
		return kubeClient.Create(context.TODO(), job)
	} else {
		job = &batchv1.Job{
			Spec: batchv1.JobSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Command: []string{"python3", "./transaction_profiling_analyzer.py"},
								Args: []string{"-C", "$FDB_CLUSTER_FILE", "--start-time", startTime, "--end-time", endTime,
									"--filter-get-range", "--top-requests", strconv.Itoa(topRequests),
								},
							},
						},
					},
				},
			},
		}
		return kubeClient.Update(context.TODO(), job)
	}
	return nil
}
