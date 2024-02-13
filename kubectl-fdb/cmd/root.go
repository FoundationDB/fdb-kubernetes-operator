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
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	rand.Seed(time.Now().Unix())

	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:          "kubectl-fdb",
		Short:        "kubectl plugin for the FoundationDB operator.",
		Long:         `kubectl fdb plugin for the interaction with the FoundationDB operator.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
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

type processSelectionOptions struct {
	ids               []string
	namespace         string
	clusterName       string
	clusterLabel      string
	processClass      string
	useProcessGroupID bool
}

// getProcessGroupsByCluster returns a map of processGroupIDs by FDB cluster that match the criteria in the provided
// processSelectionOptions.
func getProcessGroupsByCluster(kubeClient client.Client, opts processSelectionOptions) (map[*fdbv1beta2.FoundationDBCluster][]fdbv1beta2.ProcessGroupID, error) {
	if opts.clusterName == "" && opts.clusterLabel == "" {
		return nil, errors.New("processGroups will not be selected without cluster specification")
	}

	// cross-cluster logic: given a list of Pod names, we can look up the FDB clusters by pod label, and work across clusters
	if !opts.useProcessGroupID && opts.clusterName == "" {
		return fetchProcessGroupsCrossCluster(kubeClient, opts.namespace, opts.clusterLabel, opts.ids...)
	}

	// single-cluster logic
	cluster, err := loadCluster(kubeClient, opts.namespace, opts.clusterName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, fmt.Errorf("could not get cluster: %s/%s", opts.namespace, opts.clusterName)
		}
		return nil, err
	}
	// find the desired process groups in the single cluster
	var processGroupIDs []fdbv1beta2.ProcessGroupID
	if opts.processClass != "" { // match against a whole process class, ignore provided ids
		if len(opts.ids) != 0 {
			return nil, fmt.Errorf("process identifiers were provided along with a processClass and would be ignored, please only provide one or the other")
		}
		processGroupIDs = getProcessGroupIdsWithClass(cluster, opts.processClass)
		if len(processGroupIDs) == 0 {
			return nil, fmt.Errorf("found no processGroups of processClass '%s' in cluster %s", opts.processClass, opts.clusterName)
		}
	} else if !opts.useProcessGroupID { // match by pod name
		processGroupIDs, err = getProcessGroupIDsFromPodName(cluster, opts.ids)
		if err != nil {
			return nil, err
		}
	} else { // match by process group ID
		for _, id := range opts.ids {
			processGroupIDs = append(processGroupIDs, fdbv1beta2.ProcessGroupID(id))
		}
	}

	return map[*fdbv1beta2.FoundationDBCluster][]fdbv1beta2.ProcessGroupID{
		cluster: processGroupIDs,
	}, nil
}
