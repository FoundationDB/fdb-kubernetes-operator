package foundationdbcluster

import (
	ctx "context"
	"reflect"
	"time"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

type UpdateLabels struct{}

func (u UpdateLabels) Reconcile(r *ReconcileFoundationDBCluster, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	instances, err := r.PodLifecycleManager.GetInstances(r, context, getPodListOptions(cluster, "", ""))
	if err != nil {
		return false, err
	}
	for _, instance := range instances {
		if instance.Pod != nil {
			processClass := instance.Metadata.Labels["fdb-process-class"]
			instanceId := instance.Metadata.Labels["fdb-instance-id"]

			labels := getPodLabels(cluster, processClass, instanceId)
			if !reflect.DeepEqual(instance.Pod.ObjectMeta.Labels, labels) {
				instance.Pod.ObjectMeta.Labels = labels
				err = r.Update(context, instance.Pod)
				if err != nil {
					return false, err
				}
			}
		}
	}

	pvcs := &corev1.PersistentVolumeClaimList{}
	err = r.List(context, getPodListOptions(cluster, "", ""), pvcs)
	if err != nil {
		return false, err
	}
	for _, pvc := range pvcs.Items {
		processClass := pvc.ObjectMeta.Labels["fdb-process-class"]
		instanceId := pvc.ObjectMeta.Labels["fdb-instance-id"]

		labels := getPodLabels(cluster, processClass, instanceId)
		if !reflect.DeepEqual(pvc.ObjectMeta.Labels, labels) {
			pvc.ObjectMeta.Labels = labels
			err = r.Update(context, &pvc)
			if err != nil {
				return false, err
			}
		}
	}

	return true, nil
}

func (u UpdateLabels) RequeueAfter() time.Duration {
	return 0
}
