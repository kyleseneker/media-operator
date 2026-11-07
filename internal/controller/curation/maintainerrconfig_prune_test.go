package curation

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/metrics"
)

func groupNames(groups []map[string]any) []any {
	names := make([]any, 0, len(groups))
	for _, g := range groups {
		names = append(names, g["name"])
	}
	return names
}

func TestMaintainerrPrunesOnlyRuleGroupsItCreated(t *testing.T) {
	app := newFakeMaintainerr()
	spec := jellyfinSpec(app.serve(t))
	stale := spec.RuleGroups[0]
	old := stale
	old.Name = "Old movies"
	spec.RuleGroups = append(spec.RuleGroups, old)
	run := newMaintainerrRun(t, spec)

	run.reconcile(t)
	managed := run.get(t).Status.ManagedResources
	assert.Equal(t, []string{"Stale movies", "Old movies"}, managed[ruleGroupsKind])
	assert.Equal(t, []string{"Radarr"}, managed["radarr"])

	app.groups = append(app.groups, map[string]any{"id": 50, "name": "hand-made", "collection": map[string]any{}})
	cfg := run.get(t)
	cfg.Spec.RuleGroups = cfg.Spec.RuleGroups[:1]
	cfg.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{Prune: new(true)}
	require.NoError(t, run.c.Update(context.Background(), cfg))
	run.reconcile(t)

	synced := run.condition(t, "Synced")
	require.Equal(t, conditionTrue, string(synced.Status), synced.Message)
	assert.ElementsMatch(t, []any{"Stale movies", "hand-made"}, groupNames(app.groups))
	assert.Equal(t, []string{"Stale movies"}, run.get(t).Status.ManagedResources[ruleGroupsKind])
}

func TestMaintainerrDeletionPolicyRemovesRuleGroupsBeforeServers(t *testing.T) {
	app := newFakeMaintainerr()
	spec := jellyfinSpec(app.serve(t))
	policy := ctrlcommon.DeletionPolicyDelete
	spec.Reconcile = &commonv1alpha1.ReconcileConfig{DeletionPolicy: &policy}
	run := newMaintainerrRun(t, spec)
	run.reconcile(t)
	app.servers["radarr"] = append(app.servers["radarr"], map[string]any{"id": 60, "serverName": "hand-made"})

	require.NoError(t, run.c.Delete(context.Background(), run.get(t)))
	run.reconcile(t)

	assert.Empty(t, app.groups)
	require.Len(t, app.servers["radarr"], 1)
	assert.Equal(t, "hand-made", app.servers["radarr"][0]["serverName"])
}

func TestMaintainerrObserveReportsWhatPruneWouldDelete(t *testing.T) {
	app := newFakeMaintainerr()
	spec := jellyfinSpec(app.serve(t))
	run := newMaintainerrRun(t, spec)
	run.reconcile(t)
	app.groups = append(app.groups, map[string]any{"id": 51, "name": "Observed old", "collection": map[string]any{}})

	cfg := run.get(t)
	cfg.Status.ManagedResources[ruleGroupsKind] = append(cfg.Status.ManagedResources[ruleGroupsKind], "Observed old")
	require.NoError(t, run.c.Status().Update(context.Background(), cfg))
	observe := "observe"
	cfg.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{Prune: new(true), DriftPolicy: &observe}
	require.NoError(t, run.c.Update(context.Background(), cfg))
	observed := metrics.DriftObservedTotal.WithLabelValues(maintainerrApp, ruleGroupsKind, "Observed old")
	before := testutil.ToFloat64(observed)

	run.reconcile(t)

	assert.Contains(t, groupNames(app.groups), "Observed old", "observe mode deleted a rule group")
	assert.Equal(t, before+1, testutil.ToFloat64(observed))
	assert.Equal(t, "DriftDetected", run.condition(t, "Synced").Reason)
}
