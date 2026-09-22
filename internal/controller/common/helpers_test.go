package common

import (
	"context"
	"fmt"
	"testing"
	"time"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	pvrv1alpha1 "github.com/kyleseneker/media-operator/api/pvr/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
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

func boolPtr(b bool) *bool { return &b }

func TestPruneEnabled(t *testing.T) {
	tests := []struct {
		name string
		rc   *commonv1alpha1.ReconcileConfig
		want bool
	}{
		{"nil config", nil, false},
		{"nil prune", &commonv1alpha1.ReconcileConfig{}, false},
		{"false", &commonv1alpha1.ReconcileConfig{Prune: boolPtr(false)}, false},
		{"true", &commonv1alpha1.ReconcileConfig{Prune: boolPtr(true)}, true},
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

func TestRejectUnsupportedObserve(t *testing.T) {
	for _, policy := range []string{"enforce", DriftPolicyObserve} {
		t.Run(policy, func(t *testing.T) {
			obj := newConfig("", 0)
			obj.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{DriftPolicy: &policy}
			c := testClient(obj).WithStatusSubresource(obj).Build()
			rejected := RejectUnsupportedObserve(context.Background(), c.Status(), obj)
			assert.Equal(t, policy == DriftPolicyObserve, rejected)
			require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(obj), obj))
			condition := meta.FindStatusCondition(obj.Status.Conditions, "Synced")
			if rejected {
				require.NotNil(t, condition)
				assert.Equal(t, engine.ReasonInvalidConfig, condition.Reason)
			} else {
				assert.Nil(t, condition)
			}
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
