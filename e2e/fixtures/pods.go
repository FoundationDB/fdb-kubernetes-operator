/*
 * pods.go
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
	"context"
	"fmt"
	"log"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ChooseRandomPod returns a random pod from the provided array, passing through the provided error.
func (factory *Factory) ChooseRandomPod(pods *corev1.PodList) *corev1.Pod {
	items := pods.Items
	if len(items) == 0 {
		return nil
	}
	pickedPod := factory.RandomPickOnePod(pods.Items)

	return &pickedPod
}

// RandomPickPod randomly picks the number of Pods from the slice. If the slice contains less than count Pods, all Pods
// will be returned in a random order. This method will ensure that only a Pod with a non-zero deletion timestamp will be picked.
func (factory *Factory) RandomPickPod(input []corev1.Pod, count int) []corev1.Pod {
	candidates := make([]corev1.Pod, 0, len(input))
	for _, pod := range input {
		// Skip pods that are marked for deletion
		if !pod.DeletionTimestamp.IsZero() {
			log.Println(
				"skipping pod marked for deletion, name:",
				pod.Name,
				"namespace:",
				pod.Namespace,
				"deletionTimestampL",
				pod.DeletionTimestamp.String(),
			)
			continue
		}

		candidates = append(candidates, pod)
	}

	maxPods := count
	if count > len(candidates) {
		log.Println("count", count, "is bigger than the number of Pods provided", len(input))
		maxPods = len(candidates)
	}

	// Generate the randomized result
	ret := make([]corev1.Pod, 0, maxPods)
	perm := factory.randomGenerator.Perm(len(candidates))
	for i := 0; i < maxPods; i++ {
		ret = append(ret, candidates[perm[i]])
	}

	return ret
}

// RandomPickOnePod will pick one Pods randomly from the Pod slice.
func (factory *Factory) RandomPickOnePod(input []corev1.Pod) corev1.Pod {
	// TODO(johscheuer): Handle the case where no pod is available.
	return factory.RandomPickPod(input, 1)[0]
}

// RandomPickCluster randomly picks the number of FdbCluster from the slice. If the slice contains less than count FdbCluster, all FdbCluster
// will be returned in a random order.
func (factory *Factory) RandomPickCluster(input []*FdbCluster, count int) []*FdbCluster {
	ret := make([]*FdbCluster, count)
	perm := factory.randomGenerator.Perm(len(input))

	maxPods := count
	if count > len(input) {
		log.Println("count", count, "is bigger than the number of Pods provided", len(input))
		maxPods = len(input)
	}

	for i := 0; i < maxPods; i++ {
		ret[i] = input[perm[i]]
	}

	return ret
}

// RandomPickOneCluster will pick one FdbCluster randomly from the FdbCluster slice.
func (factory *Factory) RandomPickOneCluster(input []*FdbCluster) *FdbCluster {
	// TODO(johscheuer): Handle the case where no cluster is available.
	return factory.RandomPickCluster(input, 1)[0]
}

// SetFinalizerForPod will set the provided finalizer slice for the Pods
func (factory *Factory) SetFinalizerForPod(pod *corev1.Pod, finalizers []string) {
	if pod == nil {
		return
	}

	controllerClient := factory.GetControllerRuntimeClient()
	gomega.Eventually(func(g gomega.Gomega) bool {
		fetchedPod := &corev1.Pod{}
		g.Expect(controllerClient.Get(context.Background(), client.ObjectKeyFromObject(pod), fetchedPod)).
			NotTo(gomega.HaveOccurred())

		if !equality.Semantic.DeepEqual(finalizers, fetchedPod.Finalizers) {
			fetchedPod.SetFinalizers(finalizers)
			g.Expect(controllerClient.Update(context.Background(), fetchedPod)).
				NotTo(gomega.HaveOccurred())
		}

		g.Expect(fetchedPod.Finalizers).To(gomega.ConsistOf(finalizers))

		return true
	}).WithTimeout(1 * time.Minute).WithPolling(1 * time.Second).Should(gomega.BeTrue())
}

// GetProcessClass returns the Process class of this Pod.
func GetProcessClass(pod corev1.Pod) fdbv1beta2.ProcessClass {
	return fdbv1beta2.ProcessClass(pod.GetLabels()[fdbv1beta2.FDBProcessClassLabel])
}

// GetProcessGroupID returns the Process Group ID class of this Pod.
func GetProcessGroupID(pod corev1.Pod) fdbv1beta2.ProcessGroupID {
	return fdbv1beta2.ProcessGroupID(pod.GetLabels()[fdbv1beta2.FDBProcessGroupIDLabel])
}

// GetPvc returns the PVC name of this Pod.
func GetPvc(pod *corev1.Pod) string {
	for _, vol := range pod.Spec.Volumes {
		if vol.Name == "data" {
			return vol.VolumeSource.PersistentVolumeClaim.ClaimName
		}
	}

	ginkgo.Fail(fmt.Sprintf("no pvc found for %s", pod.Name))
	return ""
}
