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
	"reflect"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/pkg/podmanager"
)

// ReplaceMisconfiguredProcessGroups checks if the cluster has any misconfigured process groups that must be replaced.
func ReplaceMisconfiguredProcessGroups(ctx context.Context, podManager podmanager.PodLifecycleManager, client client.Client, log logr.Logger, cluster *fdbv1beta2.FoundationDBCluster, pvcMap map[fdbv1beta2.ProcessGroupID]corev1.PersistentVolumeClaim, replaceOnSecurityContextChange bool) (bool, error) {
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

		needsRemoval, err := ProcessGroupNeedsRemoval(ctx, podManager, client, log, cluster, processGroup, pvcMap, replaceOnSecurityContextChange)

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
func ProcessGroupNeedsRemoval(ctx context.Context, podManager podmanager.PodLifecycleManager, client client.Client, log logr.Logger, cluster *fdbv1beta2.FoundationDBCluster, processGroup *fdbv1beta2.ProcessGroupStatus, pvcMap map[fdbv1beta2.ProcessGroupID]corev1.PersistentVolumeClaim, replaceOnSecurityContextChange bool) (bool, error) {
	// TODO(johscheuer): Fix how we fetch the pvc to make better use of the controller runtime cache.
	pvc, hasPVC := pvcMap[processGroup.ProcessGroupID]
	pod, podErr := podManager.GetPod(ctx, client, cluster, processGroup.GetPodName(cluster))
	if hasPVC {
		needsPVCRemoval, err := processGroupNeedsRemovalForPVC(cluster, pvc, log, processGroup)
		if err != nil {
			return false, err
		}

		if needsPVCRemoval && podErr == nil {
			return true, nil
		}
	} else if processGroup.ProcessClass.IsStateful() {
		log.V(1).Info("Could not find PVC for process group ID",
			"processGroupID", processGroup.ProcessGroupID)
	}

	if podErr != nil {
		log.V(1).Info("Could not find Pod for process group ID",
			"processGroupID", processGroup.ProcessGroupID)
		return false, podErr
	}

	return processGroupNeedsRemovalForPod(cluster, pod, processGroup, log, replaceOnSecurityContextChange)
}

func processGroupNeedsRemovalForPVC(cluster *fdbv1beta2.FoundationDBCluster, pvc corev1.PersistentVolumeClaim, log logr.Logger, processGroup *fdbv1beta2.ProcessGroupStatus) (bool, error) {
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

func processGroupNeedsRemovalForPod(cluster *fdbv1beta2.FoundationDBCluster, pod *corev1.Pod, processGroupStatus *fdbv1beta2.ProcessGroupStatus, log logr.Logger, replaceOnSecurityContextChange bool) (bool, error) {
	if pod == nil {
		return false, nil
	}

	logger := log.WithValues("namespace", cluster.Namespace, "cluster", cluster.Name, "processGroupID", processGroupStatus.ProcessGroupID)

	if processGroupStatus.IsMarkedForRemoval() {
		return false, nil
	}

	idNum, err := processGroupStatus.ProcessGroupID.GetIDNumber()
	if err != nil {
		return false, err
	}

	_, desiredProcessGroupID := cluster.GetProcessGroupID(processGroupStatus.ProcessClass, idNum)
	if processGroupStatus.ProcessGroupID != desiredProcessGroupID {
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
	serversPerPod, err := internal.GetServersPerPodForPod(pod, processGroupStatus.ProcessClass)
	if err != nil {
		return false, err
	}

	desiredServersPerPod := cluster.GetDesiredServersPerPod(processGroupStatus.ProcessClass)
	// Replace the process group if the expected servers differ from the desired servers
	if serversPerPod != desiredServersPerPod {
		logger.Info("Replace process group",
			"serversPerPod", serversPerPod,
			"desiredServersPerPod", desiredServersPerPod,
			"reason", fmt.Sprintf("serversPerPod have changes from current: %d to desired: %d", serversPerPod, desiredServersPerPod))
		return true, nil
	}

	expectedNodeSelector := cluster.GetProcessSettings(processGroupStatus.ProcessClass).PodTemplate.Spec.NodeSelector
	if !equality.Semantic.DeepEqual(pod.Spec.NodeSelector, expectedNodeSelector) {
		specHash, err := internal.GetPodSpecHash(cluster, processGroupStatus, nil)
		if err != nil {
			return false, err
		}

		if pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey] != specHash {
			logger.Info("Replace process group",
				"reason", fmt.Sprintf("nodeSelector has changed from %s to %s", pod.Spec.NodeSelector, expectedNodeSelector))
			return true, nil
		}
	}

	if cluster.NeedsReplacement(processGroupStatus) {
		spec, err := internal.GetPodSpec(cluster, processGroupStatus)
		if err != nil {
			return false, err
		}

		specHash, err := internal.GetPodSpecHash(cluster, processGroupStatus, spec)
		if err != nil {
			return false, err
		}

		if pod.ObjectMeta.Annotations[fdbv1beta2.LastSpecKey] != specHash {
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
	}

	if pointer.BoolDeref(cluster.Spec.ReplaceInstancesWhenResourcesChange, false) {
		desiredSpec, err := internal.GetPodSpec(cluster, processGroupStatus)
		if err != nil {
			return false, err
		}

		if resourcesNeedsReplacement(desiredSpec.Containers, pod.Spec.Containers) {
			logger.Info("Replace process group",
				"reason", "Resource requests have changed")
			return true, nil
		}

		if resourcesNeedsReplacement(desiredSpec.InitContainers, pod.Spec.InitContainers) {
			logger.Info("Replace process group",
				"reason", "Resource requests have changed")
			return true, nil
		}
	}

	desiredPod, err := internal.GetPod(cluster, processGroupStatus)
	if err != nil {
		return false, err
	}
	// Some k8s instances have security context vetting which may edit the spec automatically.
	// This would cause changes to security context on a pod or container
	// to constantly be seen as having a security context change, hence we want to feature guard this.
	// https://kubernetes.io/blog/2021/04/06/podsecuritypolicy-deprecation-past-present-and-future/
	if replaceOnSecurityContextChange {
		return fileSecurityContextChanged(desiredPod, pod, log), nil
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
// See https://github.com/FoundationDB/fdb-kubernetes-operator/issues/208 for motivation
// only makes sense if both pods have containers with matching names
func fileSecurityContextChanged(desired, current *corev1.Pod, log logr.Logger) bool {
	// first check for FSGroup or FSGroupChangePolicy changes as that cannot be overridden at container level
	// (if pod security context is identical, skip these checks)
	if (desired.Spec.SecurityContext != nil || current.Spec.SecurityContext != nil) &&
		!equality.Semantic.DeepEqualWithNilDifferentFromEmpty(desired.Spec.SecurityContext, current.Spec.SecurityContext) {
		if desired.Spec.SecurityContext == nil { // check if changed non-nil -> nil
			if current.Spec.SecurityContext.FSGroup != nil || current.Spec.SecurityContext.FSGroupChangePolicy != nil {
				log.Info("Replace process group",
					"reason", "either FSGroup or FSGroupChangePolicy have changed from defined to undefined (nil) on pod SecurityContext")
				return true
			}
		} else if current.Spec.SecurityContext == nil { // check if changed nil -> non-nil
			if desired.Spec.SecurityContext.FSGroup != nil || desired.Spec.SecurityContext.FSGroupChangePolicy != nil {
				log.Info("Replace process group",
					"reason", "either FSGroup or FSGroupChangePolicy are newly defined on pod SecurityContext")
				return true
			}
		} else { // both pod security contexts are defined so check they are the same
			if !equality.Semantic.DeepEqualWithNilDifferentFromEmpty(desired.Spec.SecurityContext.FSGroup, current.Spec.SecurityContext.FSGroup) ||
				!equality.Semantic.DeepEqualWithNilDifferentFromEmpty(desired.Spec.SecurityContext.FSGroupChangePolicy, current.Spec.SecurityContext.FSGroupChangePolicy) {
				log.Info("Replace process group",
					"reason", "either FSGroup or FSGroupChangePolicy has changed for the pod SecurityContext")
				return true
			}
		}
	}
	// check for RunAsUser and RunAsGroup changes (have to check with container settings, since that can override pod settings)
	for _, desiredContainer := range desired.Spec.Containers {
		for _, currentContainer := range current.Spec.Containers {
			if desiredContainer.Name == currentContainer.Name {
				currentFields := getEffectiveFileSecurityContext(current.Spec.SecurityContext, currentContainer.SecurityContext)
				desiredFields := getEffectiveFileSecurityContext(desired.Spec.SecurityContext, desiredContainer.SecurityContext)
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
