package curation

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	curationv1alpha1 "github.com/kyleseneker/media-operator/api/curation/v1alpha1"
)

func discordNotification() curationv1alpha1.MaintainerrNotification {
	return curationv1alpha1.MaintainerrNotification{
		Name: "Discord", Agent: "discord", Types: []string{"mediaHandled", "mediaAboutToBeHandled"}, AboutScale: new(int32(5)),
		Options:       map[string]commonv1alpha1.FieldValue{"botUsername": {Raw: []byte(`"Maintainerr"`)}},
		SecretOptions: map[string]commonv1alpha1.SecretKeyRef{"webhookUrl": secretRef("discord-webhook")},
	}
}

func roundTrip(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	var out any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func TestMaintainerrCreatesNotificationsAndLinksThemToRuleGroups(t *testing.T) {
	app := newFakeMaintainerr()
	spec := jellyfinSpec(app.serve(t))
	spec.Notifications = []curationv1alpha1.MaintainerrNotification{discordNotification()}
	spec.RuleGroups[0].Notifications = []string{"Discord"}
	run := newMaintainerrRun(t, spec)

	run.reconcile(t)

	require.Len(t, app.notifications, 1)
	assert.Equal(t, map[string]any{
		"id": roundTrip(t, app.notifications[0]["id"]), "name": "Discord", "agent": "discord", "enabled": true,
		"types": []any{float64(8), float64(16)}, "aboutScale": float64(5),
		"options": map[string]any{"botUsername": "Maintainerr", "webhookUrl": "https://discord.example/hook"},
	}, roundTrip(t, app.notifications[0]))
	require.Len(t, app.groups, 1)
	assert.Equal(t, []any{map[string]any{"id": roundTrip(t, app.notifications[0]["id"])}}, roundTrip(t, app.groups[0]["notifications"]))

	var cfg curationv1alpha1.MaintainerrConfig
	require.NoError(t, run.c.Get(context.Background(), types.NamespacedName{Name: "maintainerr", Namespace: "media"}, &cfg))
	assert.Equal(t, []string{"Discord"}, cfg.Status.ManagedResources[notificationsKind])

	app.resetRequests()
	run.reconcile(t)
	assert.Empty(t, app.writes(), "second pass must not write")
}

func TestMaintainerrUpdatesANotificationKeepingWhatTheSpecLeavesOut(t *testing.T) {
	app := newFakeMaintainerr()
	app.notifications = []map[string]any{{
		"id": 40, "name": "Discord", "agent": "discord", "enabled": false, "types": []any{32}, "aboutScale": 2,
		"options": map[string]any{"botUsername": "old", "botAvatarUrl": "https://example.com/a.png"},
	}}
	spec := jellyfinSpec(app.serve(t))
	spec.Notifications = []curationv1alpha1.MaintainerrNotification{{
		Name: "Discord", Agent: "discord",
		Options: map[string]commonv1alpha1.FieldValue{"botUsername": {Raw: []byte(`"Maintainerr"`)}},
	}}

	newMaintainerrRun(t, spec).reconcile(t)

	require.Len(t, app.notifications, 1)
	assert.Equal(t, map[string]any{
		"id": float64(40), "name": "Discord", "agent": "discord", "enabled": false, "types": []any{float64(32)},
		"aboutScale": float64(2), "options": map[string]any{"botUsername": "Maintainerr", "botAvatarUrl": "https://example.com/a.png"},
	}, roundTrip(t, app.notifications[0]), "a save replaces every field, so undeclared ones must be sent back")
}

func TestMaintainerrPrunesOnlyTheNotificationsItCreated(t *testing.T) {
	app := newFakeMaintainerr()
	app.notifications = []map[string]any{
		{"id": 40, "name": "Old", "agent": "slack", "enabled": true, "types": []any{}, "aboutScale": 3, "options": map[string]any{}},
		{"id": 41, "name": "Hand-made", "agent": "slack", "enabled": true, "types": []any{}, "aboutScale": 3, "options": map[string]any{}},
	}
	spec := jellyfinSpec(app.serve(t))
	spec.Reconcile = &commonv1alpha1.ReconcileConfig{Prune: new(true)}
	spec.Notifications = []curationv1alpha1.MaintainerrNotification{discordNotification()}
	run := newMaintainerrRun(t, spec)
	var cfg curationv1alpha1.MaintainerrConfig
	require.NoError(t, run.c.Get(context.Background(), types.NamespacedName{Name: "maintainerr", Namespace: "media"}, &cfg))
	cfg.Status.ManagedResources = map[string][]string{notificationsKind: {"Old"}}
	require.NoError(t, run.c.Status().Update(context.Background(), &cfg))

	run.reconcile(t)

	names := make([]string, 0, len(app.notifications))
	for _, n := range app.notifications {
		names = append(names, n["name"].(string))
	}
	assert.ElementsMatch(t, []string{"Hand-made", "Discord"}, names)
}

func TestMaintainerrRuleGroupNamingAMissingNotificationFails(t *testing.T) {
	app := newFakeMaintainerr()
	spec := jellyfinSpec(app.serve(t))
	spec.RuleGroups[0].Notifications = []string{"Nowhere"}
	run := newMaintainerrRun(t, spec)

	run.reconcile(t)

	assert.Empty(t, app.groups, "a group whose notifications can't be resolved is not saved")
	var cfg curationv1alpha1.MaintainerrConfig
	require.NoError(t, run.c.Get(context.Background(), types.NamespacedName{Name: "maintainerr", Namespace: "media"}, &cfg))
	var message string
	for _, c := range cfg.Status.Conditions {
		if c.Type == "Synced" {
			message = c.Message
		}
	}
	assert.Contains(t, message, `maintainerr has no notification named "Nowhere"`)
}
