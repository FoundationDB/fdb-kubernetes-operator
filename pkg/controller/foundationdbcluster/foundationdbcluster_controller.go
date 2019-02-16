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
	"context"
	"fmt"
	"strconv"

	fdbtypes "github.com/brownleej/fdb-kubernetes-operator/pkg/apis/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller")

var processClasses = []string{"storage"}

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new FoundationDBCluster Controller and adds it to the Manager with default RBAC. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileFoundationDBCluster{Client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	log.Info("Adding controller", "docker root", DockerImageRoot)
	c, err := controller.New("foundationdbcluster-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to FoundationDBCluster
	err = c.Watch(&source.Kind{Type: &fdbtypes.FoundationDBCluster{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to pods owned by a FoundationDBCluster
	err = c.Watch(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &fdbtypes.FoundationDBCluster{},
	})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileFoundationDBCluster{}

// ReconcileFoundationDBCluster reconciles a FoundationDBCluster object
type ReconcileFoundationDBCluster struct {
	client.Client
	scheme *runtime.Scheme
}

func (r *ReconcileFoundationDBCluster) addPods(cluster *fdbtypes.FoundationDBCluster) error {
	for _, processClass := range processClasses {
		existingPods := &corev1.PodList{}
		podListLabels := map[string]string{
			"fdb-cluster-name":  cluster.ObjectMeta.Name,
			"fdb-process-class": processClass}
		err := r.List(
			context.TODO(),
			(&client.ListOptions{}).InNamespace(cluster.ObjectMeta.Namespace).MatchingLabels(podListLabels),
			existingPods)
		if err != nil {
			return err
		}

		desiredCount := cluster.DesiredProcessCount(processClass)
		newCount := desiredCount - len(existingPods.Items)
		if newCount > 0 {
			id := cluster.Spec.NextInstanceID
			if id < 1 {
				id = 1
			}
			for i := 0; i < newCount; i++ {
				name := fmt.Sprintf("%s-%d", cluster.ObjectMeta.Name, id)
				podLabels := map[string]string{
					"fdb-cluster-name":  podListLabels["fdb-cluster-name"],
					"fdb-process-class": podListLabels["fdb-process-class"],
					"fdb-instance-id":   strconv.Itoa(id),
				}
				isController := true
				err := r.Create(context.TODO(), &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      name,
						Namespace: cluster.Namespace,
						Labels:    podLabels,
						OwnerReferences: []metav1.OwnerReference{metav1.OwnerReference{
							APIVersion: cluster.APIVersion,
							Kind:       cluster.Kind,
							Name:       cluster.Name,
							UID:        cluster.UID,
							Controller: &isController,
						}},
					},
					Spec: GetPodSpec(cluster, processClass),
				})
				if err != nil {
					return err
				}
				id++
			}
			cluster.Spec.NextInstanceID = id

			r.Update(context.TODO(), cluster)
		}
	}
	return nil
}

// DockerImageRoot is the prefix for our docker image paths
var DockerImageRoot = "foundationdb"

// GetPodSpec builds a pod spec for a FoundationDB pod
func GetPodSpec(cluster *fdbtypes.FoundationDBCluster, processClass string) corev1.PodSpec {
	return corev1.PodSpec{
		Containers: []corev1.Container{
			corev1.Container{
				Name:  "foundationdb",
				Image: fmt.Sprintf("%s/foundationdb:%s", DockerImageRoot, cluster.Spec.Version),
			},
		},
	}
}

// Reconcile reads that state of the cluster for a FoundationDBCluster object and makes changes based on the state read
// and what is in the FoundationDBCluster.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  The scaffolding writes
// a Deployment as an example
// Automatically generate RBAC rules to allow the Controller to read and write Deployments
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;watch;list;create;update;delete
// +kubebuilder:rbac:groups=apps.foundationdb.org,resources=foundationdbclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.foundationdb.org,resources=foundationdbclusters/status,verbs=get;update;patch
func (r *ReconcileFoundationDBCluster) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	// Fetch the FoundationDBCluster instance
	cluster := &fdbtypes.FoundationDBCluster{}
	err := r.Get(context.TODO(), request.NamespacedName, cluster)

	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	err = r.addPods(cluster)
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}
