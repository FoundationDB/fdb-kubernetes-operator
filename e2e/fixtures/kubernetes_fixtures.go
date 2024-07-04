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
	"fmt"
	"log"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	namespaceRegEx          = `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	testSuiteNameAnnotation = "foundationdb.org/test-suite"
)

// factory.getRandomizedNamespaceName() checks if the username is valid to be used in the namespace name. If so this
// method will return the namespace name as the username a hyphen and 8 random chars.
func (factory *Factory) getRandomizedNamespaceName() string {
	gomega.Expect(factory.userName).To(gomega.MatchRegexp(namespaceRegEx), "user name contains invalid characters")
	return factory.userName + "-" + factory.RandStringRunes(8)
}

// MultipleNamespaces creates multiple namespaces for HA testing.
func (factory *Factory) MultipleNamespaces(config *ClusterConfig, dcIDs []string) []string {
	// If a namespace is provided in the config we will use this name as prefix.
	if config.Namespace != "" {
		factory.namespace = config.Namespace
	} else if factory.namespace == "" {
		// If not namespace is provided in the config or per command line, we will generate a random name.
		factory.namespace = factory.getRandomizedNamespaceName()
	}

	factory.namespaces = make([]string, len(dcIDs))
	for idx, dcID := range dcIDs {
		factory.namespaces[idx] = factory.createNamespace(dcID)
	}

	return factory.namespaces
}

// SingleNamespace returns a single namespace.
func (factory *Factory) SingleNamespace() string {
	if len(factory.namespaces) > 0 {
		return factory.namespaces[0]
	}

	namespace := factory.createNamespace("")
	if len(factory.namespaces) == 0 {
		factory.namespaces = append(factory.namespaces, namespace)
	}

	return namespace
}

func (factory *Factory) createNamespace(suffix string) string {
	var namespace string
	gomega.Eventually(func(g gomega.Gomega) error {
		namespace = factory.namespace

		if namespace == "" {
			namespace = factory.getRandomizedNamespaceName()
		}

		if suffix != "" {
			namespace = namespace + "-" + suffix
		}

		err := factory.checkIfNamespaceIsTerminating(namespace)
		if err != nil {
			return err
		}

		err = factory.ensureNamespaceExists(namespace)
		if err != nil {
			return err
		}

		log.Println("created namespace", namespace)
		namespaceResource := &corev1.Namespace{}
		err = factory.controllerRuntimeClient.Get(ctx.Background(), client.ObjectKey{Namespace: "", Name: namespace}, namespaceResource)
		if err != nil {
			return err
		}

		if namespaceResource.Annotations[testSuiteNameAnnotation] != testSuiteName {
			err = fmt.Errorf("namespace %s already in use by test suite: %s, current test suite: %s", namespace, namespaceResource.Annotations[testSuiteNameAnnotation], testSuiteName)
			log.Println(err.Error())
			return err
		}

		return nil
	}).WithTimeout(10 * time.Minute).WithPolling(1 * time.Second).ShouldNot(gomega.HaveOccurred())

	secret := factory.getCertificate()
	secret.SetNamespace(namespace)
	secret.SetResourceVersion("")
	gomega.Expect(factory.CreateIfAbsent(secret)).NotTo(gomega.HaveOccurred())

	factory.ensureRBACSetupExists(namespace)
	gomega.Expect(factory.ensureFDBOperatorExists(namespace)).ToNot(gomega.HaveOccurred())
	log.Printf("using namespace %s for testing", namespace)
	factory.AddShutdownHook(func() error {
		log.Printf("finished all tests, start deleting namespace %s\n", namespace)

		gomega.Eventually(func() error {
			podList := &corev1.PodList{}
			err := factory.controllerRuntimeClient.List(ctx.Background(), podList, client.InNamespace(namespace))
			if err != nil {
				return err
			}

			for _, pod := range podList.Items {
				if len(pod.Finalizers) > 0 {
					log.Printf("Removing finalizer from Pod %s/%s\n", namespace, pod.Name)
					factory.SetFinalizerForPod(&pod, []string{})
				}
			}

			return nil
		}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).ShouldNot(gomega.HaveOccurred())

		factory.Delete(&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		})

		return nil
	})

	return namespace
}

// checkIfNamespaceIsTerminating will check if the namespace has a deletionTimestamp set. If so this method will wait
// up to 5 minutes until the namespace is deleted to prevent race conditions.
func (factory *Factory) checkIfNamespaceIsTerminating(name string) error {
	controllerClient := factory.GetControllerRuntimeClient()

	namespace := &corev1.Namespace{}
	err := controllerClient.Get(ctx.Background(), client.ObjectKey{Name: name}, namespace)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}

		return err
	}

	namespaceTestSuiteName := namespace.Annotations[testSuiteNameAnnotation]
	// Don't touch the namespace of another test suite.
	if namespaceTestSuiteName != testSuiteName {
		return nil
	}

	deletionTimestamp := namespace.ObjectMeta.DeletionTimestamp
	// No deletionTimestamp is set, so we can assume the namespace is still running.
	if deletionTimestamp == nil {
		return nil
	}

	// If the namespace is in terminating, we have to check if any pod are stuck in terminating with a finalizer set.
	log.Printf("Namespace: %s is in terminating state since: %s will wait until the namespace is deleted", namespace.Name, deletionTimestamp.String())
	podList := &corev1.PodList{}
	err = controllerClient.List(ctx.Background(), podList, client.InNamespace(name))
	if err != nil {
		return err
	}

	for _, pod := range podList.Items {
		if len(pod.Finalizers) > 0 {
			log.Printf("Removing finalizer from Pod %s/%s\n", namespace, pod.Name)
			factory.SetFinalizerForPod(&pod, []string{})
		}
	}

	return fmt.Errorf("namespace %s is still in terminating state since %s", name, deletionTimestamp.String())
}

func (factory *Factory) ensureNamespaceExists(namespace string) error {
	return factory.CreateIfAbsent(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: factory.GetDefaultLabels(),
			Annotations: map[string]string{
				testSuiteNameAnnotation: testSuiteName,
			},
		},
	})
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
