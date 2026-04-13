package common

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/engine"
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
