package foundationdbcluster

import (
	ctx "context"
	"errors"
	"time"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
)

type GenerateInitialClusterFile struct{}

func (g GenerateInitialClusterFile) Reconcile(r *ReconcileFoundationDBCluster, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	if cluster.Spec.ConnectionString != "" {
		return true, nil
	}

	log.Info("Generating initial cluster file", "namespace", cluster.Namespace, "cluster", cluster.Name)
	r.Recorder.Event(cluster, "Normal", "ChangingCoordinators", "Choosing initial coordinators")
	instances, err := r.PodLifecycleManager.GetInstances(r, context, getPodListOptions(cluster, "storage", ""))
	if err != nil {
		return false, err
	}
	count := cluster.DesiredCoordinatorCount()
	if len(instances) < count {
		return false, errors.New("Cannot find enough pods to recruit coordinators")
	}

	clusterName := connectionStringNameRegex.ReplaceAllString(cluster.Name, "_")
	connectionString := fdbtypes.ConnectionString{DatabaseName: clusterName}
	err = connectionString.GenerateNewGenerationID()
	if err != nil {
		return false, err
	}

	for i := 0; i < count; i++ {
		client, err := r.getPodClient(context, cluster, instances[i])
		if err != nil {
			return false, err
		}
		connectionString.Coordinators = append(connectionString.Coordinators, cluster.GetFullAddress(client.GetPodIP()))
	}
	cluster.Spec.ConnectionString = connectionString.String()

	err = r.Update(context, cluster)
	return false, err
}

func (g GenerateInitialClusterFile) RequeueAfter() time.Duration {
	return 0
}
