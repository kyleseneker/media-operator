package subtitles

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	subtitlesv1alpha1 "github.com/kyleseneker/media-operator/api/subtitles/v1alpha1"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
)

func storedProfile(id int, name string) map[string]any {
	return map[string]any{
		"profileId": float64(id), "name": name, "cutoff": nil, "items": []any{},
		"mustContain": []any{}, "mustNotContain": []any{}, "originalFormat": false, "tag": nil,
	}
}

func sentProfileIDs(t *testing.T, app *fakeBazarr) []float64 {
	t.Helper()
	raw, ok := app.formValue(profilesField)
	require.True(t, ok, "no language profiles were saved")
	var sent []map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &sent))
	ids := make([]float64, 0, len(sent))
	for _, p := range sent {
		ids = append(ids, p["profileId"].(float64))
	}
	return ids
}

func profilesConfig(url string, ids ...int) *subtitlesv1alpha1.BazarrConfig {
	cfg := bazarrConfig(url)
	cfg.Spec.Languages = &subtitlesv1alpha1.BazarrLanguages{}
	for _, id := range ids {
		cfg.Spec.Languages.Profiles = append(cfg.Spec.Languages.Profiles, subtitlesv1alpha1.BazarrLanguageProfile{
			ProfileId: new(id), Name: "declared", Items: []subtitlesv1alpha1.BazarrLanguageItem{{Language: "en"}},
		})
	}
	return cfg
}

func bazarrManaged(t *testing.T, cfg *subtitlesv1alpha1.BazarrConfig) map[string][]string {
	t.Helper()
	c := runBazarr(t, cfg, bazarrSecrets())
	var got subtitlesv1alpha1.BazarrConfig
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "bazarr", Namespace: "media"}, &got))
	return got.Status.ManagedResources
}

func TestBazarrKeepsProfilesItDoesNotDeclare(t *testing.T) {
	app := &fakeBazarr{profiles: []map[string]any{storedProfile(2, "hand-made")}}

	managed := bazarrManaged(t, profilesConfig(app.serve(t), 1))

	assert.ElementsMatch(t, []float64{1, 2}, sentProfileIDs(t, app), "saving the declared profile deleted a hand-made one")
	assert.Equal(t, []string{"1"}, managed[profilesKind])
}

func TestBazarrPrunesOnlyProfilesItCreated(t *testing.T) {
	app := &fakeBazarr{profiles: []map[string]any{
		storedProfile(1, "declared"), storedProfile(2, "old"), storedProfile(3, "hand-made"),
	}}
	cfg := profilesConfig(app.serve(t), 1)
	cfg.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{Prune: new(true)}
	cfg.Status.ManagedResources = map[string][]string{profilesKind: {"1", "2"}}

	managed := bazarrManaged(t, cfg)

	assert.ElementsMatch(t, []float64{1, 3}, sentProfileIDs(t, app))
	assert.Equal(t, []string{"1"}, managed[profilesKind])
}

func TestBazarrObserveReportsAProfilePruneWouldDelete(t *testing.T) {
	for _, tt := range []struct {
		name   string
		owned  []string
		reason string
	}{
		{"owned and undeclared", []string{"2"}, "DriftDetected"},
		{"not owned", nil, "Observed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			app := &fakeBazarr{profiles: []map[string]any{storedProfile(1, "declared"), storedProfile(2, "old")}}
			app.profiles[0]["items"] = []any{map[string]any{
				"id": float64(1), "language": "en", "hi": "False", "forced": "False", "audio_exclude": "False",
			}}
			cfg := profilesConfig(app.serve(t), 1)
			observe := "observe"
			cfg.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{Prune: new(true), DriftPolicy: &observe}
			cfg.Status.ManagedResources = map[string][]string{profilesKind: tt.owned}

			c := runBazarr(t, cfg, bazarrSecrets())

			_, wrote := app.formValue(profilesField)
			assert.False(t, wrote)
			var got subtitlesv1alpha1.BazarrConfig
			require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "bazarr", Namespace: "media"}, &got))
			synced := metaCondition(got.Status.Conditions, "Synced")
			assert.Equal(t, tt.reason, synced.Reason, synced.Message)
		})
	}
}

func TestBazarrDeletionPolicyRemovesTheProfilesItCreated(t *testing.T) {
	app := &fakeBazarr{profiles: []map[string]any{storedProfile(1, "created"), storedProfile(2, "hand-made")}}
	cfg := profilesConfig(app.serve(t), 1)
	policy := ctrlcommon.DeletionPolicyDelete
	cfg.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{DeletionPolicy: &policy}
	cfg.Status.ManagedResources = map[string][]string{profilesKind: {"1"}}
	now := metav1.NewTime(time.Now())
	cfg.DeletionTimestamp = &now
	cfg.Finalizers = []string{ctrlcommon.Finalizer}

	runBazarr(t, cfg, bazarrSecrets())

	assert.Equal(t, []float64{2}, sentProfileIDs(t, app))
}

func metaCondition(conditions []metav1.Condition, condType string) metav1.Condition {
	for _, c := range conditions {
		if c.Type == condType {
			return c
		}
	}
	return metav1.Condition{}
}
