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
	"context"
	"errors"
	"fmt"
	"github.com/go-logr/logr"
	"io"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/klog/v2"
	"math/rand"
	"sigs.k8s.io/controller-runtime/pkg/cache"
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

func getKubeClient(ctx context.Context, o *fdbBOptions) (client.Client, error) {
	config, err := o.configFlags.ToRESTConfig()
	if err != nil {
		return nil, err
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(fdbv1beta1.AddToScheme(scheme))
	utilruntime.Must(fdbv1beta2.AddToScheme(scheme))

	// Don't printout any log messages from client-go.
	klog.SetLogger(logr.Discard())

	internalClient, err := client.NewWithWatch(config, client.Options{
		Scheme: scheme,
		Opts: client.WarningHandlerOptions{
			SuppressWarnings: true,
		},
	})
	if err != nil {
		return nil, err
	}

	namespace, err := getNamespace(*o.configFlags.Namespace)
	if err != nil {
		return nil, err
	}

	cacheBuilder := cache.MultiNamespacedCacheBuilder([]string{namespace})
	internalCache, err := cacheBuilder(config, cache.Options{
		Scheme: scheme,
		Mapper: internalClient.RESTMapper(),
	})
	if err != nil {
		return nil, err
	}

	// Setup index field to allow access to the node name more efficiently. The indexer must be created before the
	// informer is started.
	err = internalCache.IndexField(ctx, &corev1.Pod{}, "spec.nodeName", func(object client.Object) []string {
		return []string{object.(*corev1.Pod).Spec.NodeName}
	})

	if err != nil {
		return nil, err
	}

	// Setup index field to allow access to the status phase of a Pod more efficiently. The indexer must be created before the
	// informer is started.
	err = internalCache.IndexField(ctx, &corev1.Pod{}, "status.phase", func(object client.Object) []string {
		return []string{string(object.(*corev1.Pod).Status.Phase)}
	})

	if err != nil {
		return nil, err
	}

	// Make sure the internal cache is started.
	go func() {
		_ = internalCache.Start(ctx)
	}()

	internalCache.WaitForCacheSync(ctx)

	return client.NewDelegatingClient(client.NewDelegatingClientInput{
		CacheReader:       internalCache,
		Client:            internalClient,
		UncachedObjects:   nil,
		CacheUnstructured: false,
	})
}

func getNamespace(namespace string) (string, error) {
	if namespace != "" {
		return namespace, nil
	}

	clientCfg, err := clientcmd.NewDefaultClientConfigLoadingRules().Load()
	if err != nil {
		return "", err
	}

	if ctx, ok := clientCfg.Contexts[clientCfg.CurrentContext]; ok {
		if ctx.Namespace != "" {
			return ctx.Namespace, nil
		}
	}

	return "default", nil
}

func getOperator(kubeClient client.Client, operatorName string, namespace string) (*appsv1.Deployment, error) {
	operator := &appsv1.Deployment{}
	err := kubeClient.Get(context.Background(), client.ObjectKey{Namespace: namespace, Name: operatorName}, operator)
	return operator, err
}

func loadCluster(kubeClient client.Client, namespace string, clusterName string) (*fdbv1beta2.FoundationDBCluster, error) {
	cluster := &fdbv1beta2.FoundationDBCluster{}
	err := kubeClient.Get(context.Background(), types.NamespacedName{Namespace: namespace, Name: clusterName}, cluster)
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
	err := kubeClient.List(context.Background(), &nodesList, client.MatchingLabels(nodeSelector))
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
		context.Background(),
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
		return []string{}, err
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
		return podNames, err
	}

	podMap := make(map[string]corev1.Pod)
	for _, pod := range pods.Items {
		podMap[pod.Labels[cluster.GetProcessGroupIDLabel()]] = pod
	}

	for process := range processesSet {
		if _, ok := podMap[string(process)]; !ok {
			fmt.Fprintf(stdErr, "Skipping Process Group: %s, because it does not have a corresponding Pod.\n", process)
			continue
		}
		pod := podMap[string(process)]

		if pod.Labels[cluster.GetProcessGroupIDLabel()] != string(process) {
			continue
		}
		if pod.Status.Phase != corev1.PodRunning {
			fmt.Fprintf(stdErr, "Skipping Process Group: %s, Pod is not running, current phase: %s\n", process, pod.Status.Phase)
			continue
		}

		podNames = append(podNames, pod.Name)
	}

	return podNames, nil
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

// getProcessGroupIdsWithClass returns a list of ProcessGroupIDs in the given cluster which are of the given processClass
func getProcessGroupIdsWithClass(cluster *fdbv1beta2.FoundationDBCluster, processClass string) []fdbv1beta2.ProcessGroupID {
	matchingProcessGroupIDs := []fdbv1beta2.ProcessGroupID{}
	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.ProcessClass != fdbv1beta2.ProcessClass(processClass) {
			continue
		}
		matchingProcessGroupIDs = append(matchingProcessGroupIDs, processGroup.ProcessGroupID)
	}
	return matchingProcessGroupIDs
}

// fetchProcessGroupsCrossCluster fetches the list of process groups matching the given podNames and returns the
// processGroupIDs mapped by clusterName matching the given clusterLabel.
func fetchProcessGroupsCrossCluster(kubeClient client.Client, namespace string, clusterLabel string, podNames ...string) (map[*fdbv1beta2.FoundationDBCluster][]fdbv1beta2.ProcessGroupID, error) {
	var pod corev1.Pod
	podsByClusterName := map[string][]string{} // start with grouping by cluster-label values and load clusters later
	for _, podName := range podNames {
		err := kubeClient.Get(context.Background(), client.ObjectKey{Name: podName, Namespace: namespace}, &pod)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return nil, fmt.Errorf("could not get pod: %s/%s", namespace, podName)
			}
			return nil, err
		}
		clusterName, ok := pod.Labels[clusterLabel]
		if !ok {
			return nil, fmt.Errorf("no cluster-label '%s' found for pod '%s'", clusterLabel, podName)
		}
		podsByClusterName[clusterName] = append(podsByClusterName[clusterName], podName)
	}
	// using the clusterName:podNames map, get a *FoundationDBCluster:progressGroupIDs map
	processGroupsByCluster := map[*fdbv1beta2.FoundationDBCluster][]fdbv1beta2.ProcessGroupID{}
	for clusterName, pods := range podsByClusterName {
		cluster, err := loadCluster(kubeClient, namespace, clusterName)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return nil, fmt.Errorf("could not get cluster: %s/%s", namespace, clusterName)
			}
			return nil, err
		}
		for _, podName := range pods {
			processGroupsByCluster[cluster] = append(processGroupsByCluster[cluster], internal.GetProcessGroupIDFromPodName(cluster, podName))
		}
	}
	return processGroupsByCluster, nil
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

func fetchPodsOnNode(kubeClient client.Client, clusterName string, namespace string, node string, clusterLabel string) (corev1.PodList, error) {
	var pods corev1.PodList
	var err error

	var podLabelSelector client.ListOption

	if clusterName == "" {
		podLabelSelector = client.HasLabels([]string{clusterLabel})
	} else {
		cluster, err := loadCluster(kubeClient, namespace, clusterName)
		if err != nil {
			return pods, fmt.Errorf("unable to load cluster: %s. Error: %w", clusterName, err)
		}

		podLabelSelector = client.MatchingLabels(cluster.GetMatchLabels())
	}

	err = kubeClient.List(context.Background(), &pods,
		client.InNamespace(namespace),
		podLabelSelector,
		client.MatchingFieldsSelector{
			Selector: fields.OneTermEqualSelector("spec.nodeName", node),
		})
	if err != nil {
		return pods, fmt.Errorf("unable to fetch pods. Error: %w", err)
	}

	return pods, nil
}

// getProcessGroupsByCluster returns a map of processGroupIDs by FDB cluster that match the criteria in the provided
// processGroupSelectionOptions.
func getProcessGroupsByCluster(kubeClient client.Client, opts processGroupSelectionOptions) (map[*fdbv1beta2.FoundationDBCluster][]fdbv1beta2.ProcessGroupID, error) {
	if opts.clusterName == "" && opts.clusterLabel == "" {
		return nil, errors.New("processGroups will not be selected without cluster specification")
	}

	// cross-cluster logic: given a list of Pod names, we can look up the FDB clusters by pod label, and work across clusters
	if opts.clusterName == "" {
		if opts.useProcessGroupID {
			return nil, errors.New("processGroup selection without clusterName specification is only supported when podNames are provided")
		}
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
