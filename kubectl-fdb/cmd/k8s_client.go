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
	v1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

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

func getOperator(k8sClient kubernetes.Interface, operatorName string, namespace string) *v1.Deployment {
	var operator *v1.Deployment
	operator, err := k8sClient.AppsV1().Deployments(namespace).Get(operatorName, metav1.GetOptions{})
	if err == nil {
		return operator
	}

	return operator
}
