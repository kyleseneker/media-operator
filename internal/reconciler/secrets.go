package reconciler

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
)

// ResolveSecretKeyRef reads a value from a Kubernetes Secret.
func ResolveSecretKeyRef(ctx context.Context, c client.Reader, namespace string, ref commonv1alpha1.SecretKeyRef) (string, error) {
	secret := &corev1.Secret{}
	err := c.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      ref.Name,
	}, secret)
	if err != nil {
		return "", fmt.Errorf("secret %q not found: %w", ref.Name, err)
	}

	val, ok := secret.Data[ref.Key]
	if !ok {
		return "", fmt.Errorf("key %q not found in secret %q", ref.Key, ref.Name)
	}

	return string(val), nil
}
