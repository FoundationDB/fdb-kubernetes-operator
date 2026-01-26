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
	"os"
	"sort"
	"strings"
	"time"

	kubeHelper "github.com/FoundationDB/fdb-kubernetes-operator/v2/internal/kubernetes"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbstatus"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/rest"
)

// Format: "ProcessID (30 chars) [bar] BytesLeft (15 chars) (ETA: time) (20 chars)"
// Total non-bar space: ~65 chars, plus some padding
const overhead = 90

func newExclusionStatusCmd(streams genericiooptions.IOStreams) *cobra.Command {
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

			return getExclusionStatus(
				cmd,
				config,
				kubeClient,
				clientPod,
				ignoreFullyExcluded,
				interval,
			)
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

	cmd.Flags().
		Bool("ignore-fully-excluded", true, "defines if processes that are fully excluded should be ignored.")
	cmd.Flags().
		Duration("interval", 1*time.Minute, "defines in which interval new information should be fetched from the cluster.")

	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

// clearScreen clears the entire terminal screen and moves cursor to home
func clearScreen() {
	fmt.Print("\033[2J\033[H")
}

// isTerminal checks if output is to a terminal (not piped)
func isTerminal(out interface{}) bool {
	if f, ok := out.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}

	return false
}

// getTerminalWidth returns the width of the terminal, or a default if unavailable
func getTerminalWidth(out interface{}) int {
	if f, ok := out.(*os.File); ok {
		width, _, err := term.GetSize(int(f.Fd()))
		if err != nil || width <= 0 {
			// Default width if terminal size cannot be determined
			return 120
		}

		return width
	}

	return 120
}

// calculateProgressBarWidth calculates the appropriate progress bar width
// based on terminal width, accounting for other text on the line
func calculateProgressBarWidth(terminalWidth int) int {
	barWidth := terminalWidth - overhead

	// Ensure minimum and maximum bar widths
	if barWidth < 20 {
		return 20
	}

	if barWidth > 60 {
		return 120
	}

	return barWidth
}

// renderProgressBar creates a visual progress bar
func renderProgressBar(storedBytes int, maxBytes int, width int) string {
	if maxBytes == 0 {
		return strings.Repeat("━", width)
	}

	percentage := float64(maxBytes-storedBytes) / float64(maxBytes)
	if percentage < 0 {
		percentage = 0
	}
	if percentage > 1 {
		percentage = 1
	}

	filled := int(float64(width) * percentage)
	empty := width - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	return fmt.Sprintf("[%s] %.1f%%", bar, percentage*100)
}

// trackInitialBytes stores the initial stored bytes for each process
func trackInitialBytes(
	previousRun map[string]exclusionResult,
	instance string,
	storedBytes int,
) int {
	if prev, ok := previousRun[instance]; ok {
		// If we have a previous run and it has an initial value, use that
		if prev.initialBytes > 0 {
			return prev.initialBytes
		}
	}
	// Otherwise, this is the first time seeing this process
	return storedBytes
}

type exclusionResult struct {
	id           string
	estimate     string
	storedBytes  int
	initialBytes int
	timestamp    time.Time
}

func getExclusionStatus(
	cmd *cobra.Command,
	restConfig *rest.Config,
	kubeClient client.Client,
	clientPod *corev1.Pod,
	ignoreFullyExcluded bool,
	interval time.Duration,
) error {
	timer := time.NewTicker(interval)
	previousRun := map[string]exclusionResult{}
	firstRun := true
	useBarChart := isTerminal(cmd.OutOrStdout())
	// Get terminal width and calculate progress bar width
	progressBarWidth := calculateProgressBarWidth(getTerminalWidth(cmd.OutOrStdout()))
	separator := strings.Repeat("=", progressBarWidth)

	for {
		// TODO: Keeping a stream open is probably more efficient.
		stdout, stderr, err := kubeHelper.ExecuteCommandOnPod(
			cmd.Context(),
			kubeClient,
			restConfig,
			clientPod,
			fdbv1beta2.MainContainerName,
			"fdbcli --exec 'status json'",
			false,
		)
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

			if instance == "" {
				continue
			}

			if !ignoreFullyExcluded && len(process.Roles) == 0 {
				cmd.Println(instance, "is fully excluded")
				continue
			}

			for _, role := range process.Roles {
				roleClass := fdbv1beta2.ProcessClass(role.Role)
				if roleClass.IsStateful() {
					var estimate string
					initialBytes := trackInitialBytes(previousRun, instance, role.StoredBytes)

					previousResult, ok := previousRun[instance]
					if ok {
						divider := previousResult.storedBytes - role.StoredBytes
						if divider == 0 {
							estimate = "N/A"
						} else {
							estimateDuration := time.Duration(
								role.StoredBytes/divider,
							) * timestamp.Sub(previousResult.timestamp)
							estimate = estimateDuration.String()
						}
					} else {
						estimate = "N/A"
					}

					result := exclusionResult{
						id:           instance,
						storedBytes:  role.StoredBytes,
						initialBytes: initialBytes,
						estimate:     estimate,
						timestamp:    timestamp,
					}
					// TODO: Check if StoredBytes is the correct value
					ongoingExclusions = append(ongoingExclusions, result)

					previousRun[instance] = result
				}
			}
		}

		if len(ongoingExclusions) == 0 {
			timer.Stop()
			if useBarChart && !firstRun {
				cmd.Println("\nAll exclusions completed!")
			}
			break
		}

		sort.SliceStable(ongoingExclusions, func(i, j int) bool {
			return ongoingExclusions[i].id < ongoingExclusions[j].id
		})

		// Clear screen if using bar chart and not first run
		if useBarChart && !firstRun {
			clearScreen()
		}

		// Print header
		cmd.Println("Exclusion Status - Last updated:", timestamp.Format("15:04:05"))
		cmd.Println(separator)

		// Print each process with progress bar
		for _, exclusion := range ongoingExclusions {
			if useBarChart {
				bar := renderProgressBar(
					exclusion.storedBytes,
					exclusion.initialBytes,
					progressBarWidth,
				)
				cmd.Printf(
					"%-30s\t%s %s (ETA: %s)\n",
					exclusion.id,
					bar,
					fdbstatus.PrettyPrintBytes(int64(exclusion.storedBytes)),
					exclusion.estimate,
				)
			} else {
				// Fallback to original format for non-terminal output
				cmd.Printf(
					"%s:\t %s are left - estimate: %s\n",
					exclusion.id,
					fdbstatus.PrettyPrintBytes(int64(exclusion.storedBytes)),
					exclusion.estimate,
				)
			}
		}

		// Print summary
		cmd.Println(separator)
		cmd.Printf(
			"Total processes being excluded: %d | Next update in: %s\n",
			len(ongoingExclusions),
			interval,
		)

		firstRun = false

		<-timer.C
	}

	return nil
}
