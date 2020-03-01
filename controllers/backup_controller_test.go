package controllers

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"golang.org/x/net/context"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
)

func reloadBackup(client client.Client, backup *fdbtypes.FoundationDBBackup) (int64, error) {
	generations, err := reloadBackupGenerations(client, backup)
	return generations.Reconciled, err
}

func reloadBackupGenerations(client client.Client, backup *fdbtypes.FoundationDBBackup) (fdbtypes.BackupGenerationStatus, error) {
	err := k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: backup.Namespace, Name: backup.Name}, backup)
	if err != nil {
		return fdbtypes.BackupGenerationStatus{}, err
	}
	return backup.Status.Generations, err
}

var _ = Describe("backup_controller", func() {
	var cluster *fdbtypes.FoundationDBCluster
	var backup *fdbtypes.FoundationDBBackup

	BeforeEach(func() {
		ClearMockAdminClients()
		cluster = createDefaultCluster()
		backup = createDefaultBackup(cluster)
	})

	Describe("Reconciliation", func() {
		var err error
		var originalVersion int64
		var generationGap int64
		var timeout time.Duration

		BeforeEach(func() {
			cluster.Spec.ConnectionString = ""
			err = k8sClient.Create(context.TODO(), cluster)
			Expect(err).NotTo(HaveOccurred())

			timeout = time.Second * 5
			Eventually(func() (int64, error) {
				generations, err := reloadClusterGenerations(k8sClient, cluster)
				return generations.Reconciled, err
			}, timeout).ShouldNot(Equal(int64(0)))
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Create(context.TODO(), backup)
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() (int64, error) {
				generations, err := reloadClusterGenerations(k8sClient, cluster)
				return generations.Reconciled, err
			}, timeout).ShouldNot(Equal(int64(0)))
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}, cluster)
			Expect(err).NotTo(HaveOccurred())

			originalVersion = backup.ObjectMeta.Generation

			generationGap = 1
		})

		JustBeforeEach(func() {
			Eventually(func() (int64, error) { return reloadBackup(k8sClient, backup) }, timeout).Should(Equal(originalVersion + generationGap))
			err = k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: backup.Namespace, Name: backup.Name}, cluster)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			cleanupCluster(cluster)
			cleanupBackup(backup)
		})

		Context("when reconciling a new backup", func() {
			BeforeEach(func() {
				generationGap = 0
			})

			It("should create the backup deployment", func() {
				deployment := &appsv1.Deployment{}
				deploymentName := fmt.Sprintf("%s-backup-agents", cluster.Name)

				err := k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: cluster.Namespace, Name: deploymentName}, deployment)
				Expect(err).NotTo(HaveOccurred())
				Expect(*deployment.Spec.Replicas).To(Equal(int32(3)))
				Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(Equal(fmt.Sprintf("foundationdb/foundationdb:%s", cluster.Spec.Version)))
			})
		})

		Context("with a backup agent count of 0", func() {
			BeforeEach(func() {
				backup.Spec.AgentCount = 0
				err = k8sClient.Update(context.TODO(), backup)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should set the default replica count", func() {
				deployments := &appsv1.DeploymentList{}
				Eventually(func() (int, error) {
					err := k8sClient.List(context.TODO(), deployments)
					return len(deployments.Items), err
				}, timeout).Should(Equal(1))
				Expect(*deployments.Items[0].Spec.Replicas).To(Equal(int32(2)))
			})
		})

		Context("with no backup agents", func() {
			BeforeEach(func() {
				backup.Spec.AgentCount = -1
				err = k8sClient.Update(context.TODO(), backup)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should remove the deployment", func() {
				deployments := &appsv1.DeploymentList{}
				Eventually(func() (int, error) {
					err := k8sClient.List(context.TODO(), deployments)
					return len(deployments.Items), err
				}, timeout).Should(Equal(0))
			})
		})
	})
})
