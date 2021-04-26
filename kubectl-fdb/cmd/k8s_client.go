/*
 * k8s_client.go
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
	"bytes"
	ctx "context"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func getNamespace(namespace string) (string, error) {
	if namespace != "" {
		return namespace, nil
	}

	clientCfg, err := clientcmd.NewDefaultClientConfigLoadingRules().Load()
	if err != nil {
		return "", err
	}

	if context, ok := clientCfg.Contexts[clientCfg.CurrentContext]; ok {
		if context.Namespace != "" {
			return context.Namespace, nil
		}
	}

	return "default", nil
}

func getOperator(kubeClient client.Client, operatorName string, namespace string) (*appsv1.Deployment, error) {
	operator := &appsv1.Deployment{}
	err := kubeClient.Get(ctx.Background(), client.ObjectKey{Namespace: namespace, Name: operatorName}, operator)
	return operator, err
}

func getNodes(kubeClient client.Client, nodeSelector map[string]string) ([]string, error) {
	var nodesList corev1.NodeList
	err := kubeClient.List(ctx.Background(), &nodesList, client.MatchingLabels(nodeSelector))
	if err != nil {
		return []string{}, err
	}

	nodes := make([]string, len(nodesList.Items))

	for _, node := range nodesList.Items {
		nodes = append(nodes, node.Name)
	}

	return nodes, nil
}

func getPodsForCluster(kubeClient client.Client, clusterName string, namespace string) (*corev1.PodList, error) {
	var podList corev1.PodList
	err := kubeClient.List(
		ctx.Background(),
		&podList,
		client.MatchingLabels(map[string]string{
			fdbtypes.FDBClusterLabel: clusterName,
		}),
		client.InNamespace(namespace))

	return &podList, err
}

func executeCmd(restConfig *rest.Config, kubeClient *kubernetes.Clientset, name string, namespace string, command string) (*bytes.Buffer, *bytes.Buffer, error) {
	cmd := []string{
		"/bin/bash",
		"-c",
		command,
	}
	req := kubeClient.CoreV1().RESTClient().Post().
		Resource("pods").Name(name).
		Namespace(namespace).SubResource("exec")

	option := &corev1.PodExecOptions{
		Command:   cmd,
		Container: "foundationdb",
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       true,
	}
	req.VersionedParams(
		option,
		scheme.ParameterCodec,
	)
	exec, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return nil, nil, err
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: &stderr,
	})

	return &stdout, &stderr, err
}

func getAllPodsFromClusterWithCondition(kubeClient client.Client, clusterName string, namespace string, conditions []fdbtypes.ProcessGroupConditionType) ([]string, error) {
	var cluster fdbtypes.FoundationDBCluster
	err := kubeClient.Get(ctx.Background(), client.ObjectKey{
		Namespace: namespace,
		Name:      clusterName,
	}, &cluster)
	if err != nil {
		return []string{}, err
	}

	processesSet := make(map[string]bool)

	for _, condition := range conditions {
		for _, process := range fdbtypes.FilterByCondition(cluster.Status.ProcessGroups, condition, true) {
			if _, ok := processesSet[process]; ok {
				continue
			}

			processesSet[process] = true
		}
	}

	processes := make([]string, 0, len(processesSet))
	pods, err := getPodsForCluster(kubeClient, clusterName, namespace)
	if err != nil {
		return processes, err
	}

	for _, pod := range pods.Items {
		for process := range processesSet {
			if pod.Status.Phase != corev1.PodRunning {
				continue
			}

			if pod.Labels[fdbtypes.FDBInstanceIDLabel] != process {
				continue
			}

			processes = append(processes, pod.Name)
		}
	}

	return processes, nil
}
