/*
 * fdb_cluster.go
 *
 * This source file is part of the FoundationDB open source project
 *
 * Copyright 2023 Apple Inc. and the FoundationDB project authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package fixtures

import (
	ctx "context"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/client-go/util/retry"

	fdbv1beta2 "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	kubeErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FdbCluster is a fixture that allows tests to manipulate an FDB cluster with some name.
// Depending on how it was instantiated, the cluster may or may not exist, and may or may not
// be part of an HA configuration.
type FdbCluster struct {
	cluster *fdbv1beta2.FoundationDBCluster
	factory *Factory
}

// GetFDBImage return the FDB image used for the current version, defined in the FoundationDBClusterSpec.
func (fdbCluster *FdbCluster) GetFDBImage() string {
	return fdbv1beta2.SelectImageConfig(fdbCluster.GetClusterSpec().MainContainer.ImageConfigs, fdbCluster.cluster.Spec.Version).
		Image()
}

// GetSidecarImageForVersion return the sidecar image used for the specified version.
func (fdbCluster *FdbCluster) GetSidecarImageForVersion(version string) string {
	// In the case of the unified image the sidecar will also be the main container image.
	if fdbCluster.cluster.UseUnifiedImage() {
		return fdbv1beta2.SelectImageConfig(fdbCluster.GetClusterSpec().MainContainer.ImageConfigs, version).
			Image()
	}

	return fdbv1beta2.SelectImageConfig(fdbCluster.GetClusterSpec().SidecarContainer.ImageConfigs, version).
		Image()
}

// ExecuteCmdOnPod will run the provided command in a Shell.
func (fdbCluster *FdbCluster) ExecuteCmdOnPod(
	pod corev1.Pod,
	container string,
	command string,
	printOutput bool,
) (string, string, error) {
	return fdbCluster.factory.ExecuteCmd(pod.Namespace, pod.Name, container, command, printOutput)
}

func (factory *Factory) createFdbClusterObject(
	cluster *fdbv1beta2.FoundationDBCluster,
) *FdbCluster {
	return &FdbCluster{
		cluster,
		factory,
	}
}

// GetResourceLabels returns the resource labels for all created resources of the current FoundationDBCluster.
func (fdbCluster *FdbCluster) GetResourceLabels() map[string]string {
	return fdbCluster.cluster.GetResourceLabels()
}

// Name returns the name for the FoundationDBCluster.
func (fdbCluster *FdbCluster) Name() string {
	return fdbCluster.cluster.Name
}

func (fdbCluster *FdbCluster) getClient() client.Client {
	return fdbCluster.factory.GetControllerRuntimeClient()
}

// Namespace returns the namespace for the FoundationDBCluster.
func (fdbCluster *FdbCluster) Namespace() string {
	return fdbCluster.cluster.Namespace
}

// WaitUntilExists synchronously waits until the cluster exists.  Usually called after Create().
func (fdbCluster *FdbCluster) WaitUntilExists() {
	clusterRequest := fdbv1beta2.FoundationDBCluster{}
	key := client.ObjectKeyFromObject(fdbCluster.cluster)

	gomega.Eventually(func() error {
		return fdbCluster.getClient().
			Get(ctx.Background(), key, &clusterRequest)
	}).WithTimeout(2 * time.Minute).ShouldNot(gomega.HaveOccurred())
}

// Create asynchronously creates this FDB cluster.
func (fdbCluster *FdbCluster) Create() error {
	return fdbCluster.getClient().Create(ctx.Background(), fdbCluster.cluster)
}

// Update asynchronously updates this FDB cluster definition.
func (fdbCluster *FdbCluster) Update() error {
	return fdbCluster.getClient().Update(ctx.Background(), fdbCluster.cluster)
}

// ReconciliationOptions defines the different reconciliation options.
type ReconciliationOptions struct {
	allowSoftReconciliation bool
	creationTrackerLogger   CreationTrackerLogger
	minimumGeneration       int64
	timeOutInSeconds        int
	pollTimeInSeconds       int
}

// ReconciliationOption defines the reconciliation option.
type ReconciliationOption func(*ReconciliationOptions)

// SoftReconcileOption specifies that the reconciliation is completed as soon as the Status.Generations.Reconciled reaches the
// expected generation. Independent of other possible Generations, e.g. it could be still the case that the operator has to
// delete additional Process Groups.
func SoftReconcileOption(enable bool) ReconciliationOption {
	return func(options *ReconciliationOptions) {
		options.allowSoftReconciliation = enable
	}
}

// CreationTrackerLoggerOption sets the creation tracker that will printout the time for the different creation stages.
func CreationTrackerLoggerOption(creationTrackerLogger CreationTrackerLogger) ReconciliationOption {
	return func(options *ReconciliationOptions) {
		options.creationTrackerLogger = creationTrackerLogger
	}
}

// MinimumGenerationOption specifies the minimum generation to be reconciled too.
func MinimumGenerationOption(minimumGeneration int64) ReconciliationOption {
	return func(options *ReconciliationOptions) {
		options.minimumGeneration = minimumGeneration
	}
}

// TimeOutInSecondsOption defines the timeout for the reconciliation. If not set the default is 4800 seconds
func TimeOutInSecondsOption(timeOutInSeconds int) ReconciliationOption {
	return func(options *ReconciliationOptions) {
		options.timeOutInSeconds = timeOutInSeconds
	}
}

// PollTimeInSecondsOption defines the polling time for the reconciliation. If not set the default is 10 seconds
func PollTimeInSecondsOption(pollTimeInSeconds int) ReconciliationOption {
	return func(options *ReconciliationOptions) {
		options.pollTimeInSeconds = pollTimeInSeconds
	}
}

// MakeReconciliationOptionsStruct applies the provided options to the ReconciliationOptions.
func MakeReconciliationOptionsStruct(
	options ...func(*ReconciliationOptions),
) *ReconciliationOptions {
	reconciliationOptions := &ReconciliationOptions{}

	for _, option := range options {
		option(reconciliationOptions)
	}

	if reconciliationOptions.timeOutInSeconds == 0 {
		// Wait for 30 minutes as timeout.
		reconciliationOptions.timeOutInSeconds = 1800
	}

	if reconciliationOptions.pollTimeInSeconds == 0 {
		reconciliationOptions.pollTimeInSeconds = 10
	}

	return reconciliationOptions
}

// WaitForReconciliation waits for the cluster to be reconciled based on the provided options.
func (fdbCluster *FdbCluster) WaitForReconciliation(options ...func(*ReconciliationOptions)) error {
	reconciliationOptions := MakeReconciliationOptionsStruct(options...)

	return fdbCluster.waitForReconciliationToGeneration(
		reconciliationOptions.minimumGeneration,
		reconciliationOptions.allowSoftReconciliation,
		reconciliationOptions.creationTrackerLogger,
		reconciliationOptions.timeOutInSeconds,
		reconciliationOptions.pollTimeInSeconds,
	)
}

// waitForReconciliationToGeneration waits for a specific generation to be reached.
func (fdbCluster *FdbCluster) waitForReconciliationToGeneration(
	minimumGeneration int64,
	softReconciliationAllowed bool,
	creationTrackerLogger CreationTrackerLogger,
	timeOutInSeconds int,
	pollTimeInSeconds int,
) error {
	if timeOutInSeconds < pollTimeInSeconds {
		return fmt.Errorf(
			"timeout %d is less than poll time %d",
			timeOutInSeconds,
			pollTimeInSeconds,
		)
	}

	log.Printf(
		"waiting until the cluster %s/%s is healthy and reconciled",
		fdbCluster.cluster.Namespace,
		fdbCluster.cluster.Name,
	)

	if minimumGeneration > 0 {
		log.Printf(
			"waiting for generation %d, current generation: %d",
			minimumGeneration,
			fdbCluster.cluster.Generation,
		)
	}

	var creationTracker *fdbClusterCreationTracker
	if creationTrackerLogger != nil {
		creationTracker = newFdbClusterCreationTracker(
			fdbCluster.getClient(),
			creationTrackerLogger,
		)
	}

	checkIfReconciliationIsDone := func(cluster *fdbv1beta2.FoundationDBCluster) bool {
		if creationTracker != nil {
			creationTracker.trackProgress(cluster)
		}

		var reconciled bool
		if softReconciliationAllowed {
			reconciled = cluster.Status.Generations.Reconciled == cluster.ObjectMeta.Generation
		} else {
			reconciled = cluster.Status.Generations == fdbv1beta2.ClusterGenerationStatus{Reconciled: cluster.ObjectMeta.Generation}
		}

		if minimumGeneration > 0 {
			reconciled = reconciled &&
				cluster.Status.Generations.Reconciled >= minimumGeneration
		}

		if reconciled {
			log.Printf(
				"reconciled name=%s, namespace=%s, generation:%d",
				fdbCluster.cluster.Name,
				fdbCluster.cluster.Namespace,
				fdbCluster.cluster.Generation,
			)
			return true
		}

		return false
	}

	err := fdbCluster.WaitUntilWithForceReconcile(pollTimeInSeconds, timeOutInSeconds, checkIfReconciliationIsDone)
	if creationTracker != nil {
		creationTracker.report()
	}

	return err
}

// WaitUntilWithForceReconcile will wait either until the checkMethod returns true or until the timeout is hit.
func (fdbCluster *FdbCluster) WaitUntilWithForceReconcile(pollTimeInSeconds int, timeOutInSeconds int, checkMethod func(cluster *fdbv1beta2.FoundationDBCluster) bool) error {
	// Printout the initial state of the cluster before we moving forward waiting for the checkMethod to return true.
	fdbCluster.factory.DumpState(fdbCluster)

	lastForcedReconciliationTime := time.Now()
	forceReconcileDuration := 4 * time.Minute

	return wait.PollImmediate(
		time.Duration(pollTimeInSeconds)*time.Second,
		time.Duration(timeOutInSeconds)*time.Second,
		func() (bool, error) {
			resCluster := fdbCluster.GetCluster()

			if checkMethod(resCluster) {
				return true, nil
			}

			// Force a reconcile if needed.
			if time.Since(lastForcedReconciliationTime) >= forceReconcileDuration {
				fdbCluster.ForceReconcile()
				lastForcedReconciliationTime = time.Now()
			}

			return false, nil
		},
	)
}

// ForceReconcile will add an annotation with the current timestamp on the FoundationDBCluster resource to make sure
// the operator reconciliation loop is triggered. This is used to speed up some test cases.
func (fdbCluster *FdbCluster) ForceReconcile() {
	log.Printf("ForceReconcile: Status Generations=%s, Metadata Generation=%d",
		ToJSON(fdbCluster.cluster.Status.Generations),
		fdbCluster.cluster.ObjectMeta.Generation)

	fdbCluster.factory.DumpState(fdbCluster)
	patch := client.MergeFrom(fdbCluster.cluster.DeepCopy())
	if fdbCluster.cluster.Annotations == nil {
		fdbCluster.cluster.Annotations = make(map[string]string)
	}
	fdbCluster.cluster.Annotations["foundationdb.org/reconcile"] = strconv.FormatInt(
		time.Now().UnixNano(),
		10,
	)

	// This will apply an Annotation to the object which will trigger the reconcile loop.
	// This should speed up the reconcile phase.
	err := fdbCluster.getClient().Patch(
		ctx.Background(),
		fdbCluster.cluster,
		patch)
	if err != nil {
		log.Println("error patching annotation to force reconcile, error:", err.Error())
	}
}

// GetCluster returns the FoundationDBCluster of the cluster. This will fetch the latest value from  the Kubernetes API.
func (fdbCluster *FdbCluster) GetCluster() *fdbv1beta2.FoundationDBCluster {
	var cluster *fdbv1beta2.FoundationDBCluster

	gomega.Eventually(func() error {
		var err error
		cluster, err = fdbCluster.factory.getClusterStatus(
			fdbCluster.Name(),
			fdbCluster.Namespace(),
		)

		if err != nil {
			log.Println(
				"error fetching information for FoundationDBCluster",
				fdbCluster.Name(),
				"in",
				fdbCluster.Namespace(),
				"got error:",
				err.Error(),
			)
		}

		return err
	}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).ShouldNot(gomega.HaveOccurred())

	// Update the cached cluster
	fdbCluster.cluster = cluster
	return cluster
}

// GetCachedCluster returns the current cluster definition stored in the fdbCluster struct. This could be outdated and
// if you need the most recent version of the definition you should use `GetCluster`. This method is useful if you want
// to inspect fields that are not changing.
func (fdbCluster *FdbCluster) GetCachedCluster() *fdbv1beta2.FoundationDBCluster {
	return fdbCluster.cluster
}

// SetDatabaseConfiguration sets the provided DatabaseConfiguration for the FoundationDBCluster.
func (fdbCluster *FdbCluster) SetDatabaseConfiguration(
	config fdbv1beta2.DatabaseConfiguration,
	waitForReconcile bool,
) error {
	fdbCluster.cluster.Spec.DatabaseConfiguration = config
	fdbCluster.UpdateClusterSpec()

	if !waitForReconcile {
		return nil
	}

	return fdbCluster.WaitForReconciliation()
}

// UpdateClusterStatus updates the FoundationDBCluster status. This method allows to modify the status sub-resource of
// the FoundationDBCluster resource.
func (fdbCluster *FdbCluster) UpdateClusterStatus() {
	fdbCluster.UpdateClusterStatusWithStatus(fdbCluster.cluster.Status.DeepCopy())
}

// UpdateClusterStatusWithStatus ensures that the FoundationDBCluster status will be updated in Kubernetes. This method has a retry mechanism
// implemented and ensures that the provided (local) Status matches the status in Kubernetes. You must make sure that you call
// fdbCluster.GetCluster() before updating the status, to make sure you are not overwriting the current state with an outdated state.
// An example on how to update a field with this method:
//
//		// Make sure the operator doesn't modify the status.
//		fdbCluster.SetSkipReconciliation(true)
//		status := fdbCluster.GetCluster().Status.DeepCopy() // Fetch the current status.
//	    // Create a new process group.
//		processGroupID = cluster.GetNextRandomProcessGroupID(fdbv1beta2.ProcessClassStateless, processGroupIDs[fdbv1beta2.ProcessClassStateless])
//		status.ProcessGroups = append(status.ProcessGroups, fdbv1beta2.NewProcessGroupStatus(processGroupID, fdbv1beta2.ProcessClassStateless, nil))
//		fdbCluster.UpdateClusterStatusWithStatus(status)
//
//		// Make sure the operator picks up the work again
//		fdbCluster.SetSkipReconciliation(false)
func (fdbCluster *FdbCluster) UpdateClusterStatusWithStatus(desiredStatus *fdbv1beta2.FoundationDBClusterStatus) {
	fetchedCluster := &fdbv1beta2.FoundationDBCluster{}

	// This is flaky. It sometimes responds with an error saying that the object has been updated.
	// Try a few times before giving up.
	gomega.Eventually(func(g gomega.Gomega) bool {
		err := fdbCluster.getClient().
			Get(ctx.Background(), client.ObjectKeyFromObject(fdbCluster.cluster), fetchedCluster)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "error fetching cluster")

		updated := equality.Semantic.DeepEqual(fetchedCluster.Status, *desiredStatus)
		log.Println("UpdateClusterStatus: updated:", updated)
		if updated {
			return true
		}

		desiredStatus.DeepCopyInto(&fetchedCluster.Status)
		err = fdbCluster.getClient().Status().Update(ctx.Background(), fetchedCluster)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "error updating cluster status")
		// Retry here and let the method fetch the latest version of the cluster again until the spec is updated.
		return false
	}).WithTimeout(10 * time.Minute).WithPolling(1 * time.Second).Should(gomega.BeTrue())

	fdbCluster.cluster = fetchedCluster
}

// UpdateClusterSpec ensures that the FoundationDBCluster will be updated in Kubernetes. This method has a retry mechanism
// implemented and ensures that the provided (local) Spec matches the Spec in Kubernetes.
func (fdbCluster *FdbCluster) UpdateClusterSpec() {
	fdbCluster.UpdateClusterSpecWithSpec(fdbCluster.cluster.Spec.DeepCopy())
}

// UpdateClusterSpecWithSpec ensures that the FoundationDBCluster will be updated in Kubernetes. This method has a retry mechanism
// implemented and ensures that the provided (local) Spec matches the Spec in Kubernetes. You must make sure that you call
// fdbCluster.GetCluster() before updating the spec, to make sure you are not overwriting the current state with an outdated state.
// An example on how to update a field with this method:
//
//	spec := fdbCluster.GetCluster().Spec.DeepCopy() // Fetch the current Spec.
//	spec.Version = "7.1.27" // Make your changes.
//
//	fdbCluster.UpdateClusterSpecWithSpec(spec) // Update the spec.
func (fdbCluster *FdbCluster) UpdateClusterSpecWithSpec(desiredSpec *fdbv1beta2.FoundationDBClusterSpec) {
	fetchedCluster := &fdbv1beta2.FoundationDBCluster{}

	// This is flaky. It sometimes responds with an error saying that the object has been updated.
	// Try a few times before giving up.
	gomega.Eventually(func(g gomega.Gomega) bool {
		err := fdbCluster.getClient().
			Get(ctx.Background(), client.ObjectKeyFromObject(fdbCluster.cluster), fetchedCluster)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "error fetching cluster")

		specUpdated := equality.Semantic.DeepEqual(fetchedCluster.Spec, *desiredSpec)
		log.Println("UpdateClusterSpec: specUpdated:", specUpdated)
		if specUpdated {
			return true
		}

		desiredSpec.DeepCopyInto(&fetchedCluster.Spec)
		err = fdbCluster.getClient().Update(ctx.Background(), fetchedCluster)
		g.Expect(err).NotTo(gomega.HaveOccurred(), "error updating cluster spec")
		// Retry here and let the method fetch the latest version of the cluster again until the spec is updated.
		return false
	}).WithTimeout(10 * time.Minute).WithPolling(1 * time.Second).Should(gomega.BeTrue())

	fdbCluster.cluster = fetchedCluster
}

// GetAllPods returns all pods, even if not running.
func (fdbCluster *FdbCluster) GetAllPods() *corev1.PodList {
	podList := &corev1.PodList{}

	gomega.Eventually(func() error {
		return fdbCluster.getClient().
			List(ctx.TODO(), podList, client.MatchingLabels(fdbCluster.cluster.GetMatchLabels()))
	}).WithTimeout(1 * time.Minute).WithPolling(1 * time.Second).ShouldNot(gomega.HaveOccurred())

	return podList
}

// GetPods returns only running Pods.
func (fdbCluster *FdbCluster) GetPods() *corev1.PodList {
	podList := &corev1.PodList{}

	gomega.Eventually(func() error {
		return fdbCluster.getClient().List(ctx.TODO(), podList,
			client.InNamespace(fdbCluster.Namespace()),
			client.MatchingLabels(fdbCluster.cluster.GetMatchLabels()),
			client.MatchingFields(map[string]string{"status.phase": string(corev1.PodRunning)}),
		)
	}).WithTimeout(1 * time.Minute).WithPolling(1 * time.Second).ShouldNot(gomega.HaveOccurred())

	return podList
}

// GetPodsNames GetS all Running Pods and return their names.
func (fdbCluster *FdbCluster) GetPodsNames() []string {
	results := make([]string, 0)
	podList := fdbCluster.GetPods()

	for _, pod := range podList.Items {
		results = append(results, pod.Name)
	}

	return results
}

func (fdbCluster *FdbCluster) getPodsByProcessClass(
	processClass fdbv1beta2.ProcessClass,
) *corev1.PodList {
	podList := &corev1.PodList{}

	gomega.Eventually(func() error {
		return fdbCluster.getClient().List(ctx.TODO(), podList,
			client.InNamespace(fdbCluster.Namespace()),
			client.MatchingLabels(map[string]string{
				fdbv1beta2.FDBClusterLabel:      fdbCluster.cluster.Name,
				fdbv1beta2.FDBProcessClassLabel: string(processClass)}))
	}).WithTimeout(1 * time.Minute).WithPolling(1 * time.Second).ShouldNot(gomega.HaveOccurred())

	return podList
}

// GetLogPods returns all Pods of this cluster that have the process class log.
func (fdbCluster *FdbCluster) GetLogPods() *corev1.PodList {
	return fdbCluster.getPodsByProcessClass(fdbv1beta2.ProcessClassLog)
}

// GetStatelessPods returns all Pods of this cluster that have the process class stateless.
func (fdbCluster *FdbCluster) GetStatelessPods() *corev1.PodList {
	return fdbCluster.getPodsByProcessClass(fdbv1beta2.ProcessClassStateless)
}

// GetStoragePods returns all Pods of this cluster that have the process class storage.
func (fdbCluster *FdbCluster) GetStoragePods() *corev1.PodList {
	return fdbCluster.getPodsByProcessClass(fdbv1beta2.ProcessClassStorage)
}

// GetTransactionPods returns all Pods of this cluster that have the process class transaction.
func (fdbCluster *FdbCluster) GetTransactionPods() *corev1.PodList {
	return fdbCluster.getPodsByProcessClass(fdbv1beta2.ProcessClassTransaction)
}

// GetPod returns the Pod with the given name that runs in the same namespace as the FoundationDBCluster.
func (fdbCluster *FdbCluster) GetPod(name string) *corev1.Pod {
	pod := &corev1.Pod{}
	// Retry if for some reasons an error is returned
	gomega.Eventually(func() error {
		return fdbCluster.getClient().
			Get(ctx.TODO(), client.ObjectKey{Name: name, Namespace: fdbCluster.Namespace()}, pod)
	}).WithTimeout(2 * time.Minute).WithPolling(1 * time.Second).ShouldNot(gomega.HaveOccurred())

	return pod
}

// GetPodIDs returns all the process group IDs for all Pods of this cluster that have the matching process class.
func (fdbCluster *FdbCluster) GetPodIDs(processClass fdbv1beta2.ProcessClass) map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None {
	pods := fdbCluster.GetPods()

	podIDs := make(map[fdbv1beta2.ProcessGroupID]fdbv1beta2.None, len(pods.Items))
	for _, pod := range pods.Items {
		if pod.GetLabels()[fdbv1beta2.FDBProcessClassLabel] != string(processClass) {
			continue
		}

		log.Println(pod.Name)

		podIDs[GetProcessGroupID(pod)] = fdbv1beta2.None{}
	}

	return podIDs
}

// GetVolumeClaimsForProcesses returns a list of volume claims belonging to this cluster and the specific process class.
func (fdbCluster *FdbCluster) GetVolumeClaimsForProcesses(
	processClass fdbv1beta2.ProcessClass,
) *corev1.PersistentVolumeClaimList {
	volumeClaimList := &corev1.PersistentVolumeClaimList{}
	gomega.Expect(
		fdbCluster.getClient().
			List(ctx.TODO(), volumeClaimList,
				client.InNamespace(fdbCluster.Namespace()),
				client.MatchingLabels(map[string]string{
					fdbv1beta2.FDBClusterLabel:      fdbCluster.cluster.Name,
					fdbv1beta2.FDBProcessClassLabel: string(processClass),
				})),
	).NotTo(gomega.HaveOccurred())

	return volumeClaimList
}

// GetLogServersPerPod returns the current expected Log server per pod.
func (fdbCluster *FdbCluster) GetLogServersPerPod() int {
	return fdbCluster.cluster.GetLogServersPerPod()
}

// SetLogServersPerPod set the LogServersPerPod field in the cluster spec.
func (fdbCluster *FdbCluster) SetLogServersPerPod(
	serverPerPod int,
	waitForReconcile bool,
) error {
	fdbCluster.cluster.Spec.LogServersPerPod = serverPerPod
	fdbCluster.UpdateClusterSpec()

	if !waitForReconcile {
		return nil
	}
	return fdbCluster.WaitForReconciliation()
}

// GetStorageServerPerPod returns the current expected storage server per pod.
func (fdbCluster *FdbCluster) GetStorageServerPerPod() int {
	return fdbCluster.cluster.GetStorageServersPerPod()
}

func (fdbCluster *FdbCluster) setStorageServerPerPod(
	serverPerPod int,
	waitForReconcile bool,
) error {
	fdbCluster.cluster.Spec.StorageServersPerPod = serverPerPod
	fdbCluster.UpdateClusterSpec()

	if !waitForReconcile {
		return nil
	}
	return fdbCluster.WaitForReconciliation()
}

// SetStorageServerPerPod set the SetStorageServerPerPod field in the cluster spec.
func (fdbCluster *FdbCluster) SetStorageServerPerPod(serverPerPod int) error {
	return fdbCluster.setStorageServerPerPod(serverPerPod, true)
}

// SetTransactionServerPerPod set the LogServersPerPod field in the cluster spec and changes log Pods to transaction Pods.
func (fdbCluster *FdbCluster) SetTransactionServerPerPod(
	serverPerPod int,
	processCount int,
	waitForReconcile bool,
) error {
	fdbCluster.cluster.Spec.LogServersPerPod = serverPerPod
	fdbCluster.cluster.Spec.ProcessCounts.Transaction = processCount
	fdbCluster.cluster.Spec.ProcessCounts.Log = 0
	fdbCluster.UpdateClusterSpec()

	if !waitForReconcile {
		return nil
	}
	return fdbCluster.WaitForReconciliation()
}

// ReplacePod replaces the provided Pod if it's part of the FoundationDBCluster.
func (fdbCluster *FdbCluster) ReplacePod(pod corev1.Pod, waitForReconcile bool) {
	fdbCluster.cluster.Spec.ProcessGroupsToRemove = []fdbv1beta2.ProcessGroupID{GetProcessGroupID(pod)}
	fdbCluster.UpdateClusterSpec()

	if !waitForReconcile {
		return
	}

	gomega.Expect(fdbCluster.WaitForReconciliation(SoftReconcileOption(true))).NotTo(gomega.HaveOccurred())
}

// ReplacePods replaces the provided Pods in the current FoundationDBCluster.
func (fdbCluster *FdbCluster) ReplacePods(pods []corev1.Pod, waitForReconcile bool) {
	for _, pod := range pods {
		fdbCluster.cluster.Spec.ProcessGroupsToRemove = append(
			fdbCluster.cluster.Spec.ProcessGroupsToRemove,
			GetProcessGroupID(pod),
		)
	}
	fdbCluster.UpdateClusterSpec()

	if !waitForReconcile {
		return
	}

	gomega.Expect(fdbCluster.WaitForReconciliation()).NotTo(gomega.HaveOccurred())
}

// ClearProcessGroupsToRemove clears the InstancesToRemove list in the cluster
// spec.
func (fdbCluster *FdbCluster) ClearProcessGroupsToRemove() error {
	fdbCluster.cluster.Spec.ProcessGroupsToRemove = nil
	fdbCluster.UpdateClusterSpec()
	return fdbCluster.WaitForReconciliation()
}

// SetVolumeSize updates the volume size for the specified process class.
func (fdbCluster *FdbCluster) SetVolumeSize(
	processClass fdbv1beta2.ProcessClass,
	size resource.Quantity,
) error {
	processSettings, ok := fdbCluster.cluster.Spec.Processes[processClass]
	if !ok || processSettings.VolumeClaimTemplate == nil {
		processSettings, ok = fdbCluster.cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral]
		if !ok {
			return fmt.Errorf("could not find process setting for %s", processClass)
		}
	}
	setting := fdbCluster.cluster.Spec.Processes[processClass]
	// Set the new volume claim template
	if processSettings.VolumeClaimTemplate == nil {
		setting.VolumeClaimTemplate = &corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: size,
					},
				},
			},
		}
	} else {
		setting.VolumeClaimTemplate = processSettings.VolumeClaimTemplate.DeepCopy()
		setting.VolumeClaimTemplate.Spec.Resources.Requests[corev1.ResourceStorage] = size
	}
	fdbCluster.cluster.Spec.Processes[processClass] = setting
	fdbCluster.UpdateClusterSpec()
	return fdbCluster.WaitForReconciliation()
}

// GetVolumeSize returns the volume size for the specified process class.
func (fdbCluster *FdbCluster) GetVolumeSize(
	processClass fdbv1beta2.ProcessClass,
) (resource.Quantity, error) {
	processSettings, ok := fdbCluster.cluster.Spec.Processes[processClass]
	if !ok || processSettings.VolumeClaimTemplate == nil {
		processSettings, ok = fdbCluster.cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral]
		if !ok || processSettings.VolumeClaimTemplate == nil {
			return resource.MustParse("128G"), nil
		}
	}
	return processSettings.VolumeClaimTemplate.Spec.Resources.Requests[corev1.ResourceStorage], nil
}

func (fdbCluster *FdbCluster) updateLogProcessCount(
	newLogProcessCount int,
	waitForReconcile bool,
) error {
	fdbCluster.cluster.Spec.ProcessCounts.Log = newLogProcessCount
	fdbCluster.UpdateClusterSpec()
	if !waitForReconcile {
		return nil
	}
	return fdbCluster.WaitForReconciliation()
}

// UpdateLogProcessCount updates the log process count in the cluster spec.
func (fdbCluster *FdbCluster) UpdateLogProcessCount(newLogProcessCount int) error {
	return fdbCluster.updateLogProcessCount(newLogProcessCount, true)
}

// SetPodAsUnschedulable sets the provided Pod on the NoSchedule list of the current FoundationDBCluster. This will make
// sure that the Pod is stuck in Pending.
func (fdbCluster *FdbCluster) SetPodAsUnschedulable(pod corev1.Pod) error {
	fdbCluster.SetProcessGroupsAsUnschedulable([]fdbv1beta2.ProcessGroupID{GetProcessGroupID(pod)})

	fetchedPod := &corev1.Pod{}
	return wait.PollImmediate(2*time.Second, 5*time.Minute, func() (bool, error) {
		err := fdbCluster.getClient().
			Get(ctx.Background(), client.ObjectKeyFromObject(&pod), fetchedPod)
		if err != nil {
			if kubeErrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		// Try deleting the Pod as a workaround until the operator handle all cases.
		if fetchedPod.Spec.NodeName != "" && fetchedPod.DeletionTimestamp.IsZero() {
			_ = fdbCluster.getClient().Delete(ctx.Background(), &pod)
		}

		return fetchedPod.Spec.NodeName == "", nil
	})
}

// SetProcessGroupsAsUnschedulable sets the provided process groups on the NoSchedule list of the current FoundationDBCluster. This will make
// sure that the Pod is stuck in Pending.
func (fdbCluster *FdbCluster) SetProcessGroupsAsUnschedulable(processGroups []fdbv1beta2.ProcessGroupID) {
	fdbCluster.cluster.Spec.Buggify.NoSchedule = processGroups
	fdbCluster.UpdateClusterSpec()
}

// ClearBuggifyNoSchedule this will reset the NoSchedule setting for the current FoundationDBCluster.
func (fdbCluster *FdbCluster) ClearBuggifyNoSchedule(waitForReconcile bool) error {
	fdbCluster.cluster.Spec.Buggify.NoSchedule = nil
	fdbCluster.UpdateClusterSpec()

	if !waitForReconcile {
		return nil
	}

	return fdbCluster.WaitForReconciliation()
}

func (fdbCluster *FdbCluster) setPublicIPSource(
	publicIPSource fdbv1beta2.PublicIPSource,
	waitForReconcile bool,
) error {
	fdbCluster.cluster.Spec.Routing.PublicIPSource = &publicIPSource
	fdbCluster.UpdateClusterSpec()
	if !waitForReconcile {
		return nil
	}
	return fdbCluster.WaitForReconciliation()
}

// SetTLS will enabled or disable the TLS setting in the current FoundationDBCluster.
func (fdbCluster *FdbCluster) SetTLS(
	enableMainContainerTLS bool,
	enableSidecarContainerTLS bool,
) error {
	fdbCluster.cluster.Spec.MainContainer.EnableTLS = enableMainContainerTLS
	fdbCluster.cluster.Spec.SidecarContainer.EnableTLS = enableSidecarContainerTLS
	fdbCluster.UpdateClusterSpec()
	return fdbCluster.WaitForReconciliation()
}

// SetPublicIPSource will set the public IP source of the current FoundationDBCluster to the provided IP source.
func (fdbCluster *FdbCluster) SetPublicIPSource(publicIPSource fdbv1beta2.PublicIPSource) error {
	return fdbCluster.setPublicIPSource(publicIPSource, true)
}

// GetServices returns the services associated with the current FoundationDBCluster.
func (fdbCluster *FdbCluster) GetServices() *corev1.ServiceList {
	serviceList := &corev1.ServiceList{}
	gomega.Expect(
		fdbCluster.getClient().List(
			ctx.TODO(),
			serviceList,
			client.InNamespace(fdbCluster.Namespace()),
			client.MatchingLabels(fdbCluster.GetResourceLabels())),
	).NotTo(gomega.HaveOccurred())

	return serviceList
}

// SetAutoReplacements will enabled or disable the auto replacement feature and allows to specify the detection time for a replacement.
func (fdbCluster *FdbCluster) SetAutoReplacements(enabled bool, detectionTime time.Duration) error {
	return fdbCluster.SetAutoReplacementsWithWait(enabled, detectionTime, true)
}

// SetAutoReplacementsWithWait set the auto replacement setting on the operator and only waits for the cluster to reconcile
// if wait is set to true.
func (fdbCluster *FdbCluster) SetAutoReplacementsWithWait(
	enabled bool,
	detectionTime time.Duration,
	wait bool,
) error {
	detectionTimeSec := int(detectionTime.Seconds())
	fdbCluster.cluster.Spec.AutomationOptions.Replacements.Enabled = &enabled
	fdbCluster.cluster.Spec.AutomationOptions.Replacements.FailureDetectionTimeSeconds = &detectionTimeSec
	fdbCluster.UpdateClusterSpec()

	if !wait {
		return nil
	}

	return fdbCluster.WaitForReconciliation()
}

// UpdateCoordinatorSelection allows to update the coordinator selection for the current FoundationDBCluster.
func (fdbCluster *FdbCluster) UpdateCoordinatorSelection(
	setting []fdbv1beta2.CoordinatorSelectionSetting,
) error {
	fdbCluster.cluster.Spec.CoordinatorSelection = setting
	fdbCluster.UpdateClusterSpec()
	return fdbCluster.WaitForReconciliation()
}

// SetProcessGroupPrefix will set the process group prefix setting.
func (fdbCluster *FdbCluster) SetProcessGroupPrefix(prefix string) error {
	fdbCluster.cluster.Spec.ProcessGroupIDPrefix = prefix
	fdbCluster.UpdateClusterSpec()
	return fdbCluster.WaitForReconciliation()
}

// SetSkipReconciliation will set the skip setting for the current FoundationDBCluster. This setting will make sure that
// the operator is not taking any actions on this cluster.
func (fdbCluster *FdbCluster) SetSkipReconciliation(skip bool) {
	fdbCluster.cluster.Spec.Skip = skip
	// Skip wait for reconciliation since this spec update is in the operator itself and by setting it, the operator
	// skips reconciliation.
	fdbCluster.UpdateClusterSpec()
}

// WaitForPodRemoval will wait until the specified Pod is deleted.
func (fdbCluster *FdbCluster) WaitForPodRemoval(pod *corev1.Pod) {
	if pod == nil {
		return
	}

	log.Printf("waiting until the pod %s/%s is deleted", pod.Namespace, pod.Name)
	counter := 0
	forceReconcile := 10
	errDescription := fmt.Sprintf("pod %s/%s was not removed in the expected time", pod.Namespace, pod.Name)
	fetchedPod := &corev1.Pod{}
	gomega.Eventually(func() bool {
		err := fdbCluster.getClient().
			Get(ctx.Background(), client.ObjectKeyFromObject(pod), fetchedPod)
		if err != nil && kubeErrors.IsNotFound(err) {
			return true
		}

		// If the UID of the fetched Pod is different from the UID of the initial Pod we can assume
		// that the Pod was recreated e.g. by the operator.
		if fetchedPod != nil && fetchedPod.UID != pod.UID {
			return true
		}

		resCluster := fdbCluster.GetCluster()
		// We have to force a reconcile because the operator only reacts to events.
		// The network partition of the Pod won't trigger any reconcile and we would have to wait for 10h.
		if counter >= forceReconcile {
			patch := client.MergeFrom(resCluster.DeepCopy())
			if resCluster.Annotations == nil {
				resCluster.Annotations = make(map[string]string)
			}
			resCluster.Annotations["foundationdb.org/reconcile"] = strconv.FormatInt(
				time.Now().UnixNano(),
				10,
			)
			// This will apply an Annotation to the object which will trigger the reconcile loop.
			// This should speed up the reconcile phase.
			_ = fdbCluster.getClient().Patch(
				ctx.Background(),
				resCluster,
				patch)
			counter = -1
		}
		counter++

		return false
	}).WithPolling(2*time.Second).WithTimeout(10*time.Minute).Should(gomega.BeTrue(), errDescription)
}

// GetClusterSpec returns the current cluster spec.
func (fdbCluster *FdbCluster) GetClusterSpec() fdbv1beta2.FoundationDBClusterSpec {
	// Ensure we fetch the latest state to ensure we return the latest spec and not a cached state.
	_ = fdbCluster.GetCluster()
	return fdbCluster.cluster.Spec
}

// BounceClusterWithoutWait will restart all fdberver processes in the current FoundationDBCluster without waiting for the
// cluster to become available again.
func (fdbCluster *FdbCluster) BounceClusterWithoutWait() error {
	var retries int
	var err error

	// We try to execute the bounce command 5 times
	for retries < 5 {
		_, _, err = fdbCluster.RunFdbCliCommandInOperatorWithoutRetry(
			"kill; kill all; sleep 5",
			true,
			30,
		)
		if err != nil {
			log.Println(err)
			retries++
			continue
		}

		return nil
	}

	return err
}

// SetFinalizerForPvc allows to set the finalizers for the provided PVC.
func (fdbCluster *FdbCluster) SetFinalizerForPvc(
	finalizers []string,
	pvc corev1.PersistentVolumeClaim,
) error {
	patch := client.MergeFrom(pvc.DeepCopy())
	pvc.SetFinalizers(finalizers)
	return fdbCluster.getClient().Patch(ctx.Background(), &pvc, patch)
}

// UpdateStorageClass this will set the StorageClass for the provided process class of the current FoundationDBCluster.
func (fdbCluster *FdbCluster) UpdateStorageClass(
	storageClass string,
	processClass fdbv1beta2.ProcessClass,
) error {
	log.Println("Updating storage class for", processClass, "to", storageClass)
	resCluster := fdbCluster.GetCluster()
	patch := client.MergeFrom(resCluster.DeepCopy())
	resCluster.Spec.Processes[processClass].VolumeClaimTemplate.Spec.StorageClassName = &storageClass
	_ = fdbCluster.getClient().Patch(ctx.Background(), resCluster, patch)
	return fdbCluster.WaitForReconciliation()
}

// UpgradeCluster will upgrade the cluster to the specified version. If waitForReconciliation is set to true this method will
// block until the cluster is fully upgraded and all Pods are running the new image version.
func (fdbCluster *FdbCluster) UpgradeCluster(version string, waitForReconciliation bool) error {
	// Ensure we have pulled that latest state of the cluster.
	_ = fdbCluster.GetCluster()

	log.Printf(
		"Upgrading cluster from version %s to version %s",
		fdbCluster.cluster.Spec.Version,
		version,
	)

	fdbCluster.cluster.Spec.Version = version
	log.Println("Spec version", fdbCluster.cluster.Spec.Version)
	fdbCluster.UpdateClusterSpec()
	// Ensure the version is actually upgraded.
	gomega.Expect(fdbCluster.cluster.Spec.Version).To(gomega.Equal(version))

	if waitForReconciliation {
		log.Println("Waiting for generation:", fdbCluster.cluster.Generation)
		return fdbCluster.WaitForReconciliation(MinimumGenerationOption(fdbCluster.cluster.Generation))
	}

	return nil
}

// SetEmptyMonitorConf sets the buggify option EmptyMonitorConf for the current FoundationDBCluster.
func (fdbCluster *FdbCluster) SetEmptyMonitorConf(enable bool) error {
	fdbCluster.cluster.Spec.Buggify.EmptyMonitorConf = enable
	fdbCluster.UpdateClusterSpec()

	if !enable {
		err := fdbCluster.WaitForReconciliation()
		if err != nil {
			return fmt.Errorf(
				"disabling empty monitor failed in cluster %s: %w",
				fdbCluster.Name(),
				err,
			)
		}
		log.Printf("Disabling empty monitor succeeded in cluster: %s", fdbCluster.Name())
		return nil
	}
	// Don't wait for reconciliation when we set empty monitor config to true since the cluster won't reconcile
	pods := fdbCluster.GetPods().Items
	podMap := sync.Map{}

	g := new(errgroup.Group)
	for _, pod := range pods {
		targetPod := pod // https://golang.org/doc/faq#closures_and_goroutines
		podMap.Store(targetPod.Name, struct{}{})

		g.Go(func() error {
			err := wait.PollImmediate(2*time.Second, 5*time.Minute, func() (bool, error) {
				output, _, err := fdbCluster.ExecuteCmdOnPod(
					targetPod,
					fdbv1beta2.MainContainerName,
					"ps -e | grep fdbserver | wc -l",
					false,
				)
				if err != nil {
					log.Printf(
						"error executing command on %s, error: %s\n",
						targetPod.Name,
						err.Error(),
					)
					return false, nil
				}

				// If EmptyMonitor is enabled, each pod should has no fdbserver running
				if strings.TrimSpace(output) == "0" {
					podMap.Delete(targetPod.Name)
					return true, nil
				}

				return false, nil
			})

			return err
		})
	}

	err := g.Wait()
	if err != nil {
		return err
	}

	var failedPods strings.Builder
	podMap.Range(func(key any, _ any) bool {
		podName, ok := key.(string)
		if !ok {
			return false
		}
		failedPods.WriteString(podName)
		failedPods.WriteString(" ")

		return true
	})
	if failedPods.Len() > 0 {
		return fmt.Errorf("enabling empty monitor failed on pods: %s", failedPods.String())
	}

	log.Printf("Enabling empty monitor succeeded in cluster: %s", fdbCluster.Name())

	return nil
}

// SetClusterTaintConfig set fdbCluster's TaintReplacementOptions
func (fdbCluster *FdbCluster) SetClusterTaintConfig(taintOption []fdbv1beta2.TaintReplacementOption, taintReplacementTimeSeconds *int) {
	curClusterSpec := fdbCluster.GetCluster().Spec.DeepCopy()
	curClusterSpec.AutomationOptions.Replacements.TaintReplacementOptions = taintOption
	curClusterSpec.AutomationOptions.Replacements.TaintReplacementTimeSeconds = taintReplacementTimeSeconds
	fdbCluster.UpdateClusterSpecWithSpec(curClusterSpec)
}

// GetProcessCounts returns the process counts of the current FoundationDBCluster.
func (fdbCluster *FdbCluster) GetProcessCounts() (fdbv1beta2.ProcessCounts, error) {
	return fdbCluster.cluster.GetProcessCountsWithDefaults()
}

// HasHeadlessService returns true if the cluster has a headless service.
func (fdbCluster *FdbCluster) HasHeadlessService() bool {
	return fdbCluster.cluster.NeedsHeadlessService()
}

// SetCustomParameters allows to set the custom parameters of the provided process class.
func (fdbCluster *FdbCluster) SetCustomParameters(
	processClass fdbv1beta2.ProcessClass,
	customParameters fdbv1beta2.FoundationDBCustomParameters,
	waitForReconcile bool,
) error {
	setting, ok := fdbCluster.cluster.Spec.Processes[processClass]
	if !ok {
		return fmt.Errorf("could not find process settings for process class %s", processClass)
	}
	setting.CustomParameters = customParameters

	fdbCluster.cluster.Spec.Processes[processClass] = setting
	fdbCluster.UpdateClusterSpec()
	if !waitForReconcile {
		return nil
	}

	return fdbCluster.WaitForReconciliation()
}

// GetCustomParameters returns the current custom parameters for the specified process class.
func (fdbCluster *FdbCluster) GetCustomParameters(
	processClass fdbv1beta2.ProcessClass,
) fdbv1beta2.FoundationDBCustomParameters {
	return fdbCluster.cluster.Spec.Processes[processClass].CustomParameters
}

// SetPodTemplateSpec allows to set the pod template spec of the provided process class.
func (fdbCluster *FdbCluster) SetPodTemplateSpec(
	processClass fdbv1beta2.ProcessClass,
	podTemplateSpec *corev1.PodSpec,
	waitForReconcile bool,
) error {
	setting, ok := fdbCluster.cluster.Spec.Processes[processClass]
	if !ok {
		return fmt.Errorf("could not find process settings for process class %s", processClass)
	}
	setting.PodTemplate.Spec = *podTemplateSpec

	fdbCluster.cluster.Spec.Processes[processClass] = setting
	fdbCluster.UpdateClusterSpec()
	if !waitForReconcile {
		return nil
	}

	return fdbCluster.WaitForReconciliation()
}

// GetPodTemplateSpec returns the current pod template spec for the specified process class.
func (fdbCluster *FdbCluster) GetPodTemplateSpec(
	processClass fdbv1beta2.ProcessClass,
) *corev1.PodSpec {
	if classSpec, ok := fdbCluster.cluster.Spec.Processes[processClass]; ok {
		return &classSpec.PodTemplate.Spec
	}
	if generalSpec, ok := fdbCluster.cluster.Spec.Processes[fdbv1beta2.ProcessClassGeneral]; ok {
		return &generalSpec.PodTemplate.Spec
	}
	return nil
}

// CheckPodIsDeleted return true if Pod no longer exists at the executed time point
func (fdbCluster *FdbCluster) CheckPodIsDeleted(podName string) bool {
	pod := &corev1.Pod{}
	err := fdbCluster.getClient().
		Get(ctx.TODO(), client.ObjectKey{Namespace: fdbCluster.Namespace(), Name: podName}, pod)

	if err != nil {
		if kubeErrors.IsNotFound(err) {
			return true
		}
	}

	return !pod.DeletionTimestamp.IsZero()
}

// EnsurePodIsDeletedWithCustomTimeout validates that a Pod is either not existing or is marked as deleted with a non-zero deletion timestamp.
// It times out after timeoutMinutes.
func (fdbCluster *FdbCluster) EnsurePodIsDeletedWithCustomTimeout(podName string, timeoutMinutes int) {
	gomega.Eventually(func() bool {
		return fdbCluster.CheckPodIsDeleted(podName)
	}).WithTimeout(time.Duration(timeoutMinutes) * time.Minute).WithPolling(1 * time.Second).Should(gomega.BeTrue())
}

// EnsurePodIsDeleted validates that a Pod is either not existing or is marked as deleted with a non-zero deletion timestamp.
func (fdbCluster *FdbCluster) EnsurePodIsDeleted(podName string) {
	fdbCluster.EnsurePodIsDeletedWithCustomTimeout(podName, 5)
}

// SetUseDNSInClusterFile enables DNS in the cluster file. Enable this setting to use DNS instead of IP addresses in
// the connection string.
func (fdbCluster *FdbCluster) SetUseDNSInClusterFile(useDNSInClusterFile bool) error {
	fdbCluster.cluster.Spec.Routing.UseDNSInClusterFile = pointer.Bool(useDNSInClusterFile)
	fdbCluster.UpdateClusterSpec()
	return fdbCluster.WaitForReconciliation()
}

// Destroy will remove the underlying cluster.
func (fdbCluster *FdbCluster) Destroy() error {
	return fdbCluster.getClient().
		Delete(ctx.Background(), fdbCluster.cluster)
}

// SetIgnoreMissingProcessesSeconds sets the IgnoreMissingProcessesSeconds setting.
func (fdbCluster *FdbCluster) SetIgnoreMissingProcessesSeconds(duration time.Duration) {
	fdbCluster.cluster.Spec.AutomationOptions.IgnoreMissingProcessesSeconds = pointer.Int(
		int(duration.Seconds()),
	)
	fdbCluster.UpdateClusterSpec()
}

// SetKillProcesses sets the automation option to allow the operator to restart processes or not.
func (fdbCluster *FdbCluster) SetKillProcesses(allowKill bool) {
	fdbCluster.cluster.Spec.AutomationOptions.KillProcesses = pointer.Bool(allowKill)
	fdbCluster.UpdateClusterSpec()
	gomega.Expect(fdbCluster.WaitForReconciliation()).NotTo(gomega.HaveOccurred())
}

// AllProcessGroupsHaveCondition returns true if all process groups have the specified condition. If allowOtherConditions is
// set to true only this condition is allowed.
func (fdbCluster *FdbCluster) AllProcessGroupsHaveCondition(
	condition fdbv1beta2.ProcessGroupConditionType,
) bool {
	cluster := fdbCluster.GetCluster()

	for _, processGroup := range cluster.Status.ProcessGroups {
		if processGroup.IsMarkedForRemoval() {
			continue
		}

		if len(processGroup.ProcessGroupConditions) != 1 {
			return false
		}

		if processGroup.GetConditionTime(condition) == nil {
			return false
		}
	}

	return true
}

// SetCrashLoopContainers sets the crashLoopContainers of the FoundationDBCluster spec.
func (fdbCluster *FdbCluster) SetCrashLoopContainers(
	crashLoopContainers []fdbv1beta2.CrashLoopContainerObject,
	waitForReconcile bool,
) {
	fdbCluster.cluster.Spec.Buggify.CrashLoopContainers = crashLoopContainers
	fdbCluster.UpdateClusterSpec()
	if !waitForReconcile {
		return
	}
	gomega.Expect(fdbCluster.WaitForReconciliation()).NotTo(gomega.HaveOccurred())
}

// SetIgnoreDuringRestart sets the buggify option for the operator.
func (fdbCluster *FdbCluster) SetIgnoreDuringRestart(processes []fdbv1beta2.ProcessGroupID) {
	fdbCluster.cluster.Spec.Buggify.IgnoreDuringRestart = processes
	fdbCluster.UpdateClusterSpec()
	gomega.Expect(fdbCluster.WaitForReconciliation()).NotTo(gomega.HaveOccurred())
}

// UpdateContainerImage sets the image for the provided Pod for the porvided container.
func (fdbCluster *FdbCluster) UpdateContainerImage(pod *corev1.Pod, containerName string, image string) {
	for idx, container := range pod.Spec.Containers {
		if container.Name != containerName {
			continue
		}

		pod.Spec.Containers[idx].Image = image
	}

	gomega.Expect(fdbCluster.factory.GetControllerRuntimeClient().Update(ctx.Background(), pod)).NotTo(gomega.HaveOccurred())
}

// SetBuggifyBlockRemoval will set the provided list of process group IDs to be blocked for removal.
func (fdbCluster *FdbCluster) SetBuggifyBlockRemoval(blockRemovals []fdbv1beta2.ProcessGroupID) {
	fdbCluster.cluster.Spec.Buggify.BlockRemoval = blockRemovals
	fdbCluster.UpdateClusterSpec()
}

// GetAutomationOptions return the fdbCluster's AutomationOptions
func (fdbCluster *FdbCluster) GetAutomationOptions() fdbv1beta2.FoundationDBClusterAutomationOptions {
	return fdbCluster.cluster.Spec.AutomationOptions
}

// ValidateProcessesCount will make sure that the cluster has the expected count of ProcessGroups and processes running
// with the provided ProcessClass.
func (fdbCluster *FdbCluster) ValidateProcessesCount(
	processClass fdbv1beta2.ProcessClass,
	countProcessGroups int,
	countServer int,
) {
	gomega.Eventually(func() int {
		var cnt int
		for _, processGroup := range fdbCluster.GetCluster().Status.ProcessGroups {
			if processGroup.ProcessClass != processClass {
				continue
			}

			if processGroup.IsMarkedForRemoval() {
				continue
			}

			cnt++
		}

		return cnt
	}).Should(gomega.BeNumerically("==", countProcessGroups))

	gomega.Eventually(func() int {
		return fdbCluster.GetProcessCountByProcessClass(processClass)
	}).Should(gomega.BeNumerically("==", countServer))

	// Make sure that all process group have a fault domain set.
	for _, processGroup := range fdbCluster.GetCluster().Status.ProcessGroups {
		gomega.Expect(processGroup.FaultDomain).NotTo(gomega.BeEmpty())
	}
}

// UpdateAnnotationsAndLabels will update the annotations and labels to the provided values.
// Example usage:
/*
	annotations := fdbCluster.GetCachedCluster().GetAnnotations()
	if annotations == nil {
	   annotations = map[string]string{}
	}

	annotations["foundationdb.org/testing"] = "awesome"
	labels := fdbCluster.GetCachedCluster().GetLabels()
	fdbCluster.UpdateAnnotationsAndLabels(annotations, labels)

*/
func (fdbCluster *FdbCluster) UpdateAnnotationsAndLabels(annotations map[string]string, labels map[string]string) {
	// Update the annotations and labels.
	gomega.Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
		fetchedCluster := &fdbv1beta2.FoundationDBCluster{}
		err := fdbCluster.getClient().
			Get(ctx.Background(), client.ObjectKeyFromObject(fdbCluster.cluster), fetchedCluster)
		if err != nil {
			return err
		}

		patch := client.MergeFrom(fetchedCluster.DeepCopy())
		fetchedCluster.Annotations = annotations
		fetchedCluster.Labels = labels

		return fdbCluster.getClient().Patch(
			ctx.Background(),
			fetchedCluster,
			patch)
	})).NotTo(gomega.HaveOccurred())

	// Make sure the current reference is updated.
	fdbCluster.GetCluster()
}

// VerifyVersion Checks if cluster is running at the expectedVersion. This is done by checking the status of the FoundationDBCluster status.
// Before that we checked the cluster status json by checking the reported version of all processes. This approach only worked for
// version compatible upgrades, since incompatible processes won't be part of the cluster anyway. To simplify the check
// we verify the reported running version from the operator.
func (fdbCluster *FdbCluster) VerifyVersion(version string) {
	gomega.Expect(fdbCluster.WaitUntilWithForceReconcile(2, 600, func(cluster *fdbv1beta2.FoundationDBCluster) bool {
		return cluster.Status.RunningVersion == version
	})).NotTo(gomega.HaveOccurred())
}

// UpgradeAndVerify will upgrade the cluster to the new version and perform a check at the end that the running version
// matched the new version.
func (fdbCluster *FdbCluster) UpgradeAndVerify(version string) {
	startTime := time.Now()
	defer func() {
		log.Println("Upgrade took:", time.Since(startTime).String())
	}()

	gomega.Expect(fdbCluster.UpgradeCluster(version, true)).NotTo(gomega.HaveOccurred())
	fdbCluster.VerifyVersion(version)
}

// EnsureTeamTrackersAreHealthy will check if the machine-readable status suggest that the team trackers are healthy
// and all data is present.
func (fdbCluster *FdbCluster) EnsureTeamTrackersAreHealthy() {
	gomega.Eventually(func() bool {
		for _, tracker := range fdbCluster.GetStatus().Cluster.Data.TeamTrackers {
			if !tracker.State.Healthy {
				return false
			}
		}

		return true
	}).WithTimeout(1 * time.Minute).WithPolling(1 * time.Second).MustPassRepeatedly(5).Should(gomega.BeTrue())
}

// EnsureTeamTrackersHaveMinReplicas will check if the machine-readable status suggest that the team trackers min_replicas
// match the expected replicas.
func (fdbCluster *FdbCluster) EnsureTeamTrackersHaveMinReplicas() {
	desiredFaultTolerance := fdbCluster.GetCachedCluster().DesiredFaultTolerance()
	gomega.Eventually(func() int {
		minReplicas := math.MaxInt
		for _, tracker := range fdbCluster.GetStatus().Cluster.Data.TeamTrackers {
			if minReplicas > tracker.State.MinReplicasRemaining {
				minReplicas = tracker.State.MinReplicasRemaining
			}
		}

		return minReplicas
	}).WithTimeout(1 * time.Minute).WithPolling(1 * time.Second).MustPassRepeatedly(5).Should(gomega.BeNumerically(">=", desiredFaultTolerance))
}

// GetListOfUIDsFromVolumeClaims will return of list of UIDs for the current volume claims for the provided processes class.
func (fdbCluster *FdbCluster) GetListOfUIDsFromVolumeClaims(processClass fdbv1beta2.ProcessClass) []types.UID {
	volumesClaims := fdbCluster.GetVolumeClaimsForProcesses(processClass)

	uids := make([]types.UID, 0, len(volumesClaims.Items))
	for _, volumeClaim := range volumesClaims.Items {
		uids = append(uids, volumeClaim.ObjectMeta.GetObjectMeta().GetUID())
	}

	return uids
}
