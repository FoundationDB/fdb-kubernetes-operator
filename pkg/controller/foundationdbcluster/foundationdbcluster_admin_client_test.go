package foundationdbcluster

import (
	"testing"

	"github.com/onsi/gomega"
	appsv1beta1 "github.com/foundationdb/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
)

func TestGettingConfigurationString(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	configuration := DatabaseConfiguration{
		ReplicationMode: "double",
		StorageEngine:   "ssd",
		RoleCounts: appsv1beta1.RoleCounts{
			Logs: 5,
		},
	}
	g.Expect(configuration.getConfigurationString()).To(gomega.Equal("double ssd logs=5"))
}
