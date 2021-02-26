/*
 * exec.go
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
	ctx "context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/exec"

	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"github.com/FoundationDB/fdb-kubernetes-operator/controllers"
)

func newExecCmd(streams genericclioptions.IOStreams) *cobra.Command {
	o := NewFDBOptions(streams)

	cmd := &cobra.Command{
		Use:   "exec",
		Short: "Runs a command on a container in an FDB cluster",
		Long:  "Runs a command on a container in an FDB cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			cluster, err := cmd.Flags().GetString("fdb-cluster")
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

			err = runExec(kubeClient, cluster, *o.configFlags.Context, namespace, args)
			if err != nil {
				return err
			}

			return nil
		},
		Example: `
 # Open a shell.
 kubectl fdb exec -c cluster

 # Run a status command.
 kubectl fdb exec -c cluster -- fdbcli --exec "status minimal"
 `,
	}

	cmd.Flags().StringP("fdb-cluster", "c", "", "exec into the provided cluster.")
	err := cmd.MarkFlagRequired("fdb-cluster")
	if err != nil {
		log.Fatal(err)
	}
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)
	cmd.SetIn(o.In)

	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}

func buildCommand(kubeClient client.Client, clusterName string, context string, namespace string, commandArgs []string) (exec.Cmd, error) {
	pods := &corev1.PodList{}

	clusterRequirement, err := labels.NewRequirement(controllers.FDBClusterLabel, selection.Equals, []string{clusterName})
	if err != nil {
		return exec.Cmd{}, nil
	}
	processClassRequirement, err := labels.NewRequirement(controllers.FDBProcessClassLabel, selection.Exists, nil)
	if err != nil {
		return exec.Cmd{}, nil
	}

	err = kubeClient.List(ctx.Background(), pods,
		client.InNamespace(namespace),
		client.MatchingLabelsSelector{Selector: labels.NewSelector().Add(*clusterRequirement, *processClassRequirement)},
		client.MatchingFields{"status.phase": "Running"},
	)
	if err != nil {
		return exec.Cmd{}, err
	}
	if len(pods.Items) == 0 {
		return exec.Cmd{}, fmt.Errorf("No usable pods found for cluster %s", clusterName)
	}
	kubectlPath, err := exec.LookPath("kubectl")
	if err != nil {
		return exec.Cmd{}, err
	}

	args := []string{kubectlPath}
	if context != "" {
		args = append(args, "--context", context)
	}

	randomPodIndex := rand.Int31n(int32(len(pods.Items)))

	args = append(args, "--namespace", namespace, "exec", "-it", pods.Items[randomPodIndex].Name)
	if len(commandArgs) > 0 {
		args = append(args, "--")
		args = append(args, commandArgs...)
	} else {
		args = append(args, "--", "bash")
	}

	execCommand := exec.Cmd{
		Path:   kubectlPath,
		Args:   args,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}

	return execCommand, nil
}

func runExec(kubeClient client.Client, clusterName string, context string, namespace string, commandArgs []string) error {
	command, err := buildCommand(kubeClient, clusterName, context, namespace, commandArgs)
	if err != nil {
		return err
	}
	return command.Run()
}
