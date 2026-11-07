package common

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	pvrv1alpha1 "github.com/kyleseneker/media-operator/api/pvr/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/metrics"
)

func TestReconcileInterval(t *testing.T) {
	fiveMin := metav1.Duration{Duration: 5 * time.Minute}
	tenMin := metav1.Duration{Duration: 10 * time.Minute}

	tests := []struct {
		name string
		rc   *commonv1alpha1.ReconcileConfig
		want time.Duration
	}{
		{"nil config", nil, DefaultReconcileInterval},
		{"nil interval", &commonv1alpha1.ReconcileConfig{}, DefaultReconcileInterval},
		{"5m", &commonv1alpha1.ReconcileConfig{Interval: &fiveMin}, 5 * time.Minute},
		{"10m", &commonv1alpha1.ReconcileConfig{Interval: &tenMin}, 10 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ReconcileInterval(tt.rc))
		})
	}
}

func TestPruneEnabled(t *testing.T) {
	tests := []struct {
		name string
		rc   *commonv1alpha1.ReconcileConfig
		want bool
	}{
		{"nil config", nil, false},
		{"nil prune", &commonv1alpha1.ReconcileConfig{}, false},
		{"false", &commonv1alpha1.ReconcileConfig{Prune: new(false)}, false},
		{"true", &commonv1alpha1.ReconcileConfig{Prune: new(true)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, PruneEnabled(tt.rc))
		})
	}
}

func TestResultReason(t *testing.T) {
	tests := []struct {
		name   string
		result engine.ReconcileResult
		want   string
	}{
		{"success", engine.ReconcileResult{Synced: []string{"a"}}, engine.ReasonSynced},
		{"failure", engine.ReconcileResult{Errors: []string{"fail"}}, engine.ReasonSyncFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ResultReason(tt.result))
		})
	}
}

func TestBoolTo01(t *testing.T) {
	tests := []struct {
		name string
		b    bool
		want string
	}{
		{"true", true, "1"},
		{"false", false, "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, BoolTo01(tt.b))
		})
	}
}

func TestDownloadClientReferencesSecret(t *testing.T) {
	dcs := []commonv1alpha1.DownloadClient{
		{
			Name:              "qbit",
			UsernameSecretRef: &commonv1alpha1.SecretKeyRef{Name: "qbit-creds", Key: "username"},
			PasswordSecretRef: &commonv1alpha1.SecretKeyRef{Name: "qbit-creds", Key: "password"},
		},
		{
			Name:            "sabnzbd",
			APIKeySecretRef: &commonv1alpha1.SecretKeyRef{Name: "sab-secret", Key: "apiKey"},
		},
		{
			Name: "no-secrets",
		},
	}

	tests := []struct {
		name       string
		secretName string
		want       bool
	}{
		{"username ref matches", "qbit-creds", true},
		{"apiKey ref matches", "sab-secret", true},
		{"no match", "unknown-secret", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, DownloadClientReferencesSecret(dcs, tt.secretName))
		})
	}
}

func TestEmitPruneEvents(t *testing.T) {
	// EmitPruneEvents just calls recorder.Eventf — verifying it doesn't panic with nil pruned list
	EmitPruneEvents(nil, nil, nil)
	EmitPruneEvents(nil, nil, []engine.PrunedResource{})
}

func TestObservationStatus(t *testing.T) {
	for _, tt := range []struct {
		name   string
		drift  bool
		err    error
		reason string
		synced metav1.ConditionStatus
	}{
		{"matching", false, nil, "Observed", metav1.ConditionTrue},
		{"drift", true, nil, "DriftDetected", metav1.ConditionFalse},
		{"error precedes drift", true, fmt.Errorf("read failed"), engine.ReasonSyncFailed, metav1.ConditionFalse},
	} {
		t.Run(tt.name, func(t *testing.T) {
			obj := newConfig("", 0)
			c := testClient(obj).WithStatusSubresource(obj).Build()
			UpdateObservationStatus(context.Background(), c.Status(), obj, tt.drift, tt.err)
			require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(obj), obj))
			condition := meta.FindStatusCondition(obj.Status.Conditions, "Synced")
			require.NotNil(t, condition)
			assert.Equal(t, tt.reason, condition.Reason)
			assert.Equal(t, tt.synced, condition.Status)
		})
	}
}

func TestUpdateResultStatusReportsObservedDrift(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy string
		result engine.ReconcileResult
		status metav1.ConditionStatus
		reason string
	}{
		{"enforce", "enforce", engine.ReconcileResult{Synced: []string{"naming"}}, metav1.ConditionTrue, engine.ReasonSynced},
		{"observe drift", DriftPolicyObserve, engine.ReconcileResult{Drift: []string{"naming"}}, metav1.ConditionFalse, "DriftDetected"},
		{"observe match", DriftPolicyObserve, engine.ReconcileResult{Synced: []string{"naming"}}, metav1.ConditionTrue, "Observed"},
		{"observe error", DriftPolicyObserve, engine.ReconcileResult{Errors: []string{"x"}}, metav1.ConditionFalse, engine.ReasonSyncFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := newConfig("", 0)
			obj.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{DriftPolicy: &tc.policy}
			c := testClient(obj).WithStatusSubresource(obj).Build()
			UpdateResultStatus(context.Background(), c.Status(), obj, tc.result)
			require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(obj), obj))
			condition := meta.FindStatusCondition(obj.Status.Conditions, "Synced")
			require.NotNil(t, condition)
			assert.Equal(t, tc.status, condition.Status)
			assert.Equal(t, tc.reason, condition.Reason)
		})
	}
}

func TestConfigChangedPredicate(t *testing.T) {
	old := newConfig("", 0)
	old.Generation = 1
	cases := []struct {
		name   string
		change func(*pvrv1alpha1.SonarrConfig)
		want   bool
	}{
		{"status", func(c *pvrv1alpha1.SonarrConfig) { now := metav1.Now(); c.Status.LastSyncTime = &now }, false},
		{"resource version", func(c *pvrv1alpha1.SonarrConfig) { c.ResourceVersion = "2" }, false},
		{"spec", func(c *pvrv1alpha1.SonarrConfig) { c.Generation++ }, true},
		{"deletion", func(c *pvrv1alpha1.SonarrConfig) { now := metav1.Now(); c.DeletionTimestamp = &now }, true},
		{"finalizers", func(c *pvrv1alpha1.SonarrConfig) { c.Finalizers = nil }, true},
		{"annotation", func(c *pvrv1alpha1.SonarrConfig) { c.Annotations = map[string]string{"reconcile": "now"} }, true},
	}
	p := ConfigChangedPredicate()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			next := old.DeepCopy()
			tt.change(next)
			assert.Equal(t, tt.want, p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: next}))
		})
	}
	assert.False(t, p.Update(event.UpdateEvent{ObjectOld: old}))
	assert.False(t, p.Update(event.UpdateEvent{ObjectNew: old}))
	assert.True(t, p.Create(event.CreateEvent{Object: old}))
	assert.True(t, p.Delete(event.DeleteEvent{Object: old}))
	assert.True(t, p.Generic(event.GenericEvent{Object: old}))
}

func newFakeClient(objs ...client.Object) client.Client {
	s := runtime.NewScheme()
	_ = corev1.AddToScheme(s)
	return fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
}

func TestResolveDownloadClientSecrets(t *testing.T) {
	secret := &corev1.Secret{
		Name: "qbit-creds", Namespace: "default",
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
	pod := &corev1.Pod{Name: "test", Namespace: "default"}
	result := FindConfigsBySecret(context.Background(), fc, pod, &corev1.SecretList{}, func(_ *corev1.SecretList) []reconcile.Request {
		return nil
	})
	assert.Nil(t, result)
}

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
