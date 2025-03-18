/*
 * replacements.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2021 Apple Inc. and the FoundationDB project authors
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

package replacements

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"reflect"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/podmanager"
)

// ReplaceMisconfiguredProcessGroups checks if the cluster has any misconfigured process groups that must be replaced.
func ReplaceMisconfiguredProcessGroups(ctx context.Context, podManager podmanager.PodLifecycleManager, ctrlClient client.Client, log logr.Logger, cluster *fdbv1beta2.FoundationDBCluster, replaceOnSecurityContextChange bool) (bool, error) {
	hasReplacements := false

	maxReplacements, _ := getReplacementInformation(cluster, cluster.GetMaxConcurrentReplacements())
	for _, processGroup := range cluster.Status.ProcessGroups {
		if maxReplacements <= 0 {
			log.Info("Early abort, reached limit of concurrent replacements")
			break
		}

		if processGroup.IsMarkedForRemoval() {
			continue
		}

		needsRemoval, err := ProcessGroupNeedsRemoval(ctx, podManager, ctrlClient, log, cluster, processGroup, replaceOnSecurityContextChange)

		// Do not mark for removal if there is an error
		if err != nil {
			continue
		}

		if needsRemoval {
			processGroup.MarkForRemoval()
			hasReplacements = true
			maxReplacements--
		}
	}

	return hasReplacements, nil
}

// ProcessGroupNeedsRemoval checks if a process group needs to be removed.
func ProcessGroupNeedsRemoval(ctx context.Context, podManager podmanager.PodLifecycleManager, ctrlClient client.Client, log logr.Logger, cluster *fdbv1beta2.FoundationDBCluster, processGroup *fdbv1beta2.ProcessGroupStatus, replaceOnSecurityContextChange bool) (bool, error) {
	pod, podErr := podManager.GetPod(ctx, ctrlClient, cluster, processGroup.GetPodName(cluster))
	if processGroup.ProcessClass.IsStateful() {
		pvc := &corev1.PersistentVolumeClaim{}
		err := ctrlClient.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: processGroup.GetPvcName(cluster)}, pvc)
		if err != nil {
			if k8serrors.IsNotFound(err) {
				log.V(1).Info("Could not find PVC for process group ID",
					"processGroupID", processGroup.ProcessGroupID)
			}
		} else {
			needsPVCRemoval, err := processGroupNeedsRemovalForPVC(cluster, pvc, log, processGroup)
			if err != nil {
				return false, err
			}

			if needsPVCRemoval && podErr == nil {
				return true, nil
			}
		}
	}

	if podErr != nil {
		log.V(1).Info("Could not find Pod for process group ID",
			"processGroupID", processGroup.ProcessGroupID)
		return false, podErr
	}

	return processGroupNeedsRemovalForPod(cluster, pod, processGroup, log, replaceOnSecurityContextChange)
}

func processGroupNeedsRemovalForPVC(cluster *fdbv1beta2.FoundationDBCluster, pvc *corev1.PersistentVolumeClaim, log logr.Logger, processGroup *fdbv1beta2.ProcessGroupStatus) (bool, error) {
	processGroupID := internal.GetProcessGroupIDFromMeta(cluster, pvc.ObjectMeta)
	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "pvc", pvc.Name, "processGroupID", processGroupID)

	ownedByCluster := !cluster.ShouldFilterOnOwnerReferences()
	if !ownedByCluster {
		for _, ownerReference := range pvc.OwnerReferences {
			if ownerReference.UID == cluster.UID {
				ownedByCluster = true
				break
			}
		}
	}
	if !ownedByCluster {
		logger.Info("Ignoring PVC that is not owned by the cluster")
		return false, nil
	}

	desiredPVC, err := internal.GetPvc(cluster, processGroup)
	if err != nil {
		return false, err
	}
	pvcHash, err := internal.GetJSONHash(desiredPVC.Spec)
	if err != nil {
		return false, err
	}

	if pvc.Annotations[fdbv1beta2.LastSpecKey] != pvcHash {
		logger.Info("Replace process group",
			"reason", fmt.Sprintf("PVC spec has changed from %s to %s", pvcHash, pvc.Annotations[fdbv1beta2.LastSpecKey]))
		return true, nil
	}
	if pvc.Name != desiredPVC.Name {
		logger.Info("Replace process group",
			"reason", fmt.Sprintf("PVC name has changed from %s to %s", desiredPVC.Name, pvc.Name))
		return true, nil
	}

	return false, nil
}

func processGroupNeedsRemovalForPod(cluster *fdbv1beta2.FoundationDBCluster, pod *corev1.Pod, processGroup *fdbv1beta2.ProcessGroupStatus, log logr.Logger, replaceOnSecurityContextChange bool) (bool, error) {
	if pod == nil {
		return false, nil
	}

	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "processGroupID", processGroup.ProcessGroupID)

	if processGroup.IsMarkedForRemoval() {
		return false, nil
	}

	idNum, err := processGroup.ProcessGroupID.GetIDNumber()
	if err != nil {
		return false, err
	}

	_, desiredProcessGroupID := cluster.GetProcessGroupID(processGroup.ProcessClass, idNum)
	if processGroup.ProcessGroupID != desiredProcessGroupID {
		logger.Info("Replace process group",
			"reason", fmt.Sprintf("expect process group ID: %s", desiredProcessGroupID))
		return true, nil
	}

	ipSource, err := internal.GetPublicIPSource(pod)
	if err != nil {
		return false, err
	}
	if ipSource != cluster.GetPublicIPSource() {
		logger.Info("Replace process group",
			"reason", fmt.Sprintf("publicIP source has changed from %s to %s", ipSource, cluster.GetPublicIPSource()))
		return true, nil
	}
	serversPerPod, err := internal.GetServersPerPodForPod(pod, processGroup.ProcessClass)
	if err != nil {
		return false, err
	}

	desiredServersPerPod := cluster.GetDesiredServersPerPod(processGroup.ProcessClass)
	// Replace the process group if the expected servers differ from the desired servers
	if serversPerPod != desiredServersPerPod {
		logger.Info("Replace process group",
			"serversPerPod", serversPerPod,
			"desiredServersPerPod", desiredServersPerPod,
			"reason", fmt.Sprintf("serversPerPod has changed from current: %d to desired: %d", serversPerPod, desiredServersPerPod))
		return true, nil
	}

	spec, err := internal.GetPodSpec(cluster, processGroup)
	if err != nil {
		return false, err
	}
	specHash, err := internal.GetPodSpecHash(cluster, processGroup, spec)
	if err != nil {
		return false, err
	}

	if pointer.BoolDeref(cluster.Spec.ReplaceInstancesWhenResourcesChange, false) {
		if resourcesNeedsReplacement(spec.Containers, pod.Spec.Containers) {
			logger.Info("Replace process group",
				"reason", "Resource requests have changed")
			return true, nil
		}

		if resourcesNeedsReplacement(spec.InitContainers, pod.Spec.InitContainers) {
			logger.Info("Replace process group",
				"reason", "Resource requests have changed")
			return true, nil
		}
	}

	if pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey] == specHash {
		return false, nil
	}

	expectedNodeSelector := cluster.GetProcessSettings(processGroup.ProcessClass).PodTemplate.Spec.NodeSelector
	if !equality.Semantic.DeepEqual(pod.Spec.NodeSelector, expectedNodeSelector) {
		logger.Info("Replace process group",
			"reason", fmt.Sprintf("nodeSelector has changed from %s to %s", pod.Spec.NodeSelector, expectedNodeSelector))
		return true, nil
	}

	// If the image type is changed from split to unified and only a single storage server per pod is used, we have to perform
	// a replacement as the disk layout has changed.
	if cluster.GetStorageServersPerPod() == 1 && internal.GetImageType(pod) != cluster.DesiredImageType() {
		logger.Info("Replace process group",
			"reason", "imageType has been changed and only a single storage server per Pod is used")
		return true, nil
	}

	podIPFamily, err := internal.GetIPFamily(pod)
	if err != nil {
		return false, err
	}

	if !equality.Semantic.DeepEqual(cluster.Spec.Routing.PodIPFamily, podIPFamily) {
		logger.Info("Replace process group",
			"reason", "pod IP family has changed",
			"currentPodIPFamily", podIPFamily,
			"desiredPodIPFamily", cluster.Spec.Routing.PodIPFamily,
		)

		return true, nil
	}

	if cluster.NeedsReplacement(processGroup) {
		jsonSpec, err := json.Marshal(spec)
		if err != nil {
			return false, err
		}

		logger.Info("Replace process group",
			"reason", "specHash has changed",
			"desiredSpecHash", specHash,
			"currentSpecHash", pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey],
			"desiredSpec", base64.StdEncoding.EncodeToString(jsonSpec),
		)
		return true, nil
	}

	// Some k8s instances have security context vetting which may edit the spec automatically.
	// This would cause changes to security context on a pod or container
	// to constantly be seen as having a security context change, hence we want to feature guard this
	// and also guard on the spec hash below
	// https://kubernetes.io/blog/2021/04/06/podsecuritypolicy-deprecation-past-present-and-future/
	if replaceOnSecurityContextChange {
		return fileSecurityContextChanged(spec, &pod.Spec, logger), nil
	}

	return false, nil
}

func resourcesNeedsReplacement(desired []corev1.Container, current []corev1.Container) bool {
	// We only care about requests since limits are ignored during scheduling
	desiredCPURequests, desiredMemoryRequests := getCPUandMemoryRequests(desired)
	currentCPURequests, currentMemoryRequests := getCPUandMemoryRequests(current)

	return desiredCPURequests.Cmp(*currentCPURequests) == 1 || desiredMemoryRequests.Cmp(*currentMemoryRequests) == 1
}

func getCPUandMemoryRequests(containers []corev1.Container) (*resource.Quantity, *resource.Quantity) {
	cpuRequests := &resource.Quantity{}
	memoryRequests := &resource.Quantity{}

	for _, container := range containers {
		cpu := container.Resources.Requests.Cpu()

		if cpu != nil {
			cpuRequests.Add(*cpu)
		}

		memory := container.Resources.Requests.Memory()

		if memory != nil {
			memoryRequests.Add(*memory)
		}
	}

	return cpuRequests, memoryRequests
}

type containerFileSecurityContext struct {
	runAsUser, runAsGroup *int64
}

// fileSecurityContextChanged checks for changes in the effective security context by checking that there are no changes
// to the following SecurityContext (or PodSecurityContext) fields:
// RunAsGroup, RunAsUser, FSGroup, or FSGroupChangePolicy
// See https://github.com/FoundationDB/fdb-kubernetes-operator/v2/issues/208 for motivation
// only makes sense if both pods have containers with matching names
func fileSecurityContextChanged(desired, current *corev1.PodSpec, log logr.Logger) bool {
	// first check for FSGroup or FSGroupChangePolicy changes as that cannot be overridden at container level
	// (if pod security context is identical, skip these checks)
	if (desired.SecurityContext != nil || current.SecurityContext != nil) &&
		!equality.Semantic.DeepEqualWithNilDifferentFromEmpty(desired.SecurityContext, current.SecurityContext) {
		if desired.SecurityContext == nil { // check if changed non-nil -> nil
			if current.SecurityContext.FSGroup != nil || current.SecurityContext.FSGroupChangePolicy != nil {
				log.Info("Replace process group",
					"reason", "either FSGroup or FSGroupChangePolicy have changed from defined to undefined (nil) on pod SecurityContext")
				return true
			}
		} else if current.SecurityContext == nil { // check if changed nil -> non-nil
			if desired.SecurityContext.FSGroup != nil || desired.SecurityContext.FSGroupChangePolicy != nil {
				log.Info("Replace process group",
					"reason", "either FSGroup or FSGroupChangePolicy are newly defined on pod SecurityContext")
				return true
			}
		} else { // both pod security contexts are defined so check they are the same
			if !equality.Semantic.DeepEqualWithNilDifferentFromEmpty(desired.SecurityContext.FSGroup, current.SecurityContext.FSGroup) ||
				!equality.Semantic.DeepEqualWithNilDifferentFromEmpty(desired.SecurityContext.FSGroupChangePolicy, current.SecurityContext.FSGroupChangePolicy) {
				log.Info("Replace process group",
					"reason", "either FSGroup or FSGroupChangePolicy has changed for the pod SecurityContext")
				return true
			}
		}
	}
	// check for RunAsUser and RunAsGroup changes (have to check with container settings, since that can override pod settings)
	for _, desiredContainer := range desired.Containers {
		for _, currentContainer := range current.Containers {
			if desiredContainer.Name == currentContainer.Name {
				currentFields := getEffectiveFileSecurityContext(current.SecurityContext, currentContainer.SecurityContext)
				desiredFields := getEffectiveFileSecurityContext(desired.SecurityContext, desiredContainer.SecurityContext)
				if reflect.DeepEqual(desiredFields, currentFields) {
					break
				}
				log.Info("Replace process group",
					"reason", "either RunAsGroup or RunAsUser has changed on the SecurityContext")
				return true
			}
		}
	}
	return false
}

func getEffectiveFileSecurityContext(podSc *corev1.PodSecurityContext, containerSc *corev1.SecurityContext) containerFileSecurityContext {
	if containerSc == nil && podSc == nil {
		return containerFileSecurityContext{}
	}
	if containerSc == nil {
		return containerFileSecurityContext{
			runAsGroup: podSc.RunAsGroup,
			runAsUser:  podSc.RunAsUser,
		}
	}
	// container settings are not nil, so we have to check against the pod ones for defaults (or use them if the pod settings are nil)
	fileSc := containerFileSecurityContext{
		runAsGroup: containerSc.RunAsGroup,
		runAsUser:  containerSc.RunAsUser,
	}
	if podSc == nil {
		return fileSc // this is currently the container security context
	}
	if fileSc.runAsGroup == nil {
		fileSc.runAsGroup = podSc.RunAsGroup
	}
	if fileSc.runAsUser == nil {
		fileSc.runAsUser = podSc.RunAsUser
	}
	return fileSc
}
