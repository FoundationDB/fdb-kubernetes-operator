/*
 * version.go
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
	"fmt"
	"log"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
)

// TODO: should follow the operator versioning
var pluginVersion string = "0.0.1"

// versionCmd represents the version command
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "version of kubectl-fdb & foundationdb-operator",
	Long:  `version of kubectl-fdb and current running foundationdb-operator`,
	Run: func(cmd *cobra.Command, args []string) {
		namespace, err := rootCmd.Flags().GetString("namespace")
		if err != nil {
			log.Fatal(err)
		}
		version(namespace)
	},
	Example: `
#Lists the version of kubectl fdb plugin and foundationdb operator in current namespace
kubectl fdb version
#Lists the version of kubectl fdb plugin and foundationdb operator in provided namespace
kubectl fdb -n default version
`,
}

func version(namespace string) {
	fmt.Printf("kubectl-fdb: %s\n", pluginVersion)

	config := getConfig()
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	operatorDeployment := getOperator(client, namespace)
	if operatorDeployment.Name == "" {
		log.Fatalf("could not find the foundationdb operator in the namespace: %s", namespace)
	}
	operatorImage := operatorDeployment.Spec.Template.Spec.Containers[0].Image
	imageName := strings.Split(operatorImage, ":")
	imageVersion := imageName[len(imageName)-1]
	fmt.Printf("foundationd-operator: %s\n", imageVersion)
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
