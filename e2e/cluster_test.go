package e2e

import (
	"context"
	"io"
	"log"
	"math/rand"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/labels"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	controllerRuntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyz123456789")
var failed = false

// RandStringRunes randomly generates a string of length n
func randStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

func createNamespace(kubeClient *kubernetes.Clientset, namespace string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
			Labels: map[string]string{
				"foundationdb.org/testing": "ci",
			},
		},
	}

	_, err := kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{FieldManager: "foundationdb-ci"})
	return err
}

var _ = AfterSuite(func() {
	if !failed {
		return
	}

	config, err := controllerRuntime.GetConfig()
	Expect(err).NotTo(HaveOccurred())
	kubeClient, err := kubernetes.NewForConfig(config)
	Expect(err).NotTo(HaveOccurred())

	operatorPod, err := kubeClient.CoreV1().Pods("default").List(context.Background(), metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{
			"app": "fdb-kubernetes-operator-controller-manager",
		}).String()},
	)
	Expect(err).NotTo(HaveOccurred())
	Expect(len(operatorPod.Items)).To(BeNumerically("==", 1))
	var sinceSeconds int64 = 300
	podLogRequest := kubeClient.CoreV1().Pods("default").GetLogs(operatorPod.Items[0].Name, &corev1.PodLogOptions{
		Follow:       false,
		SinceSeconds: &sinceSeconds,
	})
	stream, err := podLogRequest.Stream(context.TODO())
	Expect(err).NotTo(HaveOccurred())
	defer stream.Close()

	for {
		buf := make([]byte, 10000)
		numBytes, err := stream.Read(buf)
		if numBytes == 0 || err == io.EOF {
			break
		}
		Expect(err).NotTo(HaveOccurred())
		log.Println(string(buf[:numBytes]))
	}
})

var _ = Describe("[e2e] cluster tests", func() {
	var namespace string
	var runtimeClient client.Client
	var kubeClient *kubernetes.Clientset

	BeforeEach(func() {
		rand.Seed(time.Now().UnixNano())
		config, err := controllerRuntime.GetConfig()
		Expect(err).NotTo(HaveOccurred())
		kubeClient, err = kubernetes.NewForConfig(config)
		Expect(err).NotTo(HaveOccurred())
		scheme := runtime.NewScheme()
		_ = clientgoscheme.AddToScheme(scheme)
		_ = fdbtypes.AddToScheme(scheme)
		runtimeClient, err = client.New(config, client.Options{Scheme: scheme})
		Expect(err).NotTo(HaveOccurred())
	})

	BeforeEach(func() {
		namespace = randStringRunes(32)
		Expect(createNamespace(kubeClient, namespace)).NotTo(HaveOccurred())
	})

	Context("Create a single node FDB cluster", func() {
		var testCluster *fdbtypes.FoundationDBCluster

		BeforeEach(func() {
			// This will bootstrap a minimal cluster with 1 Pod
			desiredCPU, err := resource.ParseQuantity("100m")
			Expect(err).NotTo(HaveOccurred())
			desiredMemory, err := resource.ParseQuantity("128Mi")
			Expect(err).NotTo(HaveOccurred())
			desiredStorage, err := resource.ParseQuantity("16Gi")
			Expect(err).NotTo(HaveOccurred())

			testCluster = &fdbtypes.FoundationDBCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      randStringRunes(32),
					Namespace: namespace,
				},
				Spec: fdbtypes.FoundationDBClusterSpec{
					Version: "6.2.30",
					FaultDomain: fdbtypes.FoundationDBClusterFaultDomain{
						Key: "foundationdb.org/none",
					},
					DatabaseConfiguration: fdbtypes.DatabaseConfiguration{
						RedundancyMode: "single",
					},
					ProcessCounts: fdbtypes.ProcessCounts{
						Storage:           1,
						Log:               -1,
						ClusterController: -1,
						Stateless:         -1,
					},
					Processes: map[fdbtypes.ProcessClass]fdbtypes.ProcessSettings{
						fdbtypes.ProcessClassGeneral: {
							PodTemplate: &corev1.PodTemplateSpec{
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{
										{
											Name: "foundationdb",
											Resources: corev1.ResourceRequirements{
												Requests: corev1.ResourceList{
													corev1.ResourceCPU:    desiredCPU,
													corev1.ResourceMemory: desiredMemory,
												},
											},
										},
										{
											Name: "foundationdb-kubernetes-sidecar",
											Resources: corev1.ResourceRequirements{
												Requests: corev1.ResourceList{
													corev1.ResourceCPU:    desiredCPU,
													corev1.ResourceMemory: desiredMemory,
												},
											},
										},
									},
									InitContainers: []corev1.Container{
										{
											Name: "foundationdb-kubernetes-init",
											Resources: corev1.ResourceRequirements{
												Requests: corev1.ResourceList{
													corev1.ResourceCPU:    desiredCPU,
													corev1.ResourceMemory: desiredMemory,
												},
											},
										},
									},
								},
							},
							VolumeClaimTemplate: &corev1.PersistentVolumeClaim{
								Spec: corev1.PersistentVolumeClaimSpec{
									Resources: corev1.ResourceRequirements{
										Requests: corev1.ResourceList{
											corev1.ResourceStorage: desiredStorage,
										},
									},
								},
							},
						},
					},
				},
			}

			err = runtimeClient.Create(context.Background(), testCluster)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should reconcile successfully", func() {
			clusterReconciled(runtimeClient, testCluster, kubeClient)
		})

		When("changing the storageServerPerPod", func() {
			BeforeEach(func() {
				clusterReconciled(runtimeClient, testCluster, kubeClient)
				// Change the number of storage servers per pod
				patch := client.MergeFrom(testCluster.DeepCopy())
				testCluster.Spec.StorageServersPerPod = 2
				err := runtimeClient.Patch(context.TODO(), testCluster, patch)
				Expect(err).NotTo(HaveOccurred())
			})

			PIt("should reconcile successfully", func() {
				clusterReconciled(runtimeClient, testCluster, kubeClient)
			})
		})
	})

	AfterEach(func() {
		failed = failed || CurrentGinkgoTestDescription().Failed
		err := kubeClient.CoreV1().Namespaces().Delete(context.Background(), namespace, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())
	})
})

func clusterReconciled(runtimeClient client.Client, testCluster *fdbtypes.FoundationDBCluster, kubeClient *kubernetes.Clientset) {
	counter := 0
	Eventually(func() bool {
		resCluster := &fdbtypes.FoundationDBCluster{}
		_ = runtimeClient.Get(context.Background(), client.ObjectKey{
			Name:      testCluster.Name,
			Namespace: testCluster.Namespace,
		}, resCluster)

		if resCluster.Status.Generations.Reconciled == resCluster.ObjectMeta.Generation && resCluster.Status.Health.Available {
			return true
		}

		// roughly every 10 seconds force a reconcile if something takes longer
		if counter >= 10 {
			patch := client.MergeFrom(resCluster.DeepCopy())
			if resCluster.Annotations == nil {
				resCluster.Annotations = make(map[string]string)
			}
			resCluster.Annotations["foundationdb.org/reconcile"] = strconv.FormatInt(time.Now().UnixNano(), 10)
			// This will apply an Annotation to the object which will trigger the reconcile loop.
			// This should speed up the reconcile phase.
			_ = runtimeClient.Patch(
				context.Background(),
				resCluster,
				patch)

			pods, err := kubeClient.CoreV1().Pods(resCluster.Namespace).List(context.Background(), metav1.ListOptions{
				LabelSelector: labels.SelectorFromSet(map[string]string{
					fdbtypes.FDBClusterLabel: resCluster.Name,
				}).String()},
			)
			Expect(err).NotTo(HaveOccurred())

			for _, pod := range pods.Items {
				if pod.Status.Phase == corev1.PodRunning {
					continue
				}

				log.Printf("Pod name: %s status: %s, %s\n", pod.Name, pod.Status.Phase, pod.Status.Reason)
			}

			counter = 0
		}
		counter++

		return false
	}, 300*time.Second, 1*time.Second).Should(BeTrue())
}
