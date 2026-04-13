package engine

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
)

func generateTestCAPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
}

func TestNewHTTPTransport(t *testing.T) {
	tests := []struct {
		name   string
		tlsCfg *tls.Config
	}{
		{"nil tls config", nil},
		{"with tls config", &tls.Config{InsecureSkipVerify: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := NewHTTPTransport(tt.tlsCfg)
			assert.NotNil(t, transport)
			assert.NotNil(t, transport.DialContext, "should have SSRF-safe dialer")
			if tt.tlsCfg != nil {
				assert.Equal(t, tt.tlsCfg, transport.TLSClientConfig)
			}
		})
	}
}

func TestResolveTLSConfig_Nil(t *testing.T) {
	cfg, err := ResolveTLSConfig(context.Background(), nil, "default", nil)
	assert.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestResolveTLSConfig_InsecureSkipVerify(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	tlsCfg := &commonv1alpha1.TLSConfig{InsecureSkipVerify: true}
	cfg, err := ResolveTLSConfig(context.Background(), fakeClient, "default", tlsCfg)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.True(t, cfg.InsecureSkipVerify)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
}

func TestResolveTLSConfig_CASecret(t *testing.T) {
	caPEM := generateTestCAPEM(t)

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tls-ca", Namespace: "default"},
		Data:       map[string][]byte{"ca.crt": []byte(caPEM)},
	}
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(secret).Build()

	tlsCfg := &commonv1alpha1.TLSConfig{
		CASecretRef: &commonv1alpha1.SecretKeyRef{Name: "tls-ca", Key: "ca.crt"},
	}
	cfg, err := ResolveTLSConfig(context.Background(), fakeClient, "default", tlsCfg)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.NotNil(t, cfg.RootCAs)
}

func TestResolveTLSConfig_CASecretNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	tlsCfg := &commonv1alpha1.TLSConfig{
		CASecretRef: &commonv1alpha1.SecretKeyRef{Name: "nonexistent", Key: "ca.crt"},
	}
	_, err := ResolveTLSConfig(context.Background(), fakeClient, "default", tlsCfg)
	assert.Error(t, err)
}
