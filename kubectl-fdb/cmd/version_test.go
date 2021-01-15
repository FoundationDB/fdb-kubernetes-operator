package cmd

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestVersion(t *testing.T) {
	operatorName := "/dev/random"
	expectedVersion := "0.26.0"

	client := fake.NewSimpleClientset(&appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      operatorName,
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Image: "foundationdb/fdb-kubernetes-operator:0.26.0",
						},
					},
				},
			},
		},
	})

	operatorVersion := version(client, operatorName, "default")

	if operatorVersion != expectedVersion {
		t.Logf("expected version: %s, but got: %s", expectedVersion, operatorVersion)
		t.Fail()
	}
}
