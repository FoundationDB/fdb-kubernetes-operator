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
var namespace = "test"

// MockVersionChecker will help to mock the calls to GitHub API to get 'latest' version in tests
type MockVersionChecker struct {
	MockedVersion string
}

func (versionChecker *MockVersionChecker) getLatestPluginVersion() (string, error) {
	if versionChecker. MockedVersion == "" {
		return "latest", nil
	}

	return versionChecker. MockedVersion, nil
}

func TestCmd(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "FDB plugin")
}

var _ = BeforeSuite(func() {
	Expect(scheme.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	Expect(fdbv1beta2.AddToScheme(scheme.Scheme)).NotTo(HaveOccurred())
	// We have to create those indexes, otherwise the fake client is complaining.
	k8sClient = mockclient.NewMockClientWithHooksAndIndexes(scheme.Scheme, nil, nil, true)
})

var _ = BeforeEach(func() {
	PluginVersionChecker = &MockVersionChecker{}
	cluster = generateClusterStruct(clusterName, namespace)
})

var _ = JustBeforeEach(func() {
	Expect(k8sClient.Create(context.TODO(), cluster)).NotTo(HaveOccurred())
})

var _ = AfterEach(func() {
	k8sClient.Clear()
})

func generateClusterStruct(name string, namespace string) *fdbv1beta2.FoundationDBCluster {
	return &fdbv1beta2.FoundationDBCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: fdbv1beta2.FoundationDBClusterSpec{
			ProcessCounts: fdbv1beta2.ProcessCounts{
				Storage: 1,
			},
		},
		Status: fdbv1beta2.FoundationDBClusterStatus{
			ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
				{
					ProcessGroupID: fdbv1beta2.ProcessGroupID(name + "-instance-1"),
				},
				{
					ProcessGroupID: fdbv1beta2.ProcessGroupID(name + "-instance-2"),
				},
			},
		},
	}
}
