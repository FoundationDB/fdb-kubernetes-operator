package foundationdbcluster

import (
	ctx "context"
	"fmt"
	"reflect"
	"time"

	fdbtypes "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	"k8s.io/apimachinery/pkg/types"
)

type UpdateDatabaseConfiguration struct{}

func (u UpdateDatabaseConfiguration) Reconcile(r *ReconcileFoundationDBCluster, context ctx.Context, cluster *fdbtypes.FoundationDBCluster) (bool, error) {
	adminClient, err := r.AdminClientProvider(cluster, r)

	if err != nil {
		return false, err
	}
	defer adminClient.Close()

	initialConfig := !cluster.Spec.Configured
	desiredConfiguration := cluster.DesiredDatabaseConfiguration()
	desiredConfiguration.RoleCounts.Storage = 0
	needsChange := false
	if !cluster.Spec.Configured {
		needsChange = true
	} else {
		status, err := adminClient.GetStatus()
		if err != nil {
			return false, err
		}

		needsChange = !reflect.DeepEqual(desiredConfiguration, status.Cluster.DatabaseConfiguration.FillInDefaultsFromStatus())
	}

	if needsChange {
		configurationString, _ := desiredConfiguration.GetConfigurationString()
		var enabled = cluster.Spec.AutomationOptions.ConfigureDatabase
		if enabled != nil && !*enabled {
			err := r.Get(context, types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
			if err != nil {
				return false, err
			}

			r.Recorder.Event(cluster, "Normal", "NeedsConfigurationChange",
				fmt.Sprintf("Spec require configuration change to `%s`, but configuration changes are disabled", configurationString))
			cluster.Status.Generations.NeedsConfigurationChange = cluster.ObjectMeta.Generation
			err = r.postStatusUpdate(context, cluster)
			if err != nil {
				log.Error(err, "Error updating cluster status", "namespace", cluster.Namespace, "cluster", cluster.Name)
			}
			return false, ReconciliationNotReadyError{message: "Database configuration changes are disabled"}
		}
		log.Info("Configuring database", "namespace", cluster.Namespace, "cluster", cluster.Name)
		r.Recorder.Event(cluster, "Normal", "ConfiguringDatabase",
			fmt.Sprintf("Setting database configuration to `%s`", configurationString),
		)
		err = adminClient.ConfigureDatabase(cluster.DesiredDatabaseConfiguration(), !cluster.Spec.Configured)
		if err != nil {
			return false, err
		}
		if initialConfig {
			cluster.Spec.Configured = true
			err = r.Update(context, cluster)
			if err != nil {
				return false, err
			}
		}
		log.Info("Configured database", "namespace", cluster.Namespace, "cluster", cluster.Name)
	}

	return !initialConfig, nil
}

func (u UpdateDatabaseConfiguration) RequeueAfter() time.Duration {
	return 0
}
