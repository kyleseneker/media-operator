package common

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	pvrv1alpha1 "github.com/kyleseneker/media-operator/api/pvr/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func newSecretClient() *fake.ClientBuilder {
	return fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(&corev1.Secret{
		Name: "tracker", Namespace: "arr",
		Data: map[string][]byte{"passkey": []byte("s3cr3t")},
	})
}

func TestResolveConfigFieldsFromSecret(t *testing.T) {
	c := newSecretClient().Build()
	fields := []commonv1alpha1.ConfigField{
		{Name: "apiKey", ValueFrom: &commonv1alpha1.SecretKeyRef{Name: "tracker", Key: "passkey"}},
	}
	out, err := ResolveConfigFields(context.Background(), c, "arr", "indexer x", fields)
	require.NoError(t, err)
	assert.Nil(t, out[0].ValueFrom, "valueFrom should be cleared after resolution")
	b, _ := json.Marshal(out[0].Value)
	assert.Equal(t, `"s3cr3t"`, string(b), "expected quoted secret value")
	assert.Nil(t, fields[0].Value, "input slice was mutated")
}

func TestResolveConfigFieldsRejectsBothValueAndValueFrom(t *testing.T) {
	c := newSecretClient().Build()
	raw, _ := json.Marshal("literal")
	fields := []commonv1alpha1.ConfigField{{
		Name:      "apiKey",
		Value:     &commonv1alpha1.FieldValue{Raw: raw},
		ValueFrom: &commonv1alpha1.SecretKeyRef{Name: "tracker", Key: "passkey"},
	}}
	_, err := ResolveConfigFields(context.Background(), c, "arr", "indexer x", fields)
	require.Error(t, err, "expected an error when both value and valueFrom are set")
}

func TestResolveConfigFieldsMissingSecret(t *testing.T) {
	c := newSecretClient().Build()
	fields := []commonv1alpha1.ConfigField{
		{Name: "apiKey", ValueFrom: &commonv1alpha1.SecretKeyRef{Name: "nope", Key: "passkey"}},
	}
	_, err := ResolveConfigFields(context.Background(), c, "arr", "indexer x", fields)
	require.Error(t, err, "expected an error for a missing secret")
}

func TestListsReferenceSecret(t *testing.T) {
	idx := []commonv1alpha1.Indexer{{
		Name:   "tl",
		Fields: []commonv1alpha1.ConfigField{{Name: "apiKey", ValueFrom: &commonv1alpha1.SecretKeyRef{Name: "tracker", Key: "passkey"}}},
	}}
	assert.True(t, ListsReferenceSecret(idx, nil, nil, "tracker"), "should match the referenced secret")
	assert.False(t, ListsReferenceSecret(idx, nil, nil, "other"), "should not match an unrelated secret")
}

func TestConfigSyncedGaugeTracksOutcome(t *testing.T) {
	c := &pvrv1alpha1.SonarrConfig{
		Name: "s", Namespace: "arr",
		Kind: "SonarrConfig",
	}
	_ = pvrv1alpha1.AddToScheme(scheme.Scheme)
	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).
		WithObjects(c).WithStatusSubresource(c).Build()

	UpdateStatus(context.Background(), cl.Status(), c, true, "Synced", "ok")
	assert.Equal(t, 1.0, testutil.ToFloat64(metrics.ConfigSynced.WithLabelValues("SonarrConfig", "arr", "s")),
		"expected gauge 1 after a successful sync")

	UpdateStatusUnreachable(context.Background(), cl.Status(), c, "AppUnreachable", "boom")
	assert.Equal(t, 0.0, testutil.ToFloat64(metrics.ConfigSynced.WithLabelValues("SonarrConfig", "arr", "s")),
		"expected gauge 0 after failure")
}
