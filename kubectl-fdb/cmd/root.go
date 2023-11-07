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
	"encoding/json"
	"fmt"
	"github.com/hashicorp/go-retryablehttp"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/fatih/color"

	"strings"

	"github.com/spf13/viper"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/spf13/cobra"
)

const kubectl_fb_release_url = "https://api.github.com/repos/FoundationDB/fdb-kubernetes-operator/releases/latest"

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
func NewRootCmd(streams genericclioptions.IOStreams) *cobra.Command {
	rand.Seed(time.Now().Unix())

	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:          "kubectl-fdb",
		Short:        "kubectl plugin for the FoundationDB operator.",
		Long:         `kubectl fdb plugin for the interaction with the FoundationDB operator.`,
		SilenceUsage: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			checkPluginVersion()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	viper.SetDefault("license", "apache 2")
	cmd.PersistentFlags().StringP("operator-name", "o", "fdb-kubernetes-operator-controller-manager", "Name of the Deployment for the operator.")
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

func checkPluginVersion() {
	retryClient := retryablehttp.NewClient()
	retryClient.RetryMax = 2
	retryClient.RetryWaitMax = 1 * time.Second
	retryClient.Logger = nil
	retryClient.CheckRetry = retryablehttp.ErrorPropagatedRetryPolicy

	retryClient.HTTPClient.Timeout = 1 * time.Second
	req, _ := retryablehttp.NewRequest(http.MethodGet, kubectl_fb_release_url, nil)
	resp, err := retryClient.Do(req)
	if err != nil {
		fmt.Println("Failed to fetch kubectl-fdb version from github")
		os.Exit(0)
	}
	body, err := io.ReadAll(resp.Body)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		fmt.Println("Error in version")
		os.Exit(0)
	}
	var result = GitHubResult{}
	err = json.Unmarshal(body, &result)
	if err != nil {
		fmt.Println("Failed to read version from github response\n", err)
		os.Exit(0)
	}
	ver := result.Version[1:] //removing v from beginning of release version
	if strings.Compare(pluginVersion, "latest") != 0 && strings.Compare(pluginVersion, ver) < 0 {
		pretty, _ := json.MarshalIndent(result, "", " ")
		fmt.Println("Your kubectl-fdb plugin is not up-to-date, please download latest version and try again!")
		fmt.Println("Response from github:", string(pretty))
		os.Exit(0)
	}
}

type GitHubResult struct {
	ID          int64  `json:"id"`
	Version     string `json:"tag_name"`
	Name        string `json:"name"`
	PublishDate string `json:"published_at"`
}
