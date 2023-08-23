/*
 * singleton.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2023 Apple Inc. and the FoundationDB project authors
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

package fixtures

import (
	chaosmesh "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"math/rand"
	"os/user"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	controllerRuntimeClient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

type singleton struct {
	options                 *FactoryOptions
	userName                string
	certificate             *corev1.Secret
	config                  *rest.Config
	client                  *kubernetes.Clientset
	controllerRuntimeClient controllerRuntimeClient.Client
	fdbVersion              fdbv1beta2.Version
	namespaces              []string
}

func getSingleton(options *FactoryOptions) (*singleton, error) {
	err := options.validateFlags()
	if err != nil {
		return nil, err
	}

	var userName string
	if options.username == "" {
		var u *user.User
		u, err = user.Current()
		if err != nil {
			return nil, err
		}
		userName = u.Username
	} else {
		userName = options.username
	}

	rand.Seed(time.Now().UnixNano())
	certificate, _ := GenerateCertificate()
	if err != nil {
		return nil, err
	}

	kubeConfig, err := config.GetConfigWithContext(options.context)
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, err
	}
	fdbVersion, err := fdbv1beta2.ParseFdbVersion(options.fdbVersion)
	if err != nil {
		return nil, err
	}

	// Also add Apps v1 and Core v1 to allow to use the controller runtime client
	// to modify Pods, Deployments etc.
	curScheme := runtime.NewScheme()
	err = scheme.AddToScheme(curScheme)
	if err != nil {
		return nil, err
	}
	err = appsv1.AddToScheme(curScheme)
	if err != nil {
		return nil, err
	}
	err = corev1.AddToScheme(curScheme)
	if err != nil {
		return nil, err
	}
	err = fdbv1beta2.AddToScheme(curScheme)
	if err != nil {
		return nil, err
	}
	err = chaosmesh.AddToScheme(curScheme)
	if err != nil {
		return nil, err
	}

	controllerClient, err := LoadControllerRuntimeFromContext(options.context, curScheme)
	if err != nil {
		return nil, err
	}

	return &singleton{
		options:                 options,
		userName:                userName,
		config:                  kubeConfig,
		client:                  client,
		fdbVersion:              fdbVersion,
		certificate:             certificate,
		controllerRuntimeClient: controllerClient,
	}, nil
}
