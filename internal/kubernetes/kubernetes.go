/*
 * kubernetes.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021-2024 Apple Inc. and the FoundationDB project authors
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

package kubernetes

import (
	"bytes"
	"fmt"
	"golang.org/x/net/context"
	"io"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/scheme"
	"log"
	"math/rand"
	"net/url"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// NewSPDYExecutor defines the NewSPDYExecutor method used for this package. For normal code you don't have to change it.
// For unit testing you can set NewSPDYExecutor = FakeNewSPDYExecutor.
var NewSPDYExecutor = remotecommand.NewSPDYExecutor

func getRestClient(kubernetesClient client.Client, config *rest.Config) (rest.Interface, error) {
	gvk := schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Pod",
	}

	return apiutil.RESTClientForGVK(gvk, false, config, serializer.NewCodecFactory(kubernetesClient.Scheme()))
}

// ExecuteCommandRaw will run the command without putting it into a shell.
func ExecuteCommandRaw(
	ctx context.Context,
	kubeClient client.Client,
	config *rest.Config,
	namespace string,
	name string,
	container string,
	command []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	isTty bool,
) error {
	restClient, err := getRestClient(kubeClient, config)
	if err != nil {
		return err
	}

	req := restClient.Post().
		Resource("pods").
		Name(name).
		Namespace(namespace).
		SubResource("exec")
	req.VersionedParams(
		&corev1.PodExecOptions{
			Command:   command,
			Container: container,
			Stdin:     stdin != nil,
			Stdout:    stdout != nil,
			Stderr:    stderr != nil,
			TTY:       isTty,
		},
		scheme.ParameterCodec,
	)
	exec, err := NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return err
	}

	return exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	})
}

// ExecuteCommand executes command in the default container of a Pod with shell, returns stdout and stderr.
func ExecuteCommand(
	ctx context.Context,
	kubeClient client.Client,
	config *rest.Config,
	namespace string,
	name string,
	container string,
	command string,
	printOutput bool,
) (string, string, error) {
	cmd := []string{
		"/bin/bash",
		"-c",
		command,
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := ExecuteCommandRaw(ctx, kubeClient, config, namespace, name, container, cmd, nil, &stdout, &stderr, false)
	if printOutput {
		log.Println("stdout:\n\n", stdout.String())
		log.Println("stderr:\n\n", stderr.String())
	}

	return stdout.String(), stderr.String(), err
}

// ExecuteCommandOnPod runs a command on the provided Pod. The command will be executed inside a bash -c ”.
func ExecuteCommandOnPod(
	ctx context.Context,
	kubeClient client.Client,
	config *rest.Config,
	pod *corev1.Pod,
	container string,
	command string,
	printOutput bool,
) (string, string, error) {
	return ExecuteCommand(ctx, kubeClient, config, pod.Namespace, pod.Name, container, command, printOutput)
}

// GetLogsFromPod will fetch the logs for the specified Pod and container since the provided seconds.
func GetLogsFromPod(
	ctx context.Context,
	kubeClient client.Client,
	config *rest.Config,
	pod *corev1.Pod,
	container string,
	since *int64) (string, error) {
	if pod == nil {
		return "", fmt.Errorf("provided Pod is nil")
	}

	restClient, err := getRestClient(kubeClient, config)
	if err != nil {
		return "", err
	}

	req := restClient.Post().
		Resource("pods").
		Namespace(pod.Namespace).
		Name(pod.Name).
		SubResource("log").
		VersionedParams(&corev1.PodLogOptions{
			Container:    container,
			Follow:       false,
			SinceSeconds: since,
		}, scheme.ParameterCodec)

	readCloser, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = readCloser.Close()
	}()

	logs, err := io.ReadAll(readCloser)
	if err != nil {
		return "", err
	}
	if len(logs) == 0 {
		return "", err
	}

	return string(logs), err
}

// PickRandomPod will return a random Pod from the provided list
func PickRandomPod(pods *corev1.PodList) (*corev1.Pod, error) {
	if pods == nil || len(pods.Items) < 1 {
		return nil, fmt.Errorf("pod list has no items")
	}

	chosen := pods.Items[rand.Intn(len(pods.Items))]
	return &chosen, nil
}

// FakeNewSPDYExecutor can be used for unit testing. Current this will do nothing and will be extended in the future.
var FakeNewSPDYExecutor = func(_ *rest.Config, method string, url *url.URL) (remotecommand.Executor, error) {
	return &FakeExecutor{method: method, url: url}, nil
}

// FakeExecutor used for unit testing.
type FakeExecutor struct {
	method string
	url    *url.URL
}

// Stream opens a protocol streamer to the server and streams until a client closes
// the connection or the server disconnects.
func (fakeExecutor *FakeExecutor) Stream(options remotecommand.StreamOptions) error {
	if options.Stdout != nil {
		if _, err := options.Stdout.Write([]byte{}); err != nil {
			return err
		}
	}

	return nil
}

// StreamWithContext opens a protocol streamer to the server and streams until a client closes
// the connection or the server disconnects or the context is done.
func (fakeExecutor *FakeExecutor) StreamWithContext(_ context.Context, options remotecommand.StreamOptions) error {
	return fakeExecutor.Stream(options)
}
