package replacements

import (
	"testing"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	mockclient "github.com/FoundationDB/fdb-kubernetes-operator/mock-kubernetes-client/client"
	"k8s.io/client-go/kubernetes/scheme"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCmd(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Replacements Suite")
}

var k8sClient *mockclient.MockClient

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.UseDevMode(true), zap.WriteTo(GinkgoWriter)))

	Expect(scheme.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	Expect(fdbv1beta2.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	k8sClient = mockclient.NewMockClient(scheme.Scheme)
})

var _ = AfterEach(func() {
	k8sClient.Clear()
})
