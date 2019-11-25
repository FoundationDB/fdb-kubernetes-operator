package foundationdbcluster

import (
	ctx "context"
	"fmt"
	"time"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
)

type ExcludeInstances struct{}

func (e ExcludeInstances) Reconcile(r *ReconcileFoundationDBCluster, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	adminClient, err := r.AdminClientProvider(cluster, r)
	if err != nil {
		return false, err
	}
	defer adminClient.Close()

	addresses := make([]string, 0, len(cluster.Spec.PendingRemovals))
	for _, address := range cluster.Spec.PendingRemovals {
		addresses = append(addresses, cluster.GetFullAddress(address))
	}

	if len(addresses) > 0 {
		err = adminClient.ExcludeInstances(addresses)
		r.Recorder.Event(cluster, "Normal", "ExcludingProcesses", fmt.Sprintf("Excluding %v", addresses))
		if err != nil {
			return false, err
		}
	}

	remaining := addresses
	for len(remaining) > 0 {
		remaining, err = adminClient.CanSafelyRemove(addresses)
		if err != nil {
			return false, err
		}
		if len(remaining) > 0 {
			log.Info("Waiting for exclusions to complete", "namespace", cluster.Namespace, "cluster", cluster.Name, "remainingServers", remaining)
			time.Sleep(time.Second)
		}
	}

	return true, nil
}

func (e ExcludeInstances) RequeueAfter() time.Duration {
	return 0
}
