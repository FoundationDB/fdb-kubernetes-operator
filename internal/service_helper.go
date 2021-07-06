package internal

import (
	"github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	v1 "k8s.io/api/core/v1"
)

// GetHeadlessService builds a headless service for a FoundationDB cluster.
func GetHeadlessService(cluster *v1beta1.FoundationDBCluster) *v1.Service {
	headless := cluster.Spec.Routing.HeadlessService
	if headless == nil || !*headless {
		return nil
	}

	service := &v1.Service{
		ObjectMeta: GetObjectMetadata(cluster, nil, "", ""),
	}
	service.ObjectMeta.Name = cluster.ObjectMeta.Name
	service.Spec.ClusterIP = "None"
	service.Spec.Selector = cluster.Spec.LabelConfig.MatchLabels

	return service
}
