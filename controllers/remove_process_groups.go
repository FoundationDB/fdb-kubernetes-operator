/*
 * remove_process_groups.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2019-2021 Apple Inc. and the FoundationDB project authors
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

package controllers

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal/buggify"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal/coordination"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/internal/removals"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbadminclient"
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/pkg/fdbstatus"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// removeProcessGroups provides a reconciliation step for removing process groups as part of a
// shrink or replacement.
type removeProcessGroups struct{}

// reconcile runs the reconciler's work.
func (u removeProcessGroups) reconcile(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, status *fdbv1beta2.FoundationDBStatus, logger logr.Logger) *requeue {
	adminClient, err := r.getAdminClient(logger, cluster)
	if err != nil {
		return &requeue{curError: err}
	}
	defer func() {
		_ = adminClient.Close()
	}()

	// If the status is not cached, we have to fetch it.
	if status == nil {
		status, err = adminClient.GetStatus()
		if err != nil {
			return &requeue{curError: err}
		}
	}

	remainingMap, err := removals.GetRemainingMap(logger, adminClient, cluster, status, r.MinimumRecoveryTimeForExclusion)
	if err != nil {
		return &requeue{curError: err}
	}

	coordinators := fdbstatus.GetCoordinatorsFromStatus(status)
	allExcluded, newExclusions, processGroupsToRemove := r.getProcessGroupsToRemove(logger, cluster, remainingMap, coordinators)
	// If no process groups are marked to remove we have to check if all process groups are excluded.
	if len(processGroupsToRemove) == 0 {
		if !allExcluded {
			return &requeue{message: "Reconciliation needs to exclude more processes"}
		}
		return nil
	}

	// Update the cluster to reflect the new exclusions in our status
	if newExclusions {
		err = r.updateOrApply(ctx, cluster)
		if err != nil {
			return &requeue{curError: err}
		}
	}

	// Ensure we only remove process groups that are not blocked to be removed by the buggify config.
	processGroupsToRemove = buggify.FilterBlockedRemovals(cluster, processGroupsToRemove)
	// If all of the process groups are filtered out we can stop doing the next steps.
	if len(processGroupsToRemove) == 0 {
		return nil
	}

	// We don't use the "cached" of the cluster status from the CRD to minimize the window between data loss (e.g. a node
	// or a set of Pods is not reachable anymore). We still end up with the risk to actually query the FDB cluster and after that
	// query the cluster gets into a degraded state.
	// We could be smarter here and only block removals that target stateful processes by e.g. filtering those out of the
	// processGroupsToRemove slice.
	hasDesiredFaultTolerance := fdbstatus.HasDesiredFaultToleranceFromStatus(logger, status, cluster)
	if !hasDesiredFaultTolerance {
		return &requeue{
			message: "Removals cannot proceed because cluster has degraded fault tolerance",
			delay:   30 * time.Second,
		}
	}

	// In addition to that we should add the same logic as in the exclude step
	// to ensure we never exclude/remove more process groups than desired.
	zonedRemovals, lastDeletion, err := removals.GetZonedRemovals(processGroupsToRemove)
	if err != nil {
		return &requeue{curError: err}
	}

	// If the operator is allowed to delete all process groups at the same time we don't enforce any safety checks.
	if cluster.GetRemovalMode() != fdbv1beta2.PodUpdateModeAll {
		// To ensure we are not deletion zones faster than Kubernetes actually removes Pods we are adding a wait time
		// if we have resources in the terminating state. We will only block if the terminating state was recently (in the
		// last minute).
		waitTime, allowed := removals.RemovalAllowed(lastDeletion, time.Now().Unix(), cluster.GetWaitBetweenRemovalsSeconds())
		if !allowed {
			return &requeue{message: fmt.Sprintf("not allowed to remove process groups, waiting: %vs", waitTime), delay: time.Duration(waitTime) * time.Second}
		}
	}

	zone, zoneRemovals, err := removals.GetProcessGroupsToRemove(cluster.GetRemovalMode(), zonedRemovals)
	if err != nil {
		return &requeue{curError: err}
	}

	logger.Info("Removing process groups", "zone", zone, "count", len(zoneRemovals), "deletionMode", cluster.GetRemovalMode())
	// This will return a map of the newly removed ProcessGroups and the ProcessGroups with the ResourcesTerminating condition
	removedProcessGroups := r.removeProcessGroups(ctx, logger, cluster, zoneRemovals, zonedRemovals[removals.TerminatingZone])
	err = includeProcessGroup(ctx, logger, r, cluster, removedProcessGroups, status, adminClient)
	if err != nil {
		// If the inclusion is blocked or another issues happened we will retry in 60 seconds.
		return &requeue{curError: err, delayedRequeue: true, delay: 60 * time.Second}
	}

	return nil
}

func removeProcessGroup(ctx context.Context, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, processGroup *fdbv1beta2.ProcessGroupStatus) error {
	podName := processGroup.GetPodName(cluster)
	var deletionError error

	pod, err := r.PodLifecycleManager.GetPod(ctx, r, cluster, podName)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	if err == nil {
		value, ok := pod.Annotations[fdbv1beta2.IsolateProcessGroupAnnotation]
		if ok {
			// Ignore the parsing error here and assume the pod was not isolated.
			isolated, _ := strconv.ParseBool(value)
			if isolated {
				return fmt.Errorf("not allowed to delete Pod and the assosicated resources as the Pod is isolated")
			}
		}

		if pod.DeletionTimestamp.IsZero() {
			err = r.PodLifecycleManager.DeletePod(ctx, r, pod)
			if err != nil {
				deletionError = fmt.Errorf("could not delete Pod: %w", err)
			}
		}
	}

	// TODO(johscheuer): https://github.com/FoundationDB/fdb-kubernetes-operator/v2/issues/1638
	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(ctx, pvcs, internal.GetSinglePodListOptions(cluster, processGroup.ProcessGroupID)...)
	if err != nil {
		return err
	}
	if len(pvcs.Items) == 1 && pvcs.Items[0].DeletionTimestamp.IsZero() {
		logr.FromContextOrDiscard(ctx).Info("Deleting pvc", "name", pvcs.Items[0].Name)
		err = r.Delete(ctx, &pvcs.Items[0])
		if err != nil {
			deletionError = errors.Join(deletionError, fmt.Errorf("could not delete PVC: %w", err))
		}
	} else if len(pvcs.Items) > 1 {
		return fmt.Errorf("multiple PVCs found for cluster %s, processGroupID %s", cluster.Name, processGroup.ProcessGroupID)
	}

	service := &corev1.Service{}
	err = r.Get(ctx, client.ObjectKey{Name: podName, Namespace: cluster.Namespace}, service)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}

	if err == nil && service.DeletionTimestamp.IsZero() {
		err = r.Delete(ctx, service)
		if err != nil {
			deletionError = errors.Join(deletionError, fmt.Errorf("could not delete Service: %w", err))
		}
	}

	return deletionError
}

func confirmRemoval(ctx context.Context, logger logr.Logger, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, processGroup *fdbv1beta2.ProcessGroupStatus) (bool, error) {
	canBeIncluded := true

	podName := processGroup.GetPodName(cluster)
	pod, err := r.PodLifecycleManager.GetPod(ctx, r, cluster, podName)
	// If we get an error different from not found we will return the error.
	if err != nil && !k8serrors.IsNotFound(err) {
		return false, err
	}

	// The Pod resource still exists, so we have to validate the deletion timestamp.
	if err == nil {
		if pod.DeletionTimestamp.IsZero() {
			logger.Info("Waiting for process group to get torn down", "processGroupID", processGroup.ProcessGroupID, "pod", podName)
			return false, internal.ResourceNotDeleted{Resource: pod}
		}

		// Pod is in terminating state so we don't want to block, but we also don't want to include it
		canBeIncluded = false
	}

	pvc := &corev1.PersistentVolumeClaim{}
	err = r.Get(ctx, client.ObjectKey{Name: processGroup.GetPvcName(cluster), Namespace: cluster.Namespace}, pvc)
	if err != nil && !k8serrors.IsNotFound(err) {
		return false, err
	}

	if err == nil {
		if pvc.DeletionTimestamp.IsZero() {
			logger.Info("Waiting for process group to get torn down", "processGroupID", processGroup.ProcessGroupID, "pod", podName)
			return false, internal.ResourceNotDeleted{Resource: pod}
		}

		// PVC is in terminating state so we don't want to block, but we also don't want to include it
		canBeIncluded = false
	}

	service := &corev1.Service{}
	err = r.Get(ctx, client.ObjectKey{Name: podName, Namespace: cluster.Namespace}, service)
	// If we get an error different from not found we will return the error.
	if err != nil && !k8serrors.IsNotFound(err) {
		return false, err
	}

	// The Pod resource still exists, so we have to validate the deletion timestamp.
	if err == nil {
		if service.DeletionTimestamp.IsZero() {
			logger.Info("Waiting for process group to get torn down", "processGroupID", processGroup.ProcessGroupID, "service", podName)
			return false, internal.ResourceNotDeleted{Resource: service}
		}

		// Service is in terminating state so we don't want to block, but we also don't want to include it
		canBeIncluded = false
	}

	return canBeIncluded, nil
}

func includeProcessGroup(ctx context.Context, logger logr.Logger, r *FoundationDBClusterReconciler, cluster *fdbv1beta2.FoundationDBCluster, removedProcessGroups map[fdbv1beta2.ProcessGroupID]bool, status *fdbv1beta2.FoundationDBStatus, adminClient fdbadminclient.AdminClient) error {
	// Update here for ready inclusion --> Check here
	var readyForInclusion map[fdbv1beta2.ProcessGroupID]time.Time
	readyForInclusionUpdates := map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction{}
	if cluster.GetSynchronizationMode() == fdbv1beta2.SynchronizationModeGlobal {
		var err error
		readyForInclusion, err = adminClient.GetReadyForInclusion(cluster.Spec.ProcessGroupIDPrefix)
		if err != nil {
			return err
		}
	}

	fdbProcessesToInclude, newProcessGroups, err := getProcessesToInclude(logger, cluster, removedProcessGroups, status, readyForInclusion, readyForInclusionUpdates)
	if err != nil {
		return err
	}

	if cluster.GetSynchronizationMode() == fdbv1beta2.SynchronizationModeGlobal {
		err = adminClient.UpdateReadyForInclusion(readyForInclusionUpdates)
		if err != nil {
			return err
		}
	}

	if len(fdbProcessesToInclude) == 0 {
		// In case that the operator was removing a process group without exclusion.
		// We can update the process groups at this stage, as no other processes must be included.
		if len(cluster.Status.ProcessGroups) != len(newProcessGroups) && len(newProcessGroups) > 0 {
			cluster.Status.ProcessGroups = newProcessGroups
			return r.updateOrApply(ctx, cluster)
		}

		return nil
	}

	// Make sure it's safe to include processes.
	err = fdbstatus.CanSafelyIncludeProcesses(cluster, status, r.MinimumRecoveryTimeForInclusion)
	if err != nil {
		return err
	}

	// Make sure the inclusion are coordinated across multiple operator instances.
	err = r.takeLock(logger, cluster, "include removed process groups")
	if err != nil {
		return err
	}

	if cluster.GetSynchronizationMode() == fdbv1beta2.SynchronizationModeGlobal {
		pendingForInclusion, err := adminClient.GetPendingForInclusion("")
		if err != nil {
			return err
		}

		readyForInclusion, err = adminClient.GetReadyForInclusion("")
		if err != nil {
			return err
		}

		err = coordination.AllProcessesReady(logger, pendingForInclusion, readyForInclusion, r.GlobalSynchronizationWaitDuration)
		if err != nil {
			return err
		}

		// TODO (johscheuer): What if multiple addresses are excluded for a process, e.g. because the pod was recreated during
		// the exclusion and IPs are used for exclusion.
		// Since we are here, we should be able to do a "include all", this would clean up all exclusion entries. Since this operator instance
		// has the lock and all process groups that are marked for removal are ready to be included, this should be a safe option. The only
		// risk would be exclusions that are not done with the operator.
		fdbProcessesToInclude = coordination.GetAddressesFromStatus(logger, status, readyForInclusion, true, true)
	}

	r.Recorder.Event(cluster, corev1.EventTypeNormal, "IncludingProcesses", fmt.Sprintf("Including removed processes: %v", fdbProcessesToInclude))
	err = adminClient.IncludeProcesses(fdbProcessesToInclude)
	if err != nil {
		return err
	}

	// Reset the SecondsSinceLastRecovered since the operator just included some processes, which will cause a recovery.
	status.Cluster.RecoveryState.SecondsSinceLastRecovered = 0.0
	// Update the process group list and remove all removed and included process groups.
	cluster.Status.ProcessGroups = newProcessGroups

	return r.updateOrApply(ctx, cluster)
}

func getProcessesToInclude(logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster, removedProcessGroups map[fdbv1beta2.ProcessGroupID]bool, status *fdbv1beta2.FoundationDBStatus, readyForInclusion map[fdbv1beta2.ProcessGroupID]time.Time, readyForInclusionUpdates map[fdbv1beta2.ProcessGroupID]fdbv1beta2.UpdateAction) ([]fdbv1beta2.ProcessAddress, []*fdbv1beta2.ProcessGroupStatus, error) {
	fdbProcessesToInclude := make([]fdbv1beta2.ProcessAddress, 0)

	if len(removedProcessGroups) == 0 {
		return fdbProcessesToInclude, cluster.Status.ProcessGroups, nil
	}

	excludedServers, err := fdbstatus.GetExclusions(status)
	if err != nil {
		return fdbProcessesToInclude, nil, fmt.Errorf("unable to get excluded servers from status, %w", err)
	}
	excludedServersMap := make(map[string]fdbv1beta2.None, len(excludedServers))
	for _, excludedServer := range excludedServers {
		excludedServersMap[excludedServer.String()] = fdbv1beta2.None{}
	}

	processGroups := cluster.Status.DeepCopy().ProcessGroups
	idx := 0
	for _, processGroup := range processGroups {
		// Tested processes are not excluded, so there is no need to include them.
		if processGroup.ProcessClass == fdbv1beta2.ProcessClassTest {
			continue
		}

		if processGroup.IsMarkedForRemoval() && removedProcessGroups[processGroup.ProcessGroupID] {
			foundInExcludedServerList := false
			exclusionString := processGroup.GetExclusionString()
			if _, ok := excludedServersMap[exclusionString]; ok {
				fdbProcessesToInclude = append(fdbProcessesToInclude, fdbv1beta2.ProcessAddress{StringAddress: exclusionString})
				foundInExcludedServerList = true

				if _, ok := readyForInclusion[processGroup.ProcessGroupID]; !ok {
					readyForInclusionUpdates[processGroup.ProcessGroupID] = fdbv1beta2.UpdateActionAdd
				}
			}

			for _, pAddr := range processGroup.Addresses {
				// Ensure we include the process address if the removed process group is a log process as we are always
				// excluding the log process with locality and IP address when validating that the log process was
				// fully excluded. Otherwise we might leave the IP address in the excluded list.
				if foundInExcludedServerList && processGroup.ProcessClass.IsLogProcess() {
					fdbProcessesToInclude = append(fdbProcessesToInclude, fdbv1beta2.ProcessAddress{IPAddress: net.ParseIP(pAddr)})
					continue
				}

				if _, ok := excludedServersMap[pAddr]; ok {
					fdbProcessesToInclude = append(fdbProcessesToInclude, fdbv1beta2.ProcessAddress{IPAddress: net.ParseIP(pAddr)})
					foundInExcludedServerList = true
					if _, ok := readyForInclusion[processGroup.ProcessGroupID]; !ok {
						readyForInclusionUpdates[processGroup.ProcessGroupID] = fdbv1beta2.UpdateActionAdd
					}
				}
			}

			if !foundInExcludedServerList && !processGroup.ExclusionSkipped {
				// This means that the process is marked for exclusion and is also removed in the previous step but is missing
				// its entry in the excluded servers in the status. This should not throw an error as this will block the
				// inclusion for other processes, but we should have a record of this event happening in the logs.
				logger.Info("processGroup should be included but is missing from excluded server list", "processGroup", processGroup)
			}

			continue
		}

		processGroups[idx] = processGroup
		idx++
	}

	return fdbProcessesToInclude, processGroups[:idx], nil
}

func (r *FoundationDBClusterReconciler) getProcessGroupsToRemove(logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster, remainingMap map[string]bool, cordSet map[string]fdbv1beta2.None) (bool, bool, []*fdbv1beta2.ProcessGroupStatus) {
	allExcluded := true
	newExclusions := false
	processGroupsToRemove := make([]*fdbv1beta2.ProcessGroupStatus, 0, len(cluster.Status.ProcessGroups))
	logger.V(1).Info("Get ProcessGroups to be removed.", "remainingMap", remainingMap)

	for _, processGroup := range cluster.Status.ProcessGroups {
		if !processGroup.IsMarkedForRemoval() {
			continue
		}

		if _, ok := cordSet[string(processGroup.ProcessGroupID)]; ok {
			logger.Info("Block removal of Coordinator", "processGroupID", processGroup.ProcessGroupID)
			allExcluded = false
			continue
		}

		// ProcessGroup is already marked as excluded we can add it to the processGroupsToRemove and skip further checks.
		if processGroup.IsExcluded() {
			processGroupsToRemove = append(processGroupsToRemove, processGroup)
			continue
		}

		excluded, err := processGroup.AllAddressesExcluded(logger, remainingMap)
		if processGroup.ProcessClass != fdbv1beta2.ProcessClassTest && (!excluded || err != nil) {
			logger.Info("Incomplete exclusion still present in removeProcessGroups step", "processGroupID", processGroup.ProcessGroupID, "error", err)
			allExcluded = false
			continue
		}

		logger.Info("Marking exclusion complete", "processGroupID", processGroup.ProcessGroupID, "addresses", processGroup.Addresses)
		processGroup.SetExclude()
		processGroupsToRemove = append(processGroupsToRemove, processGroup)
		newExclusions = true
	}

	return allExcluded, newExclusions, processGroupsToRemove
}

func (r *FoundationDBClusterReconciler) removeProcessGroups(ctx context.Context, logger logr.Logger, cluster *fdbv1beta2.FoundationDBCluster, processGroupsToRemove []*fdbv1beta2.ProcessGroupStatus, terminatingProcessGroups []*fdbv1beta2.ProcessGroupStatus) map[fdbv1beta2.ProcessGroupID]bool {
	processGroupNames := make([]fdbv1beta2.ProcessGroupID, len(processGroupsToRemove))
	for i, processGroup := range processGroupsToRemove {
		processGroupNames[i] = processGroup.ProcessGroupID
	}

	r.Recorder.Event(cluster, corev1.EventTypeNormal, "RemovingProcesses", fmt.Sprintf("Removing process groups: %v", processGroupNames))

	processGroups := append(processGroupsToRemove, terminatingProcessGroups...)
	for _, processGroup := range processGroups {
		err := removeProcessGroup(logr.NewContext(ctx, logger), r, cluster, processGroup)
		if err != nil {
			logger.Error(err, "Error during remove process group", "processGroupID", processGroup.ProcessGroupID)
			continue
		}
	}

	removedProcessGroups := make(map[fdbv1beta2.ProcessGroupID]bool)
	// We have to check if the currently removed process groups are completely removed.
	// In addition, we have to check if one of the terminating process groups has been cleaned up.
	for _, processGroup := range processGroups {
		include, err := confirmRemoval(ctx, logger, r, cluster, processGroup)
		if err != nil && !internal.IsResourceNotDeleted(err) {
			logger.Error(err, "Error during confirm process group removal", "processGroupID", processGroup.ProcessGroupID)
			continue
		}

		// Pods that are stuck in terminating shouldn't block reconciliation, but we also
		// don't want to include them since they have an unknown state.
		removedProcessGroups[processGroup.ProcessGroupID] = include
	}

	return removedProcessGroups
}
