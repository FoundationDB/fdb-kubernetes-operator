package helper

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
)

// CreateNamespace ...
func CreateNamespace(ctx context.Context, cfg *envconf.Config, t *testing.T, runID string) (context.Context, error) {
	ns := envconf.RandomName(runID, 10)
	ctx = context.WithValue(ctx, keyNamespaceID, ns)

	t.Logf("Creating NS %v for test %v", ns, t.Name())
	nsObj := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: ns,
		},
	}

	return ctx, cfg.Client().Resources().Create(ctx, &nsObj)
}

// DeleteNamespace ...
func DeleteNamespace(ctx context.Context, cfg *envconf.Config, t *testing.T) (context.Context, error) {
	ns := ctx.Value(keyNamespaceID).(string)
	t.Logf("Deleting NS %v for test %v", ns, t.Name())

	nsObj := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: ns,
		},
	}

	return ctx, cfg.Client().Resources().Delete(ctx, &nsObj)
}
