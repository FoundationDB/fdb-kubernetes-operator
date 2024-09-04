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
	"strconv"
	"time"

	kubeHelper "github.com/FoundationDB/fdb-kubernetes-operator/internal/kubernetes"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/fdbstatus"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
)

func newExclusionStatusCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "exclusion-status",
		Short: "Get the exclusion status for all excluded processes.",
		Long:  "Get the exclusion status for all excluded processes.",
		Args:  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
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

			kubeClient, err := getKubeClient(cmd.Context(), o)
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

			pods, err := getRunningPodsForCluster(cmd.Context(), kubeClient, cluster)
			if err != nil {
				return err
			}

			clientPod, err := kubeHelper.PickRandomPod(pods)
			if err != nil {
				return err
			}

			return getExclusionStatus(cmd, config, kubeClient, clientPod, ignoreFullyExcluded, interval)
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
	estimate    string
	storedBytes int
	timestamp   time.Time
}

func getExclusionStatus(cmd *cobra.Command, restConfig *rest.Config, kubeClient client.Client, clientPod *corev1.Pod, ignoreFullyExcluded bool, interval time.Duration) error {
	timer := time.NewTicker(interval)
	previousRun := map[string]exclusionResult{}

	for {
		// TODO: Keeping a stream open is probably more efficient.
		stdout, stderr, err := kubeHelper.ExecuteCommandOnPod(cmd.Context(), kubeClient, restConfig, clientPod, fdbv1beta2.MainContainerName, "fdbcli --exec 'status json'", false)
		if err != nil {
			// If an error occurs retry
			cmd.PrintErrln(err)
			continue
		}

		if stderr != "" {
			cmd.PrintErrln(stderr)
		}

		res, err := fdbstatus.RemoveWarningsInJSON(stdout)
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
		timestamp := time.Now()
		for _, process := range status.Cluster.Processes {
			if !process.Excluded {
				continue
			}

			// If more than one storage server per Pod is running we have to differentiate those processes. If the
			// process ID is not set, fall back to the instance ID.
			instance, ok := process.Locality[fdbv1beta2.FDBLocalityProcessIDKey]
			if !ok {
				instance = process.Locality[fdbv1beta2.FDBLocalityInstanceIDKey]
			}

			if !ignoreFullyExcluded && len(process.Roles) == 0 {
				cmd.Println(instance, "is fully excluded")
				continue
			}

			// TODO: Add progress bars
			for _, role := range process.Roles {
				roleClass := fdbv1beta2.ProcessClass(role.Role)
				if roleClass.IsStateful() {
					var estimate string

					previousResult, ok := previousRun[instance]
					if ok {
						estimateDuration := time.Duration(role.StoredBytes/(previousResult.storedBytes-role.StoredBytes)) * timestamp.Sub(previousResult.timestamp)
						estimate = estimateDuration.String()
					} else {
						estimate = "N/A"
					}

					result := exclusionResult{
						id:          instance,
						storedBytes: role.StoredBytes,
						estimate:    estimate,
						timestamp:   timestamp,
					}
					// TODO: Check if StoredBytes is the correct value
					ongoingExclusions = append(ongoingExclusions, result)

					previousRun[instance] = result
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
			cmd.Printf("%s:\t %s are left - estimate: %s\n", exclusion.id, prettyPrintStoredBytes(exclusion.storedBytes), exclusion.estimate)
		}

		cmd.Println("There are", len(ongoingExclusions), "processes that are not fully excluded.")
		cmd.Println("======================================================================================================")
		<-timer.C
	}

	return nil
}

// prettyPrintStoredBytes will return a string that represents the storedBytes in a human-readable format.
func prettyPrintStoredBytes(storedBytes int) string {
	units := []string{"", "Ki", "Mi", "Gi", "Ti", "Pi"}

	currentBytes := float64(storedBytes)
	for _, unit := range units {
		if currentBytes < 1024 {
			return fmt.Sprintf("%3.2f%s", currentBytes, unit)
		}

		currentBytes /= 1024
	}

	// Fallback will be to printout the bytes.
	return strconv.Itoa(storedBytes)
}
