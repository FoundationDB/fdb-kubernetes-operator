package foundationdbcluster

import (
	ctx "context"
	"fmt"
	"strconv"
	"time"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

type AddPods struct{}

func (a AddPods) Reconcile(r *ReconcileFoundationDBCluster, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	hasNewPods := false
	currentCounts := cluster.Status.ProcessCounts.Map()
	desiredCounts := cluster.GetProcessCountsWithDefaults().Map()

	err := r.Get(context, types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
	if err != nil {
		return false, err
	}

	currentPods := &corev1.PodList{}
	err = r.List(context, getPodListOptions(cluster, "", ""), currentPods)
	if err != nil {
		return false, err
	}
	for _, pod := range currentPods.Items {
		if pod.DeletionTimestamp != nil {
			return false, ReconciliationNotReadyError{message: "Cluster has pod that is pending deletion", retryable: true}
		}
	}

	for _, processClass := range fdbtypes.ProcessClasses {
		desiredCount := desiredCounts[processClass]
		if desiredCount < 0 {
			desiredCount = 0
		}
		newCount := desiredCount - currentCounts[processClass]
		if newCount > 0 {
			r.Recorder.Event(cluster, "Normal", "AddingProcesses", fmt.Sprintf("Adding %d %s processes", newCount, processClass))

			pvcs := &corev1.PersistentVolumeClaimList{}
			r.List(context, getPodListOptions(cluster, processClass, ""), pvcs)
			reusablePvcs := make(map[int]bool, len(pvcs.Items))
			for index, pvc := range pvcs.Items {
				if pvc.Status.Phase == "Bound" && pvc.ObjectMeta.DeletionTimestamp == nil {
					matchingInstances, err := r.PodLifecycleManager.GetInstances(
						r, context,
						getPodListOptions(cluster, processClass, pvc.Labels["fdb-instance-id"]),
					)
					if err != nil {
						return false, err
					}
					if len(matchingInstances) == 0 {
						reusablePvcs[index] = true
					}
				}
			}

			addedCount := 0
			for index := range reusablePvcs {
				if newCount <= 0 {
					break
				}
				id, err := strconv.Atoi(pvcs.Items[index].Labels["fdb-instance-id"])
				if err != nil {
					return false, err
				}

				pod, err := GetPod(context, cluster, processClass, id, r)
				if err != nil {
					return false, err
				}

				err = r.PodLifecycleManager.CreateInstance(r, context, pod)
				if err != nil {
					return false, err
				}
				addedCount++
				newCount--
			}

			id := cluster.Spec.NextInstanceID
			if id < 1 {
				id = 1
			}
			for i := 0; i < newCount; i++ {
				for id > 0 {
					pvcs := &corev1.PersistentVolumeClaimList{}
					err := r.List(context, getPodListOptions(cluster, "", strconv.Itoa(id)), pvcs)
					if err != nil {
						return false, err
					}
					if len(pvcs.Items) == 0 {
						break
					}
					id++
				}

				pvc, err := GetPvc(cluster, processClass, id)
				if err != nil {
					return false, err
				}

				if pvc != nil {
					owner, err := buildOwnerReference(context, cluster, r)
					if err != nil {
						return false, err
					}
					pvc.ObjectMeta.OwnerReferences = owner

					err = r.Create(context, pvc)
					if err != nil {
						return false, err
					}
				}

				pod, err := GetPod(context, cluster, processClass, id, r)
				if err != nil {
					return false, err
				}
				err = r.PodLifecycleManager.CreateInstance(r, context, pod)
				if err != nil {
					return false, err
				}

				addedCount++
				id++
			}
			cluster.Spec.NextInstanceID = id
			cluster.Status.ProcessCounts.IncreaseCount(processClass, addedCount)
			hasNewPods = true
		}
	}
	if hasNewPods {
		err := r.Update(context, cluster)
		if err != nil {
			return false, err
		}
	}
	return !hasNewPods, nil
}

func (a AddPods) RequeueAfter() time.Duration {
	return 0
}
