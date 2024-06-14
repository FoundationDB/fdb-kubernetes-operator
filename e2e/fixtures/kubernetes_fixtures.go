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
	"log"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	namespaceRegEx = `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
)

// factory.getRandomizedNamespaceName() checks if the username is valid to be used in the namespace name. If so this
// method will return the namespace name as the username a hyphen and 8 random chars.
func (factory *Factory) getRandomizedNamespaceName() string {
	gomega.Expect(factory.singleton.userName).To(gomega.MatchRegexp(namespaceRegEx), "user name contains invalid characters")
	return factory.singleton.userName + "-" + RandStringRunes(8)
}

// MultipleNamespaces creates multiple namespaces for HA testing.
func (factory *Factory) MultipleNamespaces(config *ClusterConfig, dcIDs []string) []string {
	// If a namespace is provided in the config we will use this name as prefix.
	if config.Namespace != "" {
		factory.options.namespace = config.Namespace
	} else {
		factory.options.namespace = factory.getRandomizedNamespaceName()
	}

	res := make([]string, len(dcIDs))
	for idx, dcID := range dcIDs {
		namespace := factory.createNamespace(dcID)
		log.Println("Create namespace", namespace)
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
		namespace = factory.getRandomizedNamespaceName()
	}

	if suffix != "" {
		namespace = namespace + "-" + suffix
	}

	factory.ensureNamespaceExists(namespace)
	factory.ensureRBACSetupExists(namespace)
	gomega.Expect(factory.ensureFDBOperatorExists(namespace)).ToNot(gomega.HaveOccurred())
	log.Printf("using namespace %s for testing", namespace)
	factory.AddShutdownHook(func() error {
		log.Printf("finished all tests, start deleting namespace %s\n", namespace)
		factory.Delete(&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		})

		return nil
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
					"list",
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
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind: rbacv1.ServiceAccountKind,
				Name: foundationdbServiceAccount,
			},
		},
	})).ToNot(gomega.HaveOccurred())

	nodeRoleName := namespace + "-" + foundationdbNodeRole
	gomega.Expect(factory.CreateIfAbsent(&rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodeRoleName,
			Labels:    factory.GetDefaultLabels(),
			Namespace: namespace,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{
					"",
				},
				Resources: []string{
					"nodes",
				},
				Verbs: []string{
					"get",
					"watch",
					"list",
				},
			},
		},
	})).ToNot(gomega.HaveOccurred())

	gomega.Expect(factory.CreateIfAbsent(&rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodeRoleName,
			Labels:    factory.GetDefaultLabels(),
			Namespace: namespace,
		},
		RoleRef: rbacv1.RoleRef{
			Name:     nodeRoleName,
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      foundationdbServiceAccount,
				Namespace: namespace,
			},
		},
	})).ToNot(gomega.HaveOccurred())

	factory.AddShutdownHook(func() error {
		factory.Delete(&rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nodeRoleName,
				Namespace: namespace,
			},
		})

		factory.Delete(&rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nodeRoleName,
				Namespace: namespace,
			},
		})

		return nil
	})
}

// LoadControllerRuntimeFromContext will load a client.Client from the provided context. The context must be existing in the
// kube config.
func LoadControllerRuntimeFromContext(context string, configScheme *runtime.Scheme) (client.Client, error) {
	kubeConfig, err := config.GetConfigWithContext(context)
	if err != nil {
		return nil, err
	}

	return client.New(
		kubeConfig,
		client.Options{Scheme: configScheme},
	)
}
