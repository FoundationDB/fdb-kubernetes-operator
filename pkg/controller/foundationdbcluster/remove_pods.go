package foundationdbcluster

import (
	ctx "context"
	"fmt"
	"time"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type RemovePods struct{}

func (u RemovePods) Reconcile(r *ReconcileFoundationDBCluster, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	if len(cluster.Spec.PendingRemovals) == 0 {
		return true, nil
	}
	r.Recorder.Event(cluster, "Normal", "RemovingProcesses", fmt.Sprintf("Removing pods: %v", cluster.Spec.PendingRemovals))
	for id := range cluster.Spec.PendingRemovals {
		err := r.removePod(context, cluster, id)
		if err != nil {
			return false, err
		}
	}

	for id := range cluster.Spec.PendingRemovals {
		removed, err := r.confirmPodRemoval(context, cluster, id)
		if !removed {
			return removed, err
		}
	}

	return true, nil
}

func (r *ReconcileFoundationDBCluster) removePod(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instanceName string) error {
	instanceListOptions := getSinglePodListOptions(cluster, instanceName)
	instances, err := r.PodLifecycleManager.GetInstances(r, context, instanceListOptions)
	if err != nil {
		return err
	}
	if len(instances) > 0 {
		err = r.PodLifecycleManager.DeleteInstance(r, context, instances[0])

		if err != nil {
			return err
		}
	}

	pvcListOptions := (&client.ListOptions{}).InNamespace(cluster.ObjectMeta.Namespace).MatchingField("metadata.name", fmt.Sprintf("%s-data", instanceName))
	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(context, pvcListOptions, pvcs)
	if err != nil {
		return err
	}
	if len(pvcs.Items) > 0 {
		err = r.Delete(context, &pvcs.Items[0])
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *ReconcileFoundationDBCluster) confirmPodRemoval(context ctx.Context, cluster *fdbtypes.FoundationDBCluster, instanceName string) (bool, error) {
	instanceListOptions := getSinglePodListOptions(cluster, instanceName)

	instances, err := r.PodLifecycleManager.GetInstances(r, context, instanceListOptions)
	if err != nil {
		return false, err
	}
	if len(instances) > 0 {
		log.Info("Waiting for instance get torn down", "namespace", cluster.Namespace, "cluster", cluster.Name, "pod", instanceName)
		return false, nil
	}

	pods := &corev1.PodList{}
	err = r.List(context, instanceListOptions, pods)
	if err != nil {
		return false, err
	}
	if len(pods.Items) > 0 {
		log.Info("Waiting for pod get torn down", "namespace", cluster.Namespace, "cluster", cluster.Name, "pod", instanceName)
		return false, nil
	}

	pvcListOptions := (&client.ListOptions{}).InNamespace(cluster.ObjectMeta.Namespace).MatchingField("metadata.name", fmt.Sprintf("%s-data", instanceName))
	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(context, pvcListOptions, pvcs)
	if err != nil {
		return false, err
	}
	if len(pvcs.Items) > 0 {
		log.Info("Waiting for volume claim get torn down", "namespace", cluster.Namespace, "cluster", cluster.Name, "name", pvcs.Items[0].Name)
		return false, nil
	}

	return true, nil
}

func (u RemovePods) RequeueAfter() time.Duration {
	return 0
}
