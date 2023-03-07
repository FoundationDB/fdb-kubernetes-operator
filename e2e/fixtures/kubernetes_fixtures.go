/*
 * kubernetes_fixtures.go
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
	ctx "context"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"log"
	"regexp"
)

// MultipleNamespaces creates multiple namespaces for HA testing.
func (factory *Factory) MultipleNamespaces(dcIDs []string) []string {
	res := make([]string, len(dcIDs))
	for idx, dcID := range dcIDs {
		namespace := factory.createNamespace(dcID)
		log.Println("Create namespace" + namespace)
		res[idx] = namespace
	}

	factory.singleton.namespaces = res

	return res
}

// SingleNamespace returns a single namespace.
func (factory *Factory) SingleNamespace() string {
	if len(factory.singleton.namespaces) > 0 {
		return factory.singleton.namespaces[0]
	}

	namespace := factory.createNamespace("")
	if len(factory.singleton.namespaces) == 0 {
		factory.singleton.namespaces = append(factory.singleton.namespaces, namespace)
	}

	return namespace
}

func (factory *Factory) createNamespace(suffix string) string {
	namespace := factory.options.namespace

	if namespace == "" {
		namespace = factory.singleton.userName + "-" + RandStringRunes(8)
		matched, err := regexp.Match(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`, []byte(namespace))
		if !matched {
			return ""
		}

		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}

	if suffix != "" {
		namespace = namespace + "-" + suffix
	}

	factory.ensureNamespaceExists(namespace)
	factory.ensureRBACSetupExists(namespace)
	gomega.Expect(factory.ensureFDBOperatorExists(namespace)).ToNot(gomega.HaveOccurred())
	log.Printf("using namespace %s for testing", namespace)
	factory.addShutdownHook(func() error {
		log.Printf("finished all tests, start deleting namespace %s\n", namespace)
		err := factory.GetControllerRuntimeClient().
			Delete(ctx.Background(), &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			})
		if k8serrors.IsNotFound(err) {
			return nil
		}

		return err
	})

	return namespace
}

func (factory *Factory) ensureNamespaceExists(namespace string) {
	gomega.Expect(factory.CreateIfAbsent(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: factory.GetDefaultLabels(),
		},
	})).NotTo(gomega.HaveOccurred())

	secret := factory.getCertificate()
	secret.SetNamespace(namespace)
	secret.SetResourceVersion("")

	gomega.Expect(factory.CreateIfAbsent(secret)).NotTo(gomega.HaveOccurred())
}

func (factory *Factory) ensureRBACSetupExists(namespace string) {
	gomega.Expect(factory.CreateIfAbsent(&corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      foundationdbServiceAccount,
			Labels:    factory.GetDefaultLabels(),
			Namespace: namespace,
		},
	})).ToNot(gomega.HaveOccurred())

	gomega.Expect(factory.CreateIfAbsent(&rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      foundationdbServiceAccount,
			Labels:    factory.GetDefaultLabels(),
			Namespace: namespace,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{
					"",
				},
				Resources: []string{
					"pods",
				},
				Verbs: []string{
					"get",
					"watch",
					"update",
					"patch",
				},
			},
		},
	})).ToNot(gomega.HaveOccurred())

	gomega.Expect(factory.CreateIfAbsent(&rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      foundationdbServiceAccount,
			Labels:    factory.GetDefaultLabels(),
			Namespace: namespace,
		},
		RoleRef: rbacv1.RoleRef{
			Name:     foundationdbServiceAccount,
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind: "ServiceAccount",
				Name: foundationdbServiceAccount,
			},
		},
	})).ToNot(gomega.HaveOccurred())
}
