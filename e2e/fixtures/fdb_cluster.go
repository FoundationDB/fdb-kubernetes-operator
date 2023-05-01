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
	"strconv"
	"strings"
	"sync"
	"time"

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
	timeOutInSeconds        int64
	pollTimeInSeconds       int64
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
func TimeOutInSecondsOption(timeOutInSeconds int64) ReconciliationOption {
	return func(options *ReconciliationOptions) {
		options.timeOutInSeconds = timeOutInSeconds
	}
}

// PollTimeInSecondsOption defines the polling time for the reconciliation. If not set the default is 10 seconds
func PollTimeInSecondsOption(pollTimeInSeconds int64) ReconciliationOption {
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
		reconciliationOptions.timeOutInSeconds = 4800
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
	timeOutInSeconds int64, // 4800
	pollTimeInSeconds int64, // 4
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
	counter := 0
	// We want to force reconcile every 5 minutes, otherwise we update the cluster spec to often and we
	// introduce to many conflicts. This will be resolved once we move to server side apply see:
	// https://github.com/FoundationDB/fdb-kubernetes-operator/issues/1278
	forceReconcile := int(300 / pollTimeInSeconds)

	// Printout the initial state of the cluster before we moving forward waiting for the reconciliation.
	fdbCluster.factory.DumpState(fdbCluster)

	var creationTracker *fdbClusterCreationTracker
	if creationTrackerLogger != nil {
		creationTracker = newFdbClusterCreationTracker(
			fdbCluster.getClient(),
			creationTrackerLogger,
		)
	}

	err := wait.PollImmediate(
		time.Duration(pollTimeInSeconds)*time.Second,
		time.Duration(timeOutInSeconds)*time.Second,
		func() (bool, error) {
			resCluster := fdbCluster.GetCluster()
			if creationTracker != nil {
				creationTracker.trackProgress(resCluster)
			}

			var reconciled bool
			if softReconciliationAllowed {
				reconciled = resCluster.Status.Generations.Reconciled == resCluster.ObjectMeta.Generation
			} else {
				reconciled = resCluster.Status.Generations == fdbv1beta2.ClusterGenerationStatus{Reconciled: resCluster.ObjectMeta.Generation}
			}
			if minimumGeneration > 0 {
				reconciled = reconciled &&
					resCluster.Status.Generations.Reconciled >= minimumGeneration
			}
			// Healthy is a bad indicator, since the tester pods will leave old processes behind and the cluster thinks it's not healthy...
			if reconciled && resCluster.Status.Health.Available {
				log.Printf(
					"reconciled name=%s, namespace=%s",
					fdbCluster.cluster.Name,
					fdbCluster.cluster.Namespace,
				)
				return true, nil
			}
			// force a reconcile
			if counter >= forceReconcile {
				log.Printf("Status Generations=%s",
					ToJSON(resCluster.Status.Generations))
				log.Printf(
					"MedataData Generations=%s",
					ToJSON(
						fdbv1beta2.ClusterGenerationStatus{
							Reconciled: resCluster.ObjectMeta.Generation,
						},
					),
				)
				fdbCluster.factory.DumpState(fdbCluster)
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
				err := fdbCluster.getClient().Patch(
					ctx.Background(),
					resCluster,
					patch)
				if err != nil {
					log.Println("error patching annotation to force reconcile, error:", err.Error())
				}
				counter = -1
			}
			counter++
			return false, nil
		},
	)

	if creationTracker != nil {
		creationTracker.report()
	}

	return err
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

// UpdateClusterSpec ensures that the FoundationDBCluster will be updated in Kubernetes. This method has a retry mechanism
// implemented and ensures that the provided (local) Spec matches the Spec in Kubernetes.
func (fdbCluster *FdbCluster) UpdateClusterSpec() {
	fdbCluster.UpdateClusterSpecWithSpec(fdbCluster.cluster.Spec.DeepCopy())
}

// UpdateClusterSpecWithSpec ensures that the FoundationDBCluster will be updated in Kubernetes. This method as a retry mechanism
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
	gomega.Eventually(func() bool {
		err := fdbCluster.getClient().
			Get(ctx.Background(), client.ObjectKeyFromObject(fdbCluster.cluster), fetchedCluster)
		if err != nil {
			log.Println("UpdateClusterSpec: error fetching cluster:", err)
			return false
		}

		specUpdated := equality.Semantic.DeepEqual(fetchedCluster.Spec, *desiredSpec)
		log.Println("UpdateClusterSpec: specUpdated:", specUpdated)
		if specUpdated {
			return true
		}

		desiredSpec.DeepCopyInto(&fetchedCluster.Spec)
		err = fdbCluster.getClient().Update(ctx.Background(), fetchedCluster)
		if err != nil {
			log.Println("UpdateClusterSpec: error updating cluster:", err)
		}
		// Retry here and let the method fetch the latest version of the cluster again until the spec is updated.
		return false
	}).WithTimeout(5 * time.Minute).WithPolling(1 * time.Second).Should(gomega.BeTrue())

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
			List(ctx.TODO(), volumeClaimList, client.MatchingLabels(map[string]string{
				fdbv1beta2.FDBClusterLabel:      fdbCluster.cluster.Name,
				fdbv1beta2.FDBProcessClassLabel: string(processClass),
			})),
	).NotTo(gomega.HaveOccurred())

	return volumeClaimList
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
func (fdbCluster *FdbCluster) ReplacePods(pods []corev1.Pod) {
	for _, pod := range pods {
		fdbCluster.cluster.Spec.ProcessGroupsToRemove = append(
			fdbCluster.cluster.Spec.ProcessGroupsToRemove,
			GetProcessGroupID(pod),
		)
	}
	fdbCluster.UpdateClusterSpec()

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
	fdbCluster.cluster.Spec.Buggify.NoSchedule = []fdbv1beta2.ProcessGroupID{GetProcessGroupID(pod)}
	fdbCluster.UpdateClusterSpec()

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
		fdbCluster.getClient().
			List(ctx.TODO(), serviceList, client.MatchingLabels(fdbCluster.GetResourceLabels())),
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
func (fdbCluster *FdbCluster) SetSkipReconciliation(skip bool) error {
	fdbCluster.cluster.Spec.Skip = skip
	// Skip wait for reconciliation since this spec update is in the operator itself and by setting it, the operator
	// skips reconciliation.
	fdbCluster.UpdateClusterSpec()
	return nil
}

// WaitForPodRemoval will wait until the specified Pod is deleted.
func (fdbCluster *FdbCluster) WaitForPodRemoval(pod *corev1.Pod) error {
	log.Printf("waiting until the pod %s/%s is deleted", pod.Namespace, pod.Name)
	counter := 0
	forceReconcile := 10

	// Poll every 2 seconds for a maximum of 40 minutes.
	fetchedPod := &corev1.Pod{}
	return wait.PollImmediate(2*time.Second, 40*time.Minute, func() (bool, error) {
		err := fdbCluster.getClient().
			Get(ctx.Background(), client.ObjectKeyFromObject(pod), fetchedPod)
		if err != nil {
			if kubeErrors.IsNotFound(err) {
				return true, nil
			}
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
		return false, nil
	})
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

	waitGroup := sync.WaitGroup{}
	pods := fdbCluster.GetPods().Items
	waitGroup.Add(len(pods))
	podMap := make(map[string]struct{})
	var mu sync.Mutex
	var errorList []string
	for i := range pods {
		go func(i int) {
			defer waitGroup.Done()
			mu.Lock()
			podMap[pods[i].Name] = struct{}{}
			mu.Unlock()
			err := wait.PollImmediate(2*time.Second, 5*time.Minute, func() (bool, error) {
				output, _, err := fdbCluster.ExecuteCmdOnPod(
					pods[i],
					fdbv1beta2.MainContainerName,
					"ps -e | grep fdbserver | wc -l",
					false,
				)
				if err != nil {
					log.Printf(
						"error executing command on %s, error: %s\n",
						pods[i].Name,
						err.Error(),
					)
					return false, nil
				}
				fdbserver := strings.TrimSpace(output)
				// If EmptyMonitor is enabled, each pod should has no fdbserver running
				if fdbserver == "0" {
					mu.Lock()
					delete(podMap, pods[i].Name)
					mu.Unlock()
					return true, nil
				}
				return false, nil
			})
			if err != nil {
				mu.Lock()
				errorList = append(
					errorList,
					fmt.Sprintf("pod: %s, error: %s\n", pods[i].Name, err.Error()),
				)
				mu.Unlock()
			}
		}(i)
	}
	waitGroup.Wait()
	if len(errorList) > 0 {
		log.Printf("Issue running command on the pods: %v", errorList)
	}

	var failedPods strings.Builder
	if len(podMap) != 0 {
		for pod := range podMap {
			failedPods.WriteString(pod)
			failedPods.WriteString(" ")
		}
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

// CheckPodIsDeleted return true if Pod no longer exists at the executed time point
func (fdbCluster *FdbCluster) CheckPodIsDeleted(podName string) bool {
	pod := &corev1.Pod{}
	err := fdbCluster.getClient().
		Get(ctx.TODO(), client.ObjectKey{Namespace: fdbCluster.Namespace(), Name: podName}, pod)

	log.Println("error: ", err, "pod", pod.ObjectMeta)
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
		pod := &corev1.Pod{}
		err := fdbCluster.getClient().
			Get(ctx.TODO(), client.ObjectKey{Namespace: fdbCluster.Namespace(), Name: podName}, pod)

		log.Println("error: ", err, "pod", pod.ObjectMeta)
		if err != nil {
			return kubeErrors.IsNotFound(err)
		}

		// For our case it's enough to validate that the Pod is marked for deletion.
		return !pod.DeletionTimestamp.IsZero()
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
func (fdbCluster *FdbCluster) SetIgnoreMissingProcessesSeconds(duration time.Duration) error {
	fdbCluster.cluster.Spec.AutomationOptions.IgnoreMissingProcessesSeconds = pointer.Int(
		int(duration.Seconds()),
	)
	fdbCluster.UpdateClusterSpec()
	return fdbCluster.WaitForReconciliation()
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
