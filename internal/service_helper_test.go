package internal

import (
	"github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var _ = Describe("service_helper.go", func() {
	Describe("GetGRPCService", func() {
		var cluster *v1beta2.FoundationDBCluster
		BeforeEach(func() {
			cluster = CreateDefaultCluster()
		})

		When("using the default configuration", func() {
			It("fills in required fields", func() {
				service, err := GetGRPCService(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(service.Name).To(Equal("operator-test-1-grpc"))
				Expect(service.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
				Expect(service.Spec.Selector).To(Equal(map[string]string{
					"foundationdb.org/fdb-cluster-name": "operator-test-1",
				}))
				Expect(service.Spec.Ports).To(HaveExactElements([]corev1.ServicePort{
					{Port: 8080, TargetPort: intstr.FromInt32(8080)},
				}))
			})
		})

		When("customizing the service type", func() {
			BeforeEach(func() {
				cluster.Spec.Routing.GRPCService = &corev1.Service{
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeLoadBalancer,
					},
				}
			})

			It("fills in missing fields", func() {
				service, err := GetGRPCService(cluster)
				Expect(err).NotTo(HaveOccurred())
				Expect(service.Name).To(Equal("operator-test-1-grpc"))
				Expect(service.Spec.Type).To(Equal(corev1.ServiceTypeLoadBalancer))
			})
		})
	})
})
