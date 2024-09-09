/*
 * root.go
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
	"bufio"
	"fmt"
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"log"
	"os"
	"strings"
)

// fdbBOptions provides information required to run different
// actions on FDB
type fdbBOptions struct {
	configFlags *genericclioptions.ConfigFlags
	genericclioptions.IOStreams
}

// newFDBOptions provides an instance of fdbBOptions with default values
func newFDBOptions(streams genericclioptions.IOStreams) *fdbBOptions {
	return &fdbBOptions{
		configFlags: genericclioptions.NewConfigFlags(true),
		IOStreams:   streams,
	}
}

// NewRootCmd provides a cobra command wrapping FDB actions
func NewRootCmd(streams genericclioptions.IOStreams, pluginVersionChecker VersionChecker) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:          "kubectl-fdb",
		Short:        "kubectl plugin for the FoundationDB operator.",
		Long:         `kubectl fdb plugin for the interaction with the FoundationDB operator.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			versionCheck, _ := cmd.Flags().GetBool("version-check")
			if versionCheck {
				return usingLatestPluginVersion(cmd, pluginVersionChecker)
			}

			return nil
		},
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	viper.SetDefault("license", "apache 2")
	cmd.PersistentFlags().StringP("operator-name", "o", "fdb-kubernetes-operator-controller-manager", "Name of the Deployment for the operator.")
	cmd.PersistentFlags().Bool("version-check", true, "If the plugin should check if a newer release of the plugin exists. If so the command will not be executed.")
	cmd.PersistentFlags().BoolP("wait", "w", true, "If the plugin should wait for confirmation before executing any action")
	cmd.PersistentFlags().Uint16P("sleep", "z", 0, "The plugin should sleep between sequential operations for the defined time in seconds (default 0)")
	o.configFlags.AddFlags(cmd.Flags())

	cmd.AddCommand(
		newVersionCmd(streams),
		newRemoveCmd(streams),
		newExecCmd(streams),
		newCordonCmd(streams),
		newRestartCmd(streams),
		newAnalyzeCmd(streams),
		newDeprecationCmd(streams),
		newFixCoordinatorIPsCmd(streams),
		newGetCmd(streams),
		newBuggifyCmd(streams),
		newRecoverMultiRegionClusterCmd(streams),
	)

	return cmd
}

// confirmAction requests a user to confirm its action
func confirmAction(action string) bool {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Printf("%s [y/n]: ", action)

		resp, err := reader.ReadString('\n')
		if err != nil {
			log.Fatal(err)
		}

		resp = strings.ToLower(strings.TrimSpace(resp))

		if resp == "y" || resp == "yes" {
			return true
		}

		if resp == "n" || resp == "no" {
			return false
		}
	}
}

type messageType int

const (
	errorMessage messageType = iota
	warnMessage
	goodMessage
)

func printStatement(cmd *cobra.Command, line string, mesType messageType) {
	if mesType == errorMessage {
		color.Set(color.FgRed)
		cmd.PrintErrf("✖ %s\n", line)
		color.Unset()
		return
	}

	if mesType == warnMessage {
		color.Set(color.FgYellow)
		cmd.PrintErrf("⚠ %s\n", line)
		color.Unset()
		return
	}

	color.Set(color.FgGreen)
	cmd.Printf("✔ %s\n", line)
	color.Unset()
}

// will check plugin version and won't let any interaction happen with cluster, if it's not latest release version
func usingLatestPluginVersion(cmd *cobra.Command, pluginVersionChecker VersionChecker) error {
	// If the user has a self build plugin we are not performing any checks.
	if strings.ToLower(pluginVersion) == "latest" {
		return nil
	}
	latestPluginVersion, err := pluginVersionChecker.getLatestPluginVersion()
	if err != nil {
		return err
	}

	if pluginVersion != latestPluginVersion {
		versionMessage := "kubectl-fdb plugin is not up-to-date, please install the latest version and try again!\n" +
			"Your version:[" + pluginVersion + "], latest release version:[" + latestPluginVersion + "].\n" +
			"Installation instructions can be found here: https://github.com/FoundationDB/fdb-kubernetes-operator/blob/main/kubectl-fdb/Readme.md"
		cmd.Println(versionMessage)
		return fmt.Errorf("outdated plugin version")
	}

	return nil
}

type processGroupSelectionOptions struct {
	ids               []string
	namespace         string
	clusterName       string
	clusterLabel      string
	matchLabels       map[string]string
	processClass      string
	useProcessGroupID bool
	conditions        []fdbv1beta2.ProcessGroupConditionType
}

func addProcessSelectionFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("fdb-cluster", "c", "", "Selects process groups from the provided cluster. "+
		"Required if not passing cluster-label.")
	cmd.Flags().String("process-class", "", "Selects process groups matching the provided value in the provided cluster.  Using this option ignores provided ids.")
	cmd.Flags().StringP("cluster-label", "l", fdbv1beta2.FDBClusterLabel, "cluster label used to identify the cluster for a requested pod. "+
		"It is incompatible with use-process-group-id, process-class, and process-condition.")
	cmd.Flags().Bool("use-process-group-id", false, "Selects process groups by process-group ID instead of the Pod name.")
	cmd.Flags().StringArray("process-condition", []string{}, "Selects process groups that are in any of the given FDB process group conditions.")
	cmd.Flags().StringToString("match-labels", map[string]string{}, "Selects process groups running on pods matching the given labels and are in the provided cluster.  Using this option ignores provided ids.")
}

func getProcessSelectionOptsFromFlags(cmd *cobra.Command, o *fdbBOptions, ids []string) (processGroupSelectionOptions, error) {
	opts := processGroupSelectionOptions{}
	cluster, err := cmd.Flags().GetString("fdb-cluster")
	if err != nil {
		return opts, err
	}
	clusterLabel, err := cmd.Flags().GetString("cluster-label")
	if err != nil {
		return opts, err
	}
	matchLabels, err := cmd.Flags().GetStringToString("match-labels")
	if err != nil {
		return opts, err
	}
	processClass, err := cmd.Flags().GetString("process-class")
	if err != nil {
		return opts, err
	}
	processConditions, err := cmd.Flags().GetStringArray("process-condition")
	if err != nil {
		return opts, err
	}
	conditions, err := convertConditions(processConditions)
	if err != nil {
		return opts, err
	}
	useProcessGroupID, err := cmd.Flags().GetBool("use-process-group-id")
	if err != nil {
		return opts, err
	}
	namespace, err := getNamespace(*o.configFlags.Namespace)
	if err != nil {
		return opts, err
	}
	return processGroupSelectionOptions{
		ids:               ids,
		namespace:         namespace,
		clusterName:       cluster,
		matchLabels:       matchLabels,
		clusterLabel:      clusterLabel,
		processClass:      processClass,
		useProcessGroupID: useProcessGroupID,
		conditions:        conditions,
	}, nil
}
