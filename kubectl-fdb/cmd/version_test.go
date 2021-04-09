package cmd

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"

	"k8s.io/cli-runtime/pkg/genericclioptions"

	fdbtypes "github.com/FoundationDB/fdb-kubernetes-operator/api/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestVersion(t *testing.T) {
	operatorName := "fdb-operator"

	tt := []struct {
		name          string
		deployment    *appsv1.Deployment
		expected      string
		expectedError error
	}{
		{
			name:     "Single container",
			expected: "0.27.0",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      operatorName,
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "manager",
									Image: "foundationdb/fdb-kubernetes-operator:0.27.0",
								},
							},
						},
					},
				},
			},
			expectedError: nil,
		},
		{
			name:     "Multi container",
			expected: "0.27.0",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      operatorName,
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test",
									Image: "test:1337",
								},
								{
									Name:  "test2",
									Image: "test:1337-2",
								},
								{
									Name:  "manager",
									Image: "foundationdb/fdb-kubernetes-operator:0.27.0",
								},
							},
						},
					},
				},
			},
			expectedError: nil,
		},
		{
			name:     "No container",
			expected: "",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      operatorName,
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{},
							},
						},
					},
				},
			},
			expectedError: fmt.Errorf("could not find container: manager in default/fdb-operator"),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = clientgoscheme.AddToScheme(scheme)
			_ = fdbtypes.AddToScheme(scheme)
			kubeClient := fake.NewFakeClientWithScheme(scheme, tc.deployment)

			operatorVersion, err := version(kubeClient, operatorName, "default", "manager")

			if !reflect.DeepEqual(err, tc.expectedError) {
				t.Errorf("Expected: %s, got: %s", tc.expectedError, err)
			}

			if operatorVersion != tc.expected {
				t.Errorf("expected version: %s, but got: %s", tc.expected, operatorVersion)
			}
		})
	}
}

func TestVersionClientOnly(t *testing.T) {
	// We use these buffers to check the input/output
	outBuffer := bytes.Buffer{}
	errBuffer := bytes.Buffer{}
	inBuffer := bytes.Buffer{}

	rootCmd := NewRootCmd(genericclioptions.IOStreams{In: &inBuffer, Out: &outBuffer, ErrOut: &errBuffer})

	args := []string{"version", "--client-only"}
	rootCmd.SetArgs(args)

	err := rootCmd.Execute()
	if err != nil {
		t.Error(err)
	}

	expectedStr := "kubectl-fdb: latest\n"
	if outBuffer.String() != expectedStr {
		t.Errorf("expected: %s\n got:%s\n", expectedStr, outBuffer.String())
	}
}
