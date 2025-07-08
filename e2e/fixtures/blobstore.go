/*
 * blobstore.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2018-2024 Apple Inc. and the FoundationDB project authors
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
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"text/template"
	"time"

	"github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	seaweedFSName       = "seaweedfs"
	seaweedFSDeployment = `apiVersion: v1
kind: Service
metadata:
  name: seaweedfs
  namespace: {{ .Namespace }}
spec:
  type: ClusterIP
  ports:
    - port: 8333
      targetPort: 8333
      protocol: TCP
  selector:
    app: seaweedfs
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: seaweedfs
  namespace: {{ .Namespace }}
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 50Gi
---
apiVersion: v1
kind: Secret
type: Opaque
metadata:
  name: seaweedfs-s3-secret
  namespace: {{ .Namespace }}
stringData:
  admin_access_key_id: "seaweedfs"
  admin_secret_access_key: "tot4llys3cure"
  seaweedfs_s3_config: |
    {
      "identities": [
        {
          "name": "anvAdmin",
          "credentials": [
            {
              "accessKey": "seaweedfs",
              "secretKey": "tot4llys3cure"
            }
          ],
          "actions": [
            "Admin",
            "Read",
            "List",
            "Tagging",
            "Write"
          ]
        }
      ]
    }

---
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app: seaweedfs
  name: seaweedfs
  namespace: {{ .Namespace }}
spec:
  replicas: 1
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app: seaweedfs
  template:
    metadata:
      labels:
        app: seaweedfs
    spec:
      containers:
        - image: {{ .Image }}
          imagePullPolicy: Always
          args:
            - server
            - -dir=/data
            - -s3
            - -s3.config=/etc/sw/seaweedfs_s3_config
          name: manager
          ports:
            - containerPort: 8333
              name: seaweedfs
          resources:
            limits:
              cpu: "2"
              memory: 4Gi
            requests:
              cpu: "1"
              memory: 2Gi
          securityContext:
            allowPrivilegeEscalation: false
            privileged: false
            readOnlyRootFilesystem: true
          volumeMounts:
            - mountPath: /data
              name: data
            - mountPath: /tmp
              name: tmp
            - mountPath: /etc/sw
              name: users-config
              readOnly: true
      terminationGracePeriodSeconds: 10
      volumes:
        - name: data
          persistentVolumeClaim:
            claimName: seaweedfs
        - name: tmp
          emptyDir: {}
        - name: users-config
          secret:
            defaultMode: 420
            secretName: seaweedfs-s3-secret
`
)

// blobstoreConfig represents the configuration of the blobstore deployment.
type blobstoreConfig struct {
	// Image represents the seaweedfs image that should be used in the Deployment.
	Image string
	// Namespace represents the namespace for the deployment and all associated resources
	Namespace string
}

// CreateBlobstoreIfAbsent creates the blobstore Deployment based on the template.
func (factory *Factory) CreateBlobstoreIfAbsent(namespace string) {
	seaweedFSDeploymentTemplate, err := template.New("seaweedFSDeployment").
		Parse(seaweedFSDeployment)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	buf := bytes.Buffer{}
	gomega.Expect(seaweedFSDeploymentTemplate.Execute(&buf, &blobstoreConfig{
		Image:     prependRegistry(factory.options.registry, factory.options.seaweedFSImage),
		Namespace: namespace,
	})).NotTo(gomega.HaveOccurred())
	decoder := yamlutil.NewYAMLOrJSONDecoder(&buf, 100000)

	for {
		var rawObj runtime.RawExtension
		err := decoder.Decode(&rawObj)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}

		obj, _, err := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme).
			Decode(rawObj.Raw, nil, nil)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		unstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		unstructuredObj := &unstructured.Unstructured{Object: unstructuredMap}

		gomega.Expect(
			factory.CreateIfAbsent(unstructuredObj),
		).NotTo(gomega.HaveOccurred())
	}

	// Make sure the blobstore Pods are running before moving forward.
	factory.waitUntilBlobstorePodsRunning(namespace)
}

// waitUntilBlobstorePodsRunning waits until the blobstore Pods are running.
func (factory *Factory) waitUntilBlobstorePodsRunning(namespace string) {
	deployment := &appsv1.Deployment{}
	gomega.Expect(
		factory.GetControllerRuntimeClient().
			Get(context.Background(), client.ObjectKey{Name: seaweedFSName, Namespace: namespace}, deployment),
	).NotTo(gomega.HaveOccurred())

	expectedReplicas := int(pointer.Int32Deref(deployment.Spec.Replicas, 1))
	gomega.Eventually(func(g gomega.Gomega) int {
		pods := factory.getBlobstorePods(namespace)
		var runningReplicas int
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning && pod.DeletionTimestamp.IsZero() {
				runningReplicas++
				continue
			}

			// If the Pod is not running after 120 seconds we delete it and let the Deployment controller create a new Pod.
			if time.Since(pod.CreationTimestamp.Time).Seconds() > 120.0 {
				log.Println(
					"seaweedfs Pod",
					pod.Name,
					"not running after 120 seconds, going to delete this Pod, status:",
					pod.Status,
				)
				err := factory.GetControllerRuntimeClient().Delete(context.Background(), &pod)
				if k8serrors.IsNotFound(err) {
					continue
				}

				g.Expect(err).NotTo(gomega.HaveOccurred())
			}
		}

		return runningReplicas
	}).WithTimeout(10 * time.Minute).WithPolling(2 * time.Second).Should(gomega.BeNumerically(">=", expectedReplicas))
}

// getBlobstorePods returns the blobstore Pods in the provided namespace.
func (factory *Factory) getBlobstorePods(namespace string) *corev1.PodList {
	pods := &corev1.PodList{}
	gomega.Eventually(func() error {
		return factory.GetControllerRuntimeClient().
			List(context.Background(), pods, client.InNamespace(namespace), client.MatchingLabels(map[string]string{"app": seaweedFSName}))
	}).WithTimeout(1 * time.Minute).WithPolling(1 * time.Second).ShouldNot(gomega.HaveOccurred())

	return pods
}
