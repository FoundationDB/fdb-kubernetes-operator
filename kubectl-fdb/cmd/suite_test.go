package cmd

import (
	"context"
	"testing"

	kubeHelper "github.com/FoundationDB/fdb-kubernetes-operator/v2/internal/kubernetes"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/v2/api/v1beta2"
	mockclient "github.com/FoundationDB/fdb-kubernetes-operator/v2/mock-kubernetes-client/client"
	"k8s.io/client-go/kubernetes/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	k8sClient         *mockclient.MockClient
	cluster           *fdbv1beta2.FoundationDBCluster
	secondCluster     *fdbv1beta2.FoundationDBCluster
	clusterName       = "test"
	secondClusterName = "test2"
	namespace         = "test"
)

// MockVersionChecker will help to mock the calls to GitHub API to get 'latest' version in tests
type MockVersionChecker struct {
	MockedVersion string
}

func (versionChecker *MockVersionChecker) getLatestPluginVersion() (string, error) {
	if versionChecker.MockedVersion == "" {
		return "latest", nil
	}

	return versionChecker.MockedVersion, nil
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
	// Allow the unit tests to run the spdy executor, we can extend that later to allow better mocking.
	kubeHelper.NewSPDYExecutor = kubeHelper.FakeNewSPDYExecutor
})

var _ = BeforeEach(func() {
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
			ProcessGroupIDPrefix: name,
		},
		Status: fdbv1beta2.FoundationDBClusterStatus{
			ProcessGroups: []*fdbv1beta2.ProcessGroupStatus{
				{
					ProcessGroupID: fdbv1beta2.ProcessGroupID(name + "-" + string(fdbv1beta2.ProcessClassStorage) + "-1"),
					ProcessClass:   fdbv1beta2.ProcessClassStorage,
					ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
						fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.PodFailing),
					},
				},
				{
					ProcessGroupID: fdbv1beta2.ProcessGroupID(name + "-" + string(fdbv1beta2.ProcessClassStorage) + "-2"),
					ProcessClass:   fdbv1beta2.ProcessClassStorage,
					ProcessGroupConditions: []*fdbv1beta2.ProcessGroupCondition{
						fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.PodFailing),
						fdbv1beta2.NewProcessGroupCondition(fdbv1beta2.MissingProcesses),
					},
				},
				{
					ProcessGroupID: fdbv1beta2.ProcessGroupID(name + "-" + string(fdbv1beta2.ProcessClassStateless) + "-3"),
					ProcessClass:   fdbv1beta2.ProcessClassStateless,
				},
			},
		},
	}
}
