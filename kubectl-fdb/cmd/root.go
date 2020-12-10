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
	"log"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:   "kubectl-fdb",
	Short: "kubectl plugin for the FoundationDB operator.",
	Long:  `kubectl fdb plugin for the interaction with the FoundationDB operator.`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func getDefaultNamespace() string {
	clientCfg, err := clientcmd.NewDefaultClientConfigLoadingRules().Load()
	if err != nil {
		log.Fatal(err)
	}

	if ctx, ok := clientCfg.Contexts[clientCfg.CurrentContext]; ok {
		return ctx.Namespace
	}

	return "default"
}

// confirmAction requests a user to confirm it's action
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

func init() {
	viper.SetDefault("license", "apache 2")
	rootCmd.Flags().StringP("namespace", "n", getDefaultNamespace(), "namespace to interact with the fdb cluster.")
	rootCmd.Flags().BoolP("force", "f", false, "Suppress the confirmation dialog")
	home := homedir.HomeDir()
	rootCmd.Flags().String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file.")
}
