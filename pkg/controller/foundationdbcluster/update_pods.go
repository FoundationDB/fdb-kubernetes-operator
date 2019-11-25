package foundationdbcluster

import (
	ctx "context"
	"encoding/json"
	"fmt"
	"time"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	"k8s.io/apimachinery/pkg/types"
)

type UpdatePods struct{}

func (u UpdatePods) Reconcile(r *ReconcileFoundationDBCluster, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	instances, err := r.PodLifecycleManager.GetInstances(r, context, getPodListOptions(cluster, "", ""))
	if err != nil {
		return false, err
	}

	updates := make(map[string][]FdbInstance)

	for _, instance := range instances {
		if instance.Pod == nil {
			continue
		}
		spec := GetPodSpec(cluster, instance.Metadata.Labels["fdb-process-class"], instance.Metadata.Labels["fdb-instance-id"])
		specBytes, err := json.Marshal(spec)
		if err != nil {
			return false, err
		}
		if instance.Metadata.Annotations[LastPodSpecKey] != string(specBytes) {
			podClient, err := r.getPodClient(context, cluster, instance)
			if err != nil {
				return false, err
			}
			substitutions, err := podClient.GetVariableSubstitutions()
			if err != nil {
				return false, err
			}
			zone := substitutions["FDB_ZONE_ID"]
			if updates[zone] == nil {
				updates[zone] = make([]FdbInstance, 0)
			}
			updates[zone] = append(updates[zone], instance)
		}
	}

	if len(updates) > 0 {
		var enabled = cluster.Spec.AutomationOptions.DeletePods
		if enabled != nil && !*enabled {
			err := r.Get(context, types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
			if err != nil {
				return false, err
			}

			r.Recorder.Event(cluster, "Normal", "NeedsPodsDeletion",
				fmt.Sprintf("Spec require deleting some pods, but deleting pods is disabled"))
			cluster.Status.Generations.NeedsPodDeletion = cluster.ObjectMeta.Generation
			err = r.postStatusUpdate(context, cluster)
			if err != nil {
				log.Error(err, "Error updating cluster status", "namespace", cluster.Namespace, "cluster", cluster.Name)
			}
			return false, ReconciliationNotReadyError{message: "Pod deletion is disabled"}
		}
	}

	for zone, zoneInstances := range updates {
		log.Info("Deleting pods", "namespace", cluster.Namespace, "cluster", cluster.Name, "zone", zone, "count", len(zoneInstances))
		r.Recorder.Event(cluster, "Normal", "UpdatingPods", fmt.Sprintf("Recreating pods in zone %s", zone))
		ready, err := r.PodLifecycleManager.CanDeletePods(r, context, cluster)
		if err != nil {
			return false, err
		}
		if !ready {
			return false, ReconciliationNotReadyError{message: "Reconciliation requires deleting pods, but deletion is not currently safe"}
		}
		err = r.PodLifecycleManager.UpdatePods(r, context, cluster, zoneInstances)
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

func (u UpdatePods) RequeueAfter() time.Duration {
	return 0
}
