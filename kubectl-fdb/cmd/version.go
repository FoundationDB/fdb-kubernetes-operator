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

	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
)

var pluginVersion = "v0.23.1"

func newVersionCmd(streams genericclioptions.IOStreams, rootCmd *cobra.Command) *cobra.Command {
	o := NewFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "version",
		Short: "version of kubectl-fdb & foundationdb-operator",
		Long:  `version of kubectl-fdb and current running foundationdb-operator`,
		RunE: func(cmd *cobra.Command, args []string) error {
			operatorName, err := rootCmd.Flags().GetString("operator-name")
			if err != nil {
				return err
			}

			config, err := o.configFlags.ToRESTConfig()
			if err != nil {
				return err
			}

			client, err := kubernetes.NewForConfig(config)
			if err != nil {
				return err
			}

			namespace, err := getNamespace(*o.configFlags.Namespace)
			if err != nil {
				return err
			}

			operatorVersion := version(client, operatorName, namespace)

			// TODO check: https://github.com/kubernetes/cli-runtime/tree/release-1.20/pkg/printers
			fmt.Printf("kubectl-fdb: %s\n", pluginVersion)
			fmt.Printf("foundationdb-operator: %s\n", operatorVersion)

			return nil
		},
		Example: `
#Lists the version of kubectl fdb plugin and foundationdb operator in current namespace
kubectl fdb version
#Lists the version of kubectl fdb plugin and foundationdb operator in provided namespace
kubectl fdb -n default version
`,
	}
	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

func version(client kubernetes.Interface, operatorName string, namespace string) string {
	operatorDeployment := getOperator(client, operatorName, namespace)
	if operatorDeployment.Name == "" {
		log.Fatalf("could not find the foundationdb-operator in the namespace: %s", namespace)
	}
	operatorImage := operatorDeployment.Spec.Template.Spec.Containers[0].Image
	imageName := strings.Split(operatorImage, ":")

	return imageName[len(imageName)-1]
}
