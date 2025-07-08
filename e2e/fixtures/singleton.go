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
	"fmt"
	"os/user"
	"sync"

	chaosmesh "github.com/FoundationDB/fdb-kubernetes-operator/v2/e2e/chaos-mesh/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

type singleton struct {
	options                 *FactoryOptions
	userName                string
	config                  *rest.Config
	controllerRuntimeClient client.Client
	fdbVersion              fdbv1beta2.Version
}

var (
	once             = sync.Once{}
	currentSingleton *singleton
	initializedError error
)

func getSingleton(options *FactoryOptions) (*singleton, error) {
	// Setup the singleton once per test suite.
	once.Do(func() {
		initializedError = options.validateFlags()
		if initializedError != nil {
			return
		}

		var userName string
		if options.username == "" {
			var u *user.User
			u, initializedError = user.Current()
			if initializedError != nil {
				return
			}
			userName = u.Username
		} else {
			userName = options.username
		}

		var kubeConfig *rest.Config
		kubeConfig, initializedError = config.GetConfigWithContext(options.context)
		if initializedError != nil {
			return
		}
		var fdbVersion fdbv1beta2.Version
		fdbVersion, initializedError = fdbv1beta2.ParseFdbVersion(options.fdbVersion)
		if initializedError != nil {
			return
		}

		// Also add Apps v1 and Core v1 to allow to use the controller runtime client
		// to modify Pods, Deployments etc.
		curScheme := runtime.NewScheme()
		initializedError = scheme.AddToScheme(curScheme)
		if initializedError != nil {
			return
		}
		initializedError = appsv1.AddToScheme(curScheme)
		if initializedError != nil {
			return
		}
		initializedError = corev1.AddToScheme(curScheme)
		if initializedError != nil {
			return
		}
		initializedError = fdbv1beta2.AddToScheme(curScheme)
		if initializedError != nil {
			return
		}
		initializedError = batchv1.AddToScheme(curScheme)
		if initializedError != nil {
			return
		}
		initializedError = chaosmesh.AddToScheme(curScheme)
		if initializedError != nil {
			return
		}

		var controllerClient client.Client
		controllerClient, initializedError = LoadControllerRuntimeFromContext(
			options.context,
			curScheme,
		)
		if initializedError != nil {
			return
		}

		currentSingleton = &singleton{
			options:                 options,
			userName:                userName,
			config:                  kubeConfig,
			fdbVersion:              fdbVersion,
			controllerRuntimeClient: controllerClient,
		}
	})

	if initializedError != nil {
		return nil, initializedError
	}

	if currentSingleton == nil {
		return nil, fmt.Errorf("singleton was not initialized")
	}

	return currentSingleton, nil
}
