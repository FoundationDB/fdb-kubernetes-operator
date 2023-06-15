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
	"fmt"
	"math/rand"
	"strings"

	fdbv1beta1 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func getKubeClient(o *fdbBOptions) (client.Client, error) {
	config, err := o.configFlags.ToRESTConfig()
	if err != nil {
		return nil, err
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(fdbv1beta1.AddToScheme(scheme))
	utilruntime.Must(fdbv1beta2.AddToScheme(scheme))

	return client.New(config, client.Options{Scheme: scheme})
}

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

func loadCluster(kubeClient client.Client, namespace string, clusterName string) (*fdbv1beta2.FoundationDBCluster, error) {
	cluster := &fdbv1beta2.FoundationDBCluster{}
	err := kubeClient.Get(ctx.Background(), types.NamespacedName{Namespace: namespace, Name: clusterName}, cluster)
	if err != nil {
		return nil, err
	}
	err = internal.NormalizeClusterSpec(cluster, internal.DeprecationOptions{})
	if err != nil {
		return nil, err
	}
	return cluster, err
}

func getNodes(kubeClient client.Client, nodeSelector map[string]string) ([]string, error) {
	var nodesList corev1.NodeList
	err := kubeClient.List(ctx.Background(), &nodesList, client.MatchingLabels(nodeSelector))
	if err != nil {
		return []string{}, err
	}

	nodes := make([]string, 0, len(nodesList.Items))

	for _, node := range nodesList.Items {
		nodes = append(nodes, node.Name)
	}

	return nodes, nil
}

func getPodsForCluster(kubeClient client.Client, cluster *fdbv1beta2.FoundationDBCluster) (*corev1.PodList, error) {
	var podList corev1.PodList
	err := kubeClient.List(
		ctx.Background(),
		&podList,
		client.MatchingLabels(cluster.GetMatchLabels()),
		client.InNamespace(cluster.GetNamespace()))

	return &podList, err
}

func executeCmd(restConfig *rest.Config, kubeClient *kubernetes.Clientset, podName string, namespace string, command string) (*bytes.Buffer, *bytes.Buffer, error) {
	cmd := []string{
		"/bin/bash",
		"-c",
		command,
	}
	req := kubeClient.CoreV1().RESTClient().Post().
		Resource("pods").Name(podName).
		Namespace(namespace).SubResource("exec")

	option := &corev1.PodExecOptions{
		Command:   cmd,
		Container: fdbv1beta2.MainContainerName,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       true,
	}
	req.VersionedParams(
		option,
		clientgoscheme.ParameterCodec,
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

func getAllPodsFromClusterWithCondition(stdErr io.Writer, kubeClient client.Client, clusterName string, namespace string, conditions []fdbv1beta2.ProcessGroupConditionType) ([]string, error) {
	cluster, err := loadCluster(kubeClient, namespace, clusterName)
	if err != nil {
		return []string{}, nil, err
	}

	processesSet := make(map[fdbv1beta2.ProcessGroupID]bool)

	for _, condition := range conditions {
		for _, process := range fdbv1beta2.FilterByCondition(cluster.Status.ProcessGroups, condition, true) {
			if _, ok := processesSet[process]; ok {
				continue
			}

			processesSet[process] = true
		}
	}

	podNames := make([]string, 0, len(processesSet))
	pods, err := getPodsForCluster(kubeClient, cluster)
	if err != nil {
		return podNames, nil, err
	}

	podMap := make(map[string]corev1.Pod)
	for _, pod := range pods.Items {
		podMap[pod.Labels[cluster.GetProcessGroupIDLabel()]] = pod
	}

	var warnings []string
	for process := range processesSet {
		if _, ok := podMap[string(process)]; !ok {
			fmt.Fprintln(stdErr, ("Process Group: %s, is missing pods.", process)
			continue
		}
		if podMap[string(process)].Labels[cluster.GetProcessGroupIDLabel()] != string(process) {
			continue
		}
		if podMap[string(process)].Status.Phase != corev1.PodRunning {
			continue
		}

		podNames = append(podNames, podMap[string(process)].Name)
	}

	if len(warnings) > 0 {
		return podNames, warnings, nil
	}
	return podNames, nil, nil
}

// getProcessGroupIDsFromPodName returns the process group IDs based on the cluster configuration.
func getProcessGroupIDsFromPodName(cluster *fdbv1beta2.FoundationDBCluster, podNames []string) ([]fdbv1beta2.ProcessGroupID, error) {
	processGroupIDs := make([]fdbv1beta2.ProcessGroupID, 0, len(podNames))

	// TODO(johscheuer): We could validate if the provided process group is actually part of the cluster
	for _, podName := range podNames {
		if podName == "" {
			continue
		}

		if !strings.HasPrefix(podName, cluster.Name) {
			return nil, fmt.Errorf("cluster name %s is not set as prefix for Pod name %s, please ensure the specified Pod is part of the cluster", cluster.Name, podName)
		}

		processGroupIDs = append(processGroupIDs, internal.GetProcessGroupIDFromPodName(cluster, podName))
	}

	return processGroupIDs, nil
}

func chooseRandomPod(pods *corev1.PodList) (*corev1.Pod, error) {
	items := pods.Items
	if len(items) == 0 {
		return nil, fmt.Errorf("no pods available")
	}

	var candidate *corev1.Pod

	var tries int
	for candidate == nil || !candidate.GetDeletionTimestamp().IsZero() || tries > 10 {
		candidate = &items[rand.Intn(len(items))]
		tries++
	}

	return candidate, nil
}
