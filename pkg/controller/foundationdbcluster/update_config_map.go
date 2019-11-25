package foundationdbcluster

import (
	ctx "context"
	"reflect"
	"time"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

type UpdateConfigMap struct{}

func (u UpdateConfigMap) Reconcile(r *ReconcileFoundationDBCluster, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	configMap, err := GetConfigMap(context, cluster, r)
	if err != nil {
		return false, err
	}
	existing := &corev1.ConfigMap{}
	err = r.Get(context, types.NamespacedName{Namespace: configMap.Namespace, Name: configMap.Name}, existing)
	if err != nil && k8serrors.IsNotFound(err) {
		log.Info("Creating config map", "namespace", configMap.Namespace, "cluster", cluster.Name, "name", configMap.Name)
		err = r.Create(context, configMap)
		return err == nil, err
	} else if err != nil {
		return false, err
	}

	if !reflect.DeepEqual(existing.Data, configMap.Data) || !reflect.DeepEqual(existing.Labels, configMap.Labels) {
		log.Info("Updating config map", "namespace", configMap.Namespace, "cluster", cluster.Name, "name", configMap.Name)
		r.Recorder.Event(cluster, "Normal", "UpdatingConfigMap", "")
		existing.ObjectMeta.Labels = configMap.ObjectMeta.Labels
		existing.Data = configMap.Data
		err = r.Update(context, existing)
		if err != nil {
			return false, err
		}
	}

	instances, err := r.PodLifecycleManager.GetInstances(r, context, getPodListOptions(cluster, "", ""))
	if err != nil {
		return false, err
	}

	for index := range instances {
		synced, err := r.updatePodDynamicConf(context, cluster, instances[index])
		if !synced {
			return synced, err
		}
	}

	return true, nil
}

func (u UpdateConfigMap) RequeueAfter() time.Duration {
	return time.Duration(30) * time.Second
}
