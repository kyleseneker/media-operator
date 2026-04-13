package reconciler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
)

func newFakeClient(objs ...client.Object) client.Reader {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func TestResolveSecretKeyRef_Success(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-secret", Namespace: "default"},
		Data:       map[string][]byte{"API_KEY": []byte("secret-value-123")},
	}
	fc := newFakeClient(secret)

	val, err := ResolveSecretKeyRef(context.Background(), fc, "default", commonv1alpha1.SecretKeyRef{
		Name: "my-secret", Key: "API_KEY",
	})
	require.NoError(t, err)
	assert.Equal(t, "secret-value-123", val)
}

func TestResolveSecretKeyRef_SecretNotFound(t *testing.T) {
	fc := newFakeClient()

	_, err := ResolveSecretKeyRef(context.Background(), fc, "default", commonv1alpha1.SecretKeyRef{
		Name: "nonexistent", Key: "key",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestResolveSecretKeyRef_KeyNotFound(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "my-secret", Namespace: "default"},
		Data:       map[string][]byte{"OTHER_KEY": []byte("value")},
	}
	fc := newFakeClient(secret)

	_, err := ResolveSecretKeyRef(context.Background(), fc, "default", commonv1alpha1.SecretKeyRef{
		Name: "my-secret", Key: "MISSING_KEY",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "key")
	assert.Contains(t, err.Error(), "not found")
}
