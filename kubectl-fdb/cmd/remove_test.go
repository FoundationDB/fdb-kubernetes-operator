package cmd

import (
	ctx "context"
	"reflect"
	"testing"

	"github.com/FoundationDB/fdb-kubernetes-operator/controllers"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestRemoveInstances(t *testing.T) {
	clusterName := "test"
	namespace := "test"

	cluster := fdbtypes.FoundationDBCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
		Spec: fdbtypes.FoundationDBClusterSpec{
			ProcessCounts: fdbtypes.ProcessCounts{
				Storage: 1,
			},
		},
	}

	podList := corev1.PodList{
		Items: []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "instance-1",
					Namespace: namespace,
					Labels: map[string]string{
						controllers.FDBProcessClassLabel: fdbtypes.ProcessClassStorage,
						controllers.FDBClusterLabel:      clusterName,
					},
				},
			},
		},
	}

	tt := []struct {
		Name                                      string
		Instances                                 []string
		WithExclusion                             bool
		WithShrink                                bool
		ExpectedInstancesToRemove                 []string
		ExpectedInstancesToRemoveWithoutExclusion []string
		ExpectedProcessCounts                     fdbtypes.ProcessCounts
	}{
		{
			Name:                      "Remove instance with exclusion",
			Instances:                 []string{"instance-1"},
			WithExclusion:             true,
			WithShrink:                false,
			ExpectedInstancesToRemove: []string{"instance-1"},
			ExpectedInstancesToRemoveWithoutExclusion: []string{},
			ExpectedProcessCounts: fdbtypes.ProcessCounts{
				Storage: 1,
			},
		},
		{
			Name:                      "Remove instance without exclusion",
			Instances:                 []string{"instance-1"},
			WithExclusion:             true,
			WithShrink:                false,
			ExpectedInstancesToRemove: []string{},
			ExpectedInstancesToRemoveWithoutExclusion: []string{"instance-1"},
			ExpectedProcessCounts: fdbtypes.ProcessCounts{
				Storage: 1,
			},
		},
		{
			Name:                      "Remove instance with exclusion and shrink",
			Instances:                 []string{"instance-1"},
			WithExclusion:             true,
			WithShrink:                true,
			ExpectedInstancesToRemove: []string{"instance-1"},
			ExpectedInstancesToRemoveWithoutExclusion: []string{},
			ExpectedProcessCounts: fdbtypes.ProcessCounts{
				Storage: 0,
			},
		},
		{
			Name:                      "Remove instance without exclusion and shrink",
			Instances:                 []string{"instance-1"},
			WithExclusion:             true,
			WithShrink:                true,
			ExpectedInstancesToRemove: []string{},
			ExpectedInstancesToRemoveWithoutExclusion: []string{"instance-1"},
			ExpectedProcessCounts: fdbtypes.ProcessCounts{
				Storage: 0,
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.Name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = clientgoscheme.AddToScheme(scheme)
			_ = fdbtypes.AddToScheme(scheme)
			kubeClient := fake.NewFakeClientWithScheme(scheme, &cluster, &podList)

			removeInstances(kubeClient, clusterName, tc.Instances, namespace, tc.WithExclusion, tc.WithShrink, true)

			var resCluster fdbtypes.FoundationDBCluster
			err := kubeClient.Get(ctx.Background(), client.ObjectKey{
				Namespace: namespace,
				Name:      clusterName,
			}, &resCluster)

			if err != nil {
				t.Log(err)
				t.Fail()
			}

			if reflect.DeepEqual(tc.ExpectedInstancesToRemove, cluster.Spec.InstancesToRemove) {
				t.Logf("InstancesToRemove expected: %s - got: %s\n", tc.ExpectedInstancesToRemove, cluster.Spec.InstancesToRemove)
				t.FailNow()
			}

			if reflect.DeepEqual(tc.ExpectedInstancesToRemoveWithoutExclusion, cluster.Spec.InstancesToRemoveWithoutExclusion) {
				t.Logf("InstancesToRemoveWithoutExclusion expected: %s - got: %s\n", tc.ExpectedInstancesToRemoveWithoutExclusion, cluster.Spec.InstancesToRemoveWithoutExclusion)
				t.FailNow()
			}

			if tc.ExpectedProcessCounts.Storage != resCluster.Spec.ProcessCounts.Storage {
				t.Logf("ProcessCounts expected: %d - got: %d\n", tc.ExpectedProcessCounts.Storage, cluster.Spec.ProcessCounts.Storage)
				t.FailNow()
			}
		})
	}
}
