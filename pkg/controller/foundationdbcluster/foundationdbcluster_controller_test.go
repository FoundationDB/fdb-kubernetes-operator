/*
Copyright 2019 FoundationDB project authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package foundationdbcluster

import (
	"testing"
	"time"

	"github.com/onsi/gomega"
	appsv1beta1 "github.com/brownleej/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var c client.Client

var expectedRequest = reconcile.Request{NamespacedName: types.NamespacedName{Name: "operator-test", Namespace: "default"}}
var depKey = types.NamespacedName{Name: "operator-test", Namespace: "default"}
var podListOptions = (&client.ListOptions{}).InNamespace("default").MatchingLabels(map[string]string{
	"fdb-cluster-name": "operator-test",
})

const timeout = time.Second * 5

var defaultCluster = &appsv1beta1.FoundationDBCluster{
	ObjectMeta: metav1.ObjectMeta{Name: "operator-test", Namespace: "default"},
	Spec: appsv1beta1.FoundationDBClusterSpec{
		Version: "6.0.18",
	},
}

func TestReconcileWithNewCluster(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	cluster := defaultCluster.DeepCopy()

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{})
	g.Expect(err).NotTo(gomega.HaveOccurred())
	c = mgr.GetClient()

	recFn, requests := SetupTestReconcile(newReconciler(mgr))
	g.Expect(add(mgr, recFn)).NotTo(gomega.HaveOccurred())

	stopMgr, mgrStopped := StartTestManager(mgr, g)

	defer func() {
		close(stopMgr)
		mgrStopped.Wait()
	}()

	// Create the FoundationDBCluster object and expect the Reconcile and Deployment to be created
	err = c.Create(context.TODO(), cluster)
	g.Expect(err).NotTo(gomega.HaveOccurred())
	defer c.Delete(context.TODO(), cluster)
	g.Eventually(requests, timeout).Should(gomega.Receive(gomega.Equal(expectedRequest)))

	pods := &corev1.PodList{}
	g.Eventually(func() (int, error) {
		err := c.List(context.TODO(), podListOptions, pods)
		return len(pods.Items), err
	}, timeout).Should(gomega.Equal(1))

	g.Eventually(func() error {
		return c.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "operator-test"}, cluster)
	}, timeout).Should(gomega.Succeed())

	g.Expect(cluster.Spec.NextInstanceID).To(gomega.Equal(2))
}

func TestPodSpecForStorageInstances(t *testing.T) {
	g := gomega.NewGomegaWithT(t)
	spec := GetPodSpec(defaultCluster, "storage")
	g.Expect(len(spec.Containers)).To(gomega.Equal(1))

	mainContainer := spec.Containers[0]
	g.Expect(mainContainer.Image).To(gomega.Equal("foundationdb/foundationdb:6.0.18"))
}
