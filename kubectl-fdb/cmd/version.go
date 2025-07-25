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
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/spf13/cobra"
)

var pluginVersion = "latest"
var pluginBuildDate = "now"
var pluginBuildCommit = "none"

func newVersionCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := newFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "version",
		Short: "version of kubectl-fdb & foundationdb-operator",
		Long:  `version of kubectl-fdb and current running foundationdb-operator`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			operatorName, err := cmd.Root().Flags().GetString("operator-name")
			if err != nil {
				return err
			}
			clientOnly, err := cmd.Flags().GetBool("client-only")
			if err != nil {
				return err
			}
			containerName, err := cmd.Flags().GetString("container-name")
			if err != nil {
				return err
			}

			if !clientOnly {
				kubeClient, err := getKubeClient(cmd.Context(), o)
				if err != nil {
					return err
				}

				namespace, err := getNamespace(*o.configFlags.Namespace)
				if err != nil {
					return err
				}

				operatorVersion, err := version(kubeClient, operatorName, namespace, containerName)
				if err != nil {
					return err
				}
				cmd.Printf("foundationdb-operator: %s\n", operatorVersion)
			}

			cmd.Printf(`kubectl-fdb build information:

version:      %s
build date:   %s
build commit: %s
`, pluginVersion, pluginBuildDate, pluginBuildCommit)

			return nil
		},
		Example: `
# Lists the version of kubectl fdb plugin and foundationdb operator in current namespace
kubectl fdb version

# Lists the version of kubectl fdb plugin and foundationdb operator in provided namespace
kubectl fdb -n default version

# Lists the version of kubectl fdb plugin without checking the operator version
kubectl fdb version --client-only
`,
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	o.configFlags.AddFlags(cmd.Flags())
	cmd.Flags().
		Bool("client-only", false, "Prints out the plugin version only without checking the operator version.")
	cmd.Flags().String("container-name", "manager", "The container name of Kubernetes Deployment.")

	return cmd
}

func version(
	kubeClient client.Client,
	operatorName string,
	namespace string,
	containerName string,
) (string, error) {
	operatorDeployment, err := getOperator(kubeClient, operatorName, namespace)
	if err != nil {
		return "", err
	}

	for _, container := range operatorDeployment.Spec.Template.Spec.Containers {
		if container.Name != containerName {
			continue
		}

		imageName := strings.Split(container.Image, ":")
		return imageName[len(imageName)-1], nil
	}

	return "", fmt.Errorf(
		"could not find container: %s in %s/%s",
		containerName,
		namespace,
		operatorName,
	)
}
