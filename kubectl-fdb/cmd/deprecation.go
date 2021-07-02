/*
 * deprecation.go
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
	"fmt"
	"strings"

	"github.com/FoundationDB/fdb-kubernetes-operator/internal"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/google/go-cmp/cmp"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

func newDeprecationCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "deprecation",
		Short: "Checks the given cluster(s) for deprecated settings",
		Long: `Checks the given cluster(s) for deprecated settings. 
The result will be presented as a diff.
Deprecated settings that should be replace by a newer setting (or removed) are prefixed with a -`,
		RunE: func(cmd *cobra.Command, args []string) error {
			useFutureDefaults, err := cmd.Flags().GetBool("use-future-defaults")
			if err != nil {
				return err
			}
			onlyShowChanges, err := cmd.Flags().GetBool("only-show-changes")
			if err != nil {
				return err
			}
			showClusterSpec, err := cmd.Flags().GetBool("show-cluster-spec")
			if err != nil {
				return err
			}

			config, err := o.configFlags.ToRESTConfig()
			if err != nil {
				return err
			}

			scheme := runtime.NewScheme()
			_ = clientgoscheme.AddToScheme(scheme)
			_ = fdbtypes.AddToScheme(scheme)

			kubeClient, err := client.New(config, client.Options{Scheme: scheme})
			if err != nil {
				return err
			}

			namespace, err := getNamespace(*o.configFlags.Namespace)
			if err != nil {
				return err
			}

			return checkDeprecation(cmd, kubeClient, args, namespace, internal.DeprecationOptions{
				UseFutureDefaults: useFutureDefaults,
				OnlyShowChanges:   onlyShowChanges,
			}, showClusterSpec)
		},
		Example: `
# Shows deprecations for all clusters in the current namespace
kubectl fdb deprecation 

# Shows deprecations for all clusters in the namespace fdb
kubectl fdb -n fdb deprecation 

# Shows all deprecations for the sample-cluster
kubectl fdb deprecation sample-cluster

# Shows deprecations for all clusters in the current namespace with the future defaults
kubectl fdb deprecation --use-future-defaults

# Shows deprecations for all clusters in the current namespace including unset defaults
kubectl fdb deprecation --only-show-changes=false

# Shows the cluster spec for all clusters in the current namespace with deprecations
kubectl fdb deprecation --show-cluster-spec`,
	}

	cmd.Flags().Bool("use-future-defaults", false,
		"Whether we should apply the latest defaults rather than the defaults that were initially established for this major version.",
	)
	cmd.Flags().Bool("only-show-changes", true,
		"Whether we should only fill in defaults that have changes between major versions of the operator.",
	)
	cmd.Flags().Bool("show-cluster-spec", false,
		"Instead of a diff this will printout the new expected cluster spec.",
	)

	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)
	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

func checkDeprecation(cmd *cobra.Command, kubeClient client.Client, inputClusters []string, namespace string, deprecationOptions internal.DeprecationOptions, showClusterSpec bool) error {
	clusters := &fdbtypes.FoundationDBClusterList{}

	err := kubeClient.List(context.Background(), clusters, client.InNamespace(namespace))
	if err != nil {
		return err
	}

	deprecationCounter := 0
	clusterCounter := 0
	for _, cluster := range clusters.Items {
		// If we have some input clusters we have to filter them
		if len(inputClusters) > 0 {
			contains := false
			for _, inputCluster := range inputClusters {
				if cluster.Name != inputCluster {
					continue
				}

				contains = true
				break
			}

			if !contains {
				continue
			}
		}

		clusterCounter++
		originalSpec := cluster.Spec.DeepCopy()
		err = internal.NormalizeClusterSpec(&cluster.Spec, deprecationOptions)
		if err != nil {
			return err
		}

		originalYAML, err := yaml.Marshal(originalSpec)
		if err != nil {
			return err
		}

		normalizedYAML, err := yaml.Marshal(cluster.Spec)
		if err != nil {
			return err
		}

		diff := cmp.Diff(originalYAML, normalizedYAML)
		if diff == "" {
			if !showClusterSpec {
				cmd.Printf("Cluster %s has no deprecation\n", cluster.Name)
			}
			continue
		}

		if showClusterSpec {
			cmd.Println("---")
			cmd.Println(string(normalizedYAML))
		} else {
			cmd.Printf("Cluster %s has deprecations\n", cluster.Name)
			cmd.Println(strings.TrimSpace(diff))
		}

		deprecationCounter++
	}

	if deprecationCounter > 0 {
		return fmt.Errorf("%d/%d cluster(s) with deprecations", deprecationCounter, clusterCounter)
	}

	cmd.Printf("%d cluster(s) without deprecations\n", clusterCounter)
	return nil
}
