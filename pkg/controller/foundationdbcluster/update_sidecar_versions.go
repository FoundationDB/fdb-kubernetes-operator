package foundationdbcluster

import (
	ctx "context"
	"fmt"
	"time"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
)

type UpdateSidecarVersions struct {
}

func (u UpdateSidecarVersions) Reconcile(r *ReconcileFoundationDBCluster, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	instances, err := r.PodLifecycleManager.GetInstances(r, context, getPodListOptions(cluster, "", ""))
	if err != nil {
		return false, err
	}
	upgraded := false
	image := cluster.Spec.SidecarContainer.ImageName
	if image == "" {
		image = "foundationdb/foundationdb-kubernetes-sidecar"
	}
	image = fmt.Sprintf("%s:%s", image, cluster.GetFullSidecarVersion())
	for _, instance := range instances {
		if instance.Pod == nil {
			return false, MissingPodError(instance, cluster)
		}
		for containerIndex, container := range instance.Pod.Spec.Containers {
			if container.Name == "foundationdb-kubernetes-sidecar" && container.Image != image {
				log.Info("Upgrading sidecar", "namespace", cluster.Namespace, "cluster", cluster.Name, "pod", instance.Pod.Name, "oldImage", container.Image, "newImage", image)
				instance.Pod.Spec.Containers[containerIndex].Image = image
				err := r.Update(context, instance.Pod)
				if err != nil {
					return false, err
				}
				upgraded = true
			}
		}
	}
	if upgraded {
		r.Recorder.Event(cluster, "Normal", "SidecarUpgraded", fmt.Sprintf("New version: %s", cluster.Spec.Version))
	}
	return true, nil
}

func (u UpdateSidecarVersions) RequeueAfter() time.Duration {
	return 0
}
