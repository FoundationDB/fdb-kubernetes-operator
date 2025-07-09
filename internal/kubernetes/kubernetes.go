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
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/url"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/cmd/exec"
	"k8s.io/kubectl/pkg/scheme"
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

	httpClient, err := rest.HTTPClientFor(config)
	if err != nil {
		return nil, err
	}

	return apiutil.RESTClientForGVK(
		gvk,
		false,
		config,
		serializer.NewCodecFactory(kubernetesClient.Scheme()),
		httpClient,
	)
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
	// The exec.StreamOptions struct is used below to set up the tty if needed. This will prevent issues with
	// output and input if another terminal is started, e.g. is someone does kubectl fdb -c dev -- fdbcli
	opts := &exec.StreamOptions{
		PodName:       name,
		Namespace:     namespace,
		ContainerName: container,
		Stdin:         stdin != nil,
		TTY:           isTty,
		IOStreams: genericclioptions.IOStreams{
			In:     stdin,
			Out:    stdout,
			ErrOut: stderr,
		},
	}

	// Those lines are copied and adjusted from https://github.com/kubernetes/kubectl/blob/master/pkg/cmd/exec/exec.go#L355
	t := opts.SetupTTY()
	var sizeQueue remotecommand.TerminalSizeQueue
	if t.Raw {
		// this call spawns a goroutine to monitor/update the terminal size
		sizeQueue = t.MonitorSize(t.GetSize())

		// unset p.Err if it was previously set because both stdout and stderr go over p.Out when tty is
		// true
		opts.ErrOut = nil
	}

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
			Stdin:     opts.Stdin,
			Stdout:    opts.Out != nil,
			Stderr:    opts.ErrOut != nil,
			TTY:       t.Raw,
		},
		scheme.ParameterCodec,
	)
	executor, err := NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return err
	}

	return t.Safe(func() error {
		return executor.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdin:             opts.In,
			Stdout:            opts.Out,
			Stderr:            opts.ErrOut,
			Tty:               t.Raw,
			TerminalSizeQueue: sizeQueue,
		})
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
	err := ExecuteCommandRaw(
		ctx,
		kubeClient,
		config,
		namespace,
		name,
		container,
		cmd,
		nil,
		&stdout,
		&stderr,
		false,
	)
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
	return ExecuteCommand(
		ctx,
		kubeClient,
		config,
		pod.Namespace,
		pod.Name,
		container,
		command,
		printOutput,
	)
}

// DownloadFile will download the file from the provided Pod/container into dst.
func DownloadFile(
	ctx context.Context,
	kubeClient client.Client,
	config *rest.Config,
	target *corev1.Pod,
	container string,
	src string,
	dst io.Writer) error {
	errOut := bytes.NewBuffer([]byte{})
	reader, writer := io.Pipe()
	var wg errgroup.Group

	defer func() {
		log.Println("Done downloading file")
	}()

	// Copy the content from stdout of the container to the new file.
	wg.Go(func() error {
		defer func() {
			_ = reader.Close()
		}()
		_, err := io.Copy(dst, reader)
		return err
	})

	err := ExecuteCommandRaw(
		ctx,
		kubeClient,
		config,
		target.Namespace,
		target.Name,
		container,
		[]string{"/bin/cp", src, "/dev/stdout"},
		nil,
		writer,
		errOut,
		false,
	)
	if err != nil {
		log.Println(errOut.String())
	}

	// Close the writer to let the reader terminate.
	_ = writer.Close()

	// In case of a pipe error return the pipe error, otherwise return err.
	pipeErr := wg.Wait()
	if pipeErr != nil {
		return pipeErr
	}

	return err
}

// UploadFile uploads a file from src into the Pod/container dst.
func UploadFile(
	ctx context.Context,
	kubeClient client.Client,
	config *rest.Config,
	target *corev1.Pod,
	container string,
	src io.Reader,
	dst string) error {
	out := bytes.NewBuffer([]byte{})
	errOut := bytes.NewBuffer([]byte{})
	reader, writer := io.Pipe()
	var wg errgroup.Group

	defer func() {
		log.Println("Done uploading file")
	}()

	// Read the file provided via src and pipe it to the reader.
	wg.Go(func() error {
		defer func() {
			_ = writer.Close()
		}()
		_, err := io.Copy(writer, src)
		return err
	})

	err := ExecuteCommandRaw(
		ctx,
		kubeClient,
		config,
		target.Namespace,
		target.Name,
		container,
		[]string{"tee", "-a", dst},
		reader,
		out,
		errOut,
		false,
	)
	if err != nil {
		log.Println(errOut.String())
	}

	// Close the reader to let the writer terminate.
	_ = reader.Close()

	// In case of a pipe error return the pipe error, otherwise return err.
	pipeErr := wg.Wait()
	if pipeErr != nil {
		return pipeErr
	}

	return err
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

	req := restClient.Get().
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
func (fakeExecutor *FakeExecutor) StreamWithContext(
	_ context.Context,
	options remotecommand.StreamOptions,
) error {
	return fakeExecutor.Stream(options)
}
