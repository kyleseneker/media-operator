package curation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/intstr"

	curationv1alpha1 "github.com/kyleseneker/media-operator/api/curation/v1alpha1"
	maintainerrclient "github.com/kyleseneker/media-operator/internal/client/maintainerr"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

func testRuleEnv(mediaServer string) ruleEnv {
	return ruleEnv{
		mediaServer: mediaServer,
		apps:        ruleApps(mustJSON(fakeRuleConstants(true)).(map[string]any)),
		libraries: []map[string]any{
			{"id": "1", "title": "Movies", "type": "movie"},
			{"id": "2", "title": "TV Shows", "type": "show"},
		},
		servers: map[maintainerrclient.Arr]map[string]int{
			maintainerrclient.Radarr: {"Radarr": 3},
			maintainerrclient.Sonarr: {"Sonarr": 4},
		},
	}
}

func rule(operator, first, action string) curationv1alpha1.MaintainerrRule {
	return curationv1alpha1.MaintainerrRule{
		Operator: operator, FirstValue: first, Action: action,
		CustomValue: &curationv1alpha1.MaintainerrCustomValue{Type: "number", Value: intstr.FromInt32(0)},
	}
}

// The first rule of a later section carries the operator that joins the
// section; the first rule overall carries none.
func TestBuildRulesPlacesSectionOperators(t *testing.T) {
	send, _, err := buildRules([]curationv1alpha1.MaintainerrRuleSection{
		{Rules: []curationv1alpha1.MaintainerrRule{rule("", "Plex.addDate", "BEFORE"), rule("OR", "Plex.viewCount", "EQUALS")}},
		{Operator: "AND", Rules: []curationv1alpha1.MaintainerrRule{rule("", "Plex.viewCount", "BIGGER")}},
	}, testRuleEnv("plex"))
	require.NoError(t, err)
	require.Len(t, send, 3)
	ops := []any{send[0].(map[string]any)["operator"], send[1].(map[string]any)["operator"], send[2].(map[string]any)["operator"]}
	assert.Equal(t, []any{nil, 1, 0}, ops)
	assert.Equal(t, 1, send[2].(map[string]any)["section"])
}

// Maintainerr's YAML import defaults a missing operator to OR within a section.
// On rules that decide what gets deleted, the operator is required instead.
func TestBuildRulesRequiresExplicitOperators(t *testing.T) {
	env := testRuleEnv("plex")
	for name, sections := range map[string][]curationv1alpha1.MaintainerrRuleSection{
		"missing on a later rule": {{Rules: []curationv1alpha1.MaintainerrRule{
			rule("", "Plex.addDate", "BEFORE"), rule("", "Plex.viewCount", "EQUALS")}}},
		"on the first rule":    {{Rules: []curationv1alpha1.MaintainerrRule{rule("AND", "Plex.addDate", "BEFORE")}}},
		"on the first section": {{Operator: "OR", Rules: []curationv1alpha1.MaintainerrRule{rule("", "Plex.addDate", "BEFORE")}}},
		"missing on a later section": {
			{Rules: []curationv1alpha1.MaintainerrRule{rule("", "Plex.addDate", "BEFORE")}},
			{Rules: []curationv1alpha1.MaintainerrRule{rule("", "Plex.viewCount", "EQUALS")}},
		},
	} {
		_, _, err := buildRules(sections, env)
		assert.Error(t, err, name)
	}
}

func TestCustomValueMatchesMaintainerrsYAMLImport(t *testing.T) {
	for _, tc := range []struct {
		typ   string
		value intstr.IntOrString
		want  map[string]any
	}{
		{"custom_days", intstr.FromInt32(180), map[string]any{"ruleTypeId": 0, "value": "15552000"}},
		{"number", intstr.FromString("2.5"), map[string]any{"ruleTypeId": 0, "value": "2.5"}},
		{"date", intstr.FromString("2026-01-01"), map[string]any{"ruleTypeId": 1, "value": "2026-01-01"}},
		{"text", intstr.FromString("Comedy"), map[string]any{"ruleTypeId": 2, "value": "Comedy"}},
		{"boolean", intstr.FromString("true"), map[string]any{"ruleTypeId": 3, "value": "1"}},
		{"boolean", intstr.FromString("false"), map[string]any{"ruleTypeId": 3, "value": "0"}},
		{"text_list", intstr.FromString(`["a","b"]`), map[string]any{"ruleTypeId": 4, "value": `["a","b"]`}},
	} {
		got, err := customValue(&curationv1alpha1.MaintainerrCustomValue{Type: tc.typ, Value: tc.value})
		require.NoError(t, err, tc.typ)
		assert.Equal(t, tc.want, got, tc.typ)
	}
	_, err := customValue(&curationv1alpha1.MaintainerrCustomValue{Type: "boolean", Value: intstr.FromString("yes")})
	assert.Error(t, err)
	_, err = customValue(&curationv1alpha1.MaintainerrCustomValue{Type: "custom_days", Value: intstr.FromString("soon")})
	assert.Error(t, err)
}

// Maintainerr routes Plex and Jellyfin properties to whichever server is
// configured but keeps the property id, which means something else there.
func TestResolveRejectsTheOtherMediaServersProperties(t *testing.T) {
	env := testRuleEnv("jellyfin")
	got, err := env.resolve("jellyfin.ADDDATE")
	require.NoError(t, err)
	assert.Equal(t, []int{6, 0}, got, "names match case-insensitively, like Maintainerr's import")

	_, err = env.resolve("Plex.addDate")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "media server is jellyfin")
}

// Maintainerr's YAML import takes the first property with a name, which for
// Radarr and Sonarr "monitored" is the deprecated one.
func TestResolvePrefersThePropertyNotMarkedDeprecated(t *testing.T) {
	got, err := testRuleEnv("plex").resolve("Radarr.monitored")
	require.NoError(t, err)
	assert.Equal(t, []int{1, 9}, got)
}

func TestResolveExplainsUnknownNames(t *testing.T) {
	env := testRuleEnv("plex")
	_, err := env.resolve("Tautulli.viewCount")
	assert.ErrorContains(t, err, "only the apps it is connected to")
	_, err = env.resolve("Plex.nope")
	assert.ErrorContains(t, err, `no Plex property "nope"`)
}

func TestRuleDataTypeFollowsTheLibrary(t *testing.T) {
	movies := map[string]any{"type": "movie"}
	shows := map[string]any{"type": "show"}
	got, err := ruleDataType(&curationv1alpha1.MaintainerrRuleGroup{}, movies)
	require.NoError(t, err)
	assert.Equal(t, "movie", got)
	got, err = ruleDataType(&curationv1alpha1.MaintainerrRuleGroup{}, shows)
	require.NoError(t, err)
	assert.Equal(t, "show", got)
	got, err = ruleDataType(&curationv1alpha1.MaintainerrRuleGroup{DataType: "season"}, shows)
	require.NoError(t, err)
	assert.Equal(t, "season", got)
	_, err = ruleDataType(&curationv1alpha1.MaintainerrRuleGroup{DataType: "episode"}, movies)
	assert.Error(t, err)
	_, err = ruleDataType(&curationv1alpha1.MaintainerrRuleGroup{DataType: "movie"}, shows)
	assert.Error(t, err)
}

// Maintainerr stores these as false where they do not apply, which would make
// the operator rewrite the group on every pass.
func TestBuildRuleGroupRejectsSettingsMaintainerrWouldDrop(t *testing.T) {
	base := func() *curationv1alpha1.MaintainerrRuleGroup {
		return &curationv1alpha1.MaintainerrRuleGroup{
			Name: "g", Library: "TV Shows", ArrAction: "unmonitor",
			Sections: []curationv1alpha1.MaintainerrRuleSection{{Rules: []curationv1alpha1.MaintainerrRule{rule("", "Plex.viewCount", "EQUALS")}}},
		}
	}
	env := testRuleEnv("plex")

	g := base()
	g.CleanupLeftoverFolders = new(true)
	_, _, err := buildRuleGroup(g, env, nil)
	assert.ErrorContains(t, err, "leftover folders")

	g = base()
	g.DataType, g.ForceSeerr = "episode", new(true)
	_, _, err = buildRuleGroup(g, env, nil)
	assert.ErrorContains(t, err, "forceSeerr")

	g = base()
	g.ArrAction, g.CleanupLeftoverFolders = "unmonitorDeleteExisting", new(true)
	_, _, err = buildRuleGroup(g, env, nil)
	assert.NoError(t, err)
}

func TestBuildRuleGroupNeedsAValueUnlessTheActionTestsExistence(t *testing.T) {
	g := &curationv1alpha1.MaintainerrRuleGroup{
		Name: "g", Library: "Movies", ArrAction: "delete",
		Sections: []curationv1alpha1.MaintainerrRuleSection{{Rules: []curationv1alpha1.MaintainerrRule{
			{FirstValue: "Plex.viewCount", Action: "EQUALS"},
		}}},
	}
	_, _, err := buildRuleGroup(g, testRuleEnv("plex"), nil)
	assert.ErrorContains(t, err, "needs lastValue or customValue")

	g.Sections[0].Rules[0].Action = "EXISTS"
	_, _, err = buildRuleGroup(g, testRuleEnv("plex"), nil)
	assert.NoError(t, err)
}

// What Maintainerr lists after a save must compare equal to what was saved, or
// every pass rewrites the group. Maintainerr reports the group settings under the
// collection, keeps each rule as sent, and returns a UI-saved operator as text.
func TestStoredRuleGroupCoversWhatWasSaved(t *testing.T) {
	g := &curationv1alpha1.MaintainerrRuleGroup{
		Name: "g", Library: "Movies", ArrAction: "delete", RadarrServer: "Radarr", ActionAfterDays: new(int32(30)),
		Sections: []curationv1alpha1.MaintainerrRuleSection{{Rules: []curationv1alpha1.MaintainerrRule{
			rule("", "Plex.addDate", "BEFORE"), rule("AND", "Plex.viewCount", "EQUALS"),
		}}},
	}
	send, compare, err := buildRuleGroup(g, testRuleEnv("plex"), nil)
	require.NoError(t, err)

	saved := mustJSON(newRuleGroupBody(send)).(map[string]any)
	saved["id"] = 4.0
	listed := mustJSON(storeRuleGroup(saved, nil)).(map[string]any)
	listed["description"] = nil
	rows := listed["rules"].([]any)
	rows[1].(map[string]any)["ruleJson"] = `{"operator":"0","action":2,"firstVal":[0,5],"section":0,"customVal":{"ruleTypeId":0,"value":"0"}}`

	assert.True(t, reconciler.Covers(storedRuleGroup(listed), mustJSON(compare)))

	rows[0].(map[string]any)["ruleJson"] = `{"operator":null,"action":5,"firstVal":[0,0],"section":0,"customVal":{"ruleTypeId":0,"value":"0"},"lastVal":[0,7]}`
	assert.False(t, reconciler.Covers(storedRuleGroup(listed), mustJSON(compare)),
		"a value added to a rule in Maintainerr's UI must count as a difference")
}

func TestMaskPlexTokenMatchesMaintainerr(t *testing.T) {
	assert.Equal(t, "", maskPlexToken(""))
	assert.Equal(t, "****", maskPlexToken("abcdef"))
	assert.Equal(t, "abc...xyz", maskPlexToken("abcdefxyz"))
}
