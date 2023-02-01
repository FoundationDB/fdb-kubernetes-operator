package cmd

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	mockclient "github.com/FoundationDB/fdb-kubernetes-operator/mock-kubernetes-client/client"
	"k8s.io/client-go/kubernetes/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var k8sClient *mockclient.MockClient
var cluster *fdbv1beta2.FoundationDBCluster
var clusterName = "test"
var secondCluster *fdbv1beta2.FoundationDBCluster
var secondClusterName = "test2"
var namespace = "test"

func TestCmd(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "FDB plugin")
}

var _ = BeforeSuite(func() {
	Expect(scheme.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	Expect(fdbv1beta2.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	k8sClient = mockclient.NewMockClient(scheme.Scheme)
})

var _ = BeforeEach(func() {
	cluster = createCluster(clusterName, namespace)
	secondCluster = createCluster(secondClusterName, namespace)
})

var _ = JustBeforeEach(func() {
	Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())
	Expect(k8sClient.Create(context.TODO(), secondCluster)).NotTo(HaveOccurred())
})

var _ = AfterEach(func() {
	k8sClient.Clear()
})

func createCluster(givenName string, givenNamespace string) *fdbv1beta2.FoundationDBCluster {
	return &fdbv1beta2.FoundationDBCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      givenName,
			Namespace: givenNamespace,
		},
		Spec: fdbv1beta2.FoundationDBClusterSpec{
			ProcessCounts: fdbv1beta2.ProcessCounts{
				Storage: 1,
			},
		},
	}
}
