package common

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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
)

func newFakeClient(objs ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
}

func TestResolveDownloadClientSecrets(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "qbit-creds", Namespace: "default"},
		Data: map[string][]byte{
			"username": []byte("admin"),
			"password": []byte("pass123"),
		},
	}
	fc := newFakeClient(secret)

	dcs := []commonv1alpha1.DownloadClient{
		{
			Name:              "qbit",
			UsernameSecretRef: &commonv1alpha1.SecretKeyRef{Name: "qbit-creds", Key: "username"},
			PasswordSecretRef: &commonv1alpha1.SecretKeyRef{Name: "qbit-creds", Key: "password"},
		},
		{
			Name: "no-secrets",
		},
	}

	resolved, err := ResolveDownloadClientSecrets(context.Background(), fc, "default", dcs)
	require.NoError(t, err)
	assert.Equal(t, "admin", resolved["qbit"].Username)
	assert.Equal(t, "pass123", resolved["qbit"].Password)
	assert.Empty(t, resolved["no-secrets"].Username)
}

func TestResolveDownloadClientSecrets_MissingSecret(t *testing.T) {
	fc := newFakeClient()

	dcs := []commonv1alpha1.DownloadClient{
		{
			Name:              "qbit",
			UsernameSecretRef: &commonv1alpha1.SecretKeyRef{Name: "nonexistent", Key: "username"},
		},
	}

	_, err := ResolveDownloadClientSecrets(context.Background(), fc, "default", dcs)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "username")
}

func TestFindConfigsBySecret_NonSecret(t *testing.T) {
	fc := newFakeClient()
	// Passing a non-Secret object should return nil
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}}
	result := FindConfigsBySecret(context.Background(), fc, pod, &corev1.SecretList{}, func(_ *corev1.SecretList) []reconcile.Request {
		return nil
	})
	assert.Nil(t, result)
}
