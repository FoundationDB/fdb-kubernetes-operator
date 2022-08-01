/*
 * exclusion_status.go
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
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func newExclusionStatusCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "exclusion-status",
		Short: "Get the exclusion status for all excluded processes.",
		Long:  "Get the exclusion status for all excluded processes.",
		Args:  cobra.ExactValidArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ignoreFullyExcluded, err := cmd.Flags().GetBool("ignore-fully-excluded")
			if err != nil {
				return err
			}

			interval, err := cmd.Flags().GetDuration("interval")
			if err != nil {
				return err
			}

			config, err := o.configFlags.ToRESTConfig()
			if err != nil {
				return err
			}

			clientSet, err := kubernetes.NewForConfig(config)
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

			cluster, err := loadCluster(kubeClient, namespace, args[0])
			if err != nil {
				return err
			}

			pods, err := getPodsForCluster(kubeClient, cluster, namespace)
			if err != nil {
				return err
			}

			if len(pods.Items) == 0 {
				return fmt.Errorf("no running Pods are found for cluster: %s/%s", cluster.Namespace, cluster.Name)
			}

			// TODO get the pod randomly
			err = getExclusionStatus(cmd, config, clientSet, pods.Items[0].Name, namespace, ignoreFullyExcluded, interval)
			if err != nil {
				return err
			}

			return nil
		},
		Example: `
Experimental feature!

This command shows the ongoing exclusions for a cluster and how much data must be moved before the exclusion is done.

# Get the exclusion status for cluster c1
kubectl fdb get exclusion-status c1


# Get the exclusion status for cluster c1 and prints out processes that are fully excluded
kubectl fdb get exclusion-status c1 --ignore-fully-excluded=false


# Get the exclusion status for cluster c1 and updates the data every 5 minutes
kubectl fdb get exclusion-status c1 --interval=5m
`,
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	cmd.Flags().Bool("ignore-fully-excluded", true, "defines if processes that are fully excluded should be ignored.")
	cmd.Flags().Duration("interval", 1*time.Minute, "defines in which interval new information should be fetched from the cluster.")

	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

type exclusionResult struct {
	id          string
	storedBytes int
	estimate    string
}

func getExclusionStatus(cmd *cobra.Command, restConfig *rest.Config, kubeClient *kubernetes.Clientset, clientPod string, namespace string, ignoreFullyExcluded bool, interval time.Duration) error {
	timer := time.NewTicker(interval)
	previousRun := map[string]int{}

	for {
		out, serr, err := executeCmd(restConfig, kubeClient, clientPod, namespace, "fdbcli --exec 'status json'")
		if err != nil {
			// If an error occurs retry
			cmd.PrintErrln(err)
			continue
		}

		if serr.Len() > 0 {
			cmd.PrintErrln(serr.String())
		}

		res, err := internal.RemoveWarningsInJSON(out.String())
		if err != nil {
			// If an error occurs retry
			cmd.PrintErrln(err)
			continue
		}

		status := &fdbv1beta2.FoundationDBStatus{}
		err = json.Unmarshal(res, status)
		if err != nil {
			// If an error occurs retry
			cmd.PrintErrln(err)
			continue
		}

		var ongoingExclusions []exclusionResult
		for _, process := range status.Cluster.Processes {
			if !process.Excluded {
				continue
			}

			if !ignoreFullyExcluded && len(process.Roles) == 0 {
				cmd.Println(process.Locality["instance_id"], "is fully excluded")
			}

			instance := process.Locality["instance_id"]
			// TODO: Add estimate when an exclusion is done
			// TODO: Add progress bars
			for _, role := range process.Roles {
				roleClass := fdbv1beta2.ProcessClass(role.Role)
				if roleClass.IsStateful() {
					var estimate string

					previousBytes, ok := previousRun[instance]
					if ok {
						// TODO calculate estimates for duration
						_ = previousBytes
						estimate = "N/A"
					}

					// TODO: Check if StoredBytes is the correct value
					ongoingExclusions = append(ongoingExclusions, exclusionResult{
						id:          instance,
						storedBytes: role.StoredBytes,
						estimate:    estimate,
					})

					previousRun[instance] = role.StoredBytes
				}
			}
		}

		if len(ongoingExclusions) == 0 {
			timer.Stop()
			break
		}

		sort.SliceStable(ongoingExclusions, func(i, j int) bool {
			return ongoingExclusions[i].id < ongoingExclusions[j].id
		})

		for _, exclusion := range ongoingExclusions {
			cmd.Printf("%s:\t %d bytes are left - estimate: %s\n", exclusion.id, exclusion.storedBytes, exclusion.estimate)
		}

		cmd.Println("There are", len(ongoingExclusions), "processes that are not fully excluded.")
		cmd.Println("======================================================================================================")
		<-timer.C
	}

	return nil
}
