/*
 * operator_helper.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2022 Apple Inc. and the FoundationDB project authors
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

package helper

import (
	"context"
	"os"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/e2e-framework/klient/decoder"
	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/k8s/resources"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

// InstallOperator is a helper method to install the FDB operator inside the test namespace.
func InstallOperator(ctx context.Context, cfg *envconf.Config, _ *testing.T) (context.Context, error) {
	r, err := resources.New(cfg.Client().RESTConfig())
	if err != nil {
		return ctx, err
	}

	ns := ctx.Value(keyNamespaceID).(string)

	// TODO (johscheuer): Make the image a flag that can be passed
	err = decoder.DecodeEachFile(ctx, os.DirFS("../config/samples"), "deployment.yaml",
		decoder.CreateHandler(r),
		decoder.MutateNamespace(ns),
		mutateOperatorImage("manager", "foundationdb/fdb-kubernetes-operator:latest"),
	)
	if err != nil {
		return ctx, err
	}

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fdb-kubernetes-operator-controller-manager",
			Namespace: ns,
		},
	}

	err = wait.For(conditions.New(cfg.Client().Resources(ns)).ResourceMatch(deploy, func(object k8s.Object) bool {
		d := object.(*appsv1.Deployment)
		return d.Status.ReadyReplicas >= 1
	}), wait.WithTimeout(time.Minute*2))

	return ctx, err
}

func mutateOperatorImage(name string, image string) decoder.DecodeOption {
	return decoder.MutateOption(func(obj k8s.Object) error {
		deploy, ok := obj.(*appsv1.Deployment)
		if !ok {
			return nil
		}

		for idx, container := range deploy.Spec.Template.Spec.Containers {
			if container.Name != name {
				continue
			}

			container.Image = image
			deploy.Spec.Template.Spec.Containers[idx] = container
		}

		return nil
	})
}
