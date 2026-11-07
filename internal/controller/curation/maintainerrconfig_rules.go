package curation

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	curationv1alpha1 "github.com/kyleseneker/media-operator/api/curation/v1alpha1"
	maintainerrclient "github.com/kyleseneker/media-operator/internal/client/maintainerr"
	"github.com/kyleseneker/media-operator/internal/metrics"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

// Maintainerr's ServarrAction, in id order.
var maintainerrArrActions = []string{
	"delete", "unmonitorDeleteAll", "unmonitorDeleteExisting", "unmonitor",
	"doNothing", "deleteShowIfEmpty", "unmonitorShowIfEmpty", "changeQualityProfile",
}

// Maintainerr's RulePossibility, in id order.
var maintainerrComparisons = []string{
	"BIGGER", "SMALLER", "EQUALS", "NOT_EQUALS", "CONTAINS", "BEFORE", "AFTER", "IN_LAST", "IN_NEXT",
	"NOT_CONTAINS", "CONTAINS_PARTIAL", "NOT_CONTAINS_PARTIAL", "CONTAINS_ALL", "NOT_CONTAINS_ALL",
	"COUNT_EQUALS", "COUNT_NOT_EQUALS", "COUNT_BIGGER", "COUNT_SMALLER", "EXISTS", "NOT_EXISTS",
}

var maintainerrOperators = map[string]int{"AND": 0, "OR": 1}

// The collection types Maintainerr stores for a rule group.
const (
	dataMovie   = "movie"
	dataShow    = "show"
	dataSeason  = "season"
	dataEpisode = "episode"
)

// The rule applications that stand for the media server. Maintainerr routes all
// of them to the configured server but keeps the property id, and the ids differ
// between servers, so a property of the wrong server reads something else.
var maintainerrMediaServerApps = []string{"plex", "jellyfin", "emby"}

// The group settings Maintainerr takes at the top level of a save but reports
// under the group's collection.
var maintainerrGroupFieldsOnCollection = []string{
	"arrAction", "listExclusions", "cleanupLeftoverFolders", "forceSeerr", "forceOmbi",
	"tautulliWatchedPercentOverride", "radarrSettingsId", "sonarrSettingsId", "sportarrSettingsId",
	"radarrQualityProfileId", "sonarrQualityProfileId", "sportarrQualityProfileId", "tagInArr",
	"keepInMaintainerrOnly",
}

// The collection settings a save takes under "collection".
var maintainerrCollectionFields = []string{
	"visibleOnRecommended", "visibleOnHome", "deleteAfterDays", "manualCollection",
	"manualCollectionName", "keepLogsForMonths", "sortTitle", "mediaServerSort", "overlayEnabled",
	"overlayTemplateId",
}

// ruleEnv is what rule groups are resolved against.
type ruleEnv struct {
	mediaServer string
	apps        map[string]ruleApp
	libraries   []map[string]any
	servers     map[maintainerrclient.Arr]map[string]int
}

type ruleApp struct {
	id    int
	props map[string][]ruleProp
}

type ruleProp struct {
	id         int
	deprecated bool
}

func (s *maintainerrSync) ruleGroups(
	ctx context.Context, groups []curationv1alpha1.MaintainerrRuleGroup, servers map[maintainerrclient.Arr]map[string]int,
) []string {
	fail := func(err error) []string { return []string{fmt.Sprintf("ruleGroups: %v", err)} }
	// Read after the media server and the other apps are configured: rule
	// properties exist only for configured apps.
	settings, err := s.mc.GetSettings(ctx)
	if err != nil {
		return fail(fmt.Errorf("reading settings: %w", err))
	}
	constants, err := s.mc.RuleConstants(ctx)
	if err != nil {
		return fail(fmt.Errorf("reading rule properties: %w", err))
	}
	libraries, err := s.mc.ListLibraries(ctx)
	if err != nil {
		return fail(fmt.Errorf("listing media server libraries: %w", err))
	}
	stored, err := s.mc.ListRuleGroups(ctx)
	if err != nil {
		return fail(fmt.Errorf("listing rule groups: %w", err))
	}
	mediaServer, _ := settings["media_server_type"].(string)
	env := ruleEnv{
		mediaServer: mediaServer, apps: ruleApps(constants), libraries: libraries, servers: servers,
	}

	var errs []string
	for i := range groups {
		if err := s.ruleGroup(ctx, &groups[i], env, stored); err != nil {
			errs = append(errs, fmt.Sprintf("ruleGroup(%s): %v", groups[i].Name, err))
		}
	}
	return errs
}

func (s *maintainerrSync) ruleGroup(
	ctx context.Context, g *curationv1alpha1.MaintainerrRuleGroup, env ruleEnv, stored []map[string]any,
) error {
	profile, err := s.qualityProfile(ctx, g, env)
	if err != nil {
		return err
	}
	send, compare, err := buildRuleGroup(g, env, profile)
	if err != nil {
		return err
	}

	var match map[string]any
	for _, e := range stored {
		if e["name"] != g.Name {
			continue
		}
		if match != nil {
			return fmt.Errorf("maintainerr has more than one rule group named %q", g.Name)
		}
		match = e
	}
	if match == nil {
		return s.mc.CreateRuleGroup(ctx, newRuleGroupBody(send))
	}

	current := storedRuleGroup(match)
	if reconciler.Covers(current, mustJSON(compare)) {
		return nil
	}
	// An update deletes and recreates every rule and writes a collection log
	// entry, so it is sent only when something differs.
	metrics.DriftCorrectedTotal.WithLabelValues(maintainerrApp, "ruleGroup", g.Name).Inc()
	return s.mc.UpdateRuleGroup(ctx, overlayRuleGroup(current, send))
}

// qualityProfile resolves the profile changeQualityProfile switches to, in the
// server the group acts through.
func (s *maintainerrSync) qualityProfile(
	ctx context.Context, g *curationv1alpha1.MaintainerrRuleGroup, env ruleEnv,
) (*int, error) {
	if g.ArrAction != "changeQualityProfile" {
		if g.QualityProfile != "" {
			return nil, fmt.Errorf("qualityProfile is used only by arrAction changeQualityProfile")
		}
		return nil, nil
	}
	if g.QualityProfile == "" {
		return nil, fmt.Errorf("arrAction changeQualityProfile needs qualityProfile")
	}
	kind, name := maintainerrclient.Sonarr, g.SonarrServer
	if g.RadarrServer != "" {
		kind, name = maintainerrclient.Radarr, g.RadarrServer
	}
	if name == "" {
		return nil, fmt.Errorf("arrAction changeQualityProfile needs radarrServer or sonarrServer")
	}
	id, ok := env.servers[kind][name]
	if !ok {
		return nil, fmt.Errorf("maintainerr has no %s server named %q", kind, name)
	}
	profiles, err := s.mc.QualityProfiles(ctx, kind, id)
	if err != nil {
		return nil, fmt.Errorf("listing %s quality profiles: %w", kind, err)
	}
	for _, p := range profiles {
		if p["name"] == g.QualityProfile {
			if pid, ok := p["id"].(float64); ok {
				n := int(pid)
				return &n, nil
			}
		}
	}
	return nil, fmt.Errorf("%s server %q has no quality profile named %q", kind, name, g.QualityProfile)
}

// buildRuleGroup renders a rule group as Maintainerr saves it (send) and the
// subset of what Maintainerr stores that it has to match (compare). They differ
// only in the rules: compare names every optional rule key, so a value added in
// Maintainerr's UI counts as a difference.
func buildRuleGroup(
	g *curationv1alpha1.MaintainerrRuleGroup, env ruleEnv, qualityProfile *int,
) (send, compare map[string]any, err error) {
	library, err := findLibrary(env.libraries, g.Library)
	if err != nil {
		return nil, nil, err
	}
	dataType, err := ruleDataType(g, library)
	if err != nil {
		return nil, nil, err
	}
	action := slices.Index(maintainerrArrActions, g.ArrAction)
	if action < 0 {
		return nil, nil, fmt.Errorf("unknown arrAction %q", g.ArrAction)
	}
	if isTrue(g.CleanupLeftoverFolders) && !leftoverCleanupAllowed(dataType, g.ArrAction) {
		return nil, nil, fmt.Errorf("maintainerr does not clean up leftover folders for %s on %s; "+
			"it would store cleanupLeftoverFolders as false", g.ArrAction, dataType)
	}
	if isTrue(g.ForceSeerr) && dataType == dataEpisode {
		return nil, nil, fmt.Errorf("maintainerr does not support forceSeerr for episodes; it would store it as false")
	}
	radarr, err := serverID(env, maintainerrclient.Radarr, g.RadarrServer)
	if err != nil {
		return nil, nil, err
	}
	sonarr, err := serverID(env, maintainerrclient.Sonarr, g.SonarrServer)
	if err != nil {
		return nil, nil, err
	}
	sendRules, compareRules, err := buildRules(g.Sections, env)
	if err != nil {
		return nil, nil, err
	}

	var schedule any
	if g.Schedule != "" {
		schedule = g.Schedule
	}
	collection := map[string]any{
		"visibleOnHome": isTrue(g.VisibleOnHome), "visibleOnRecommended": isTrue(g.VisibleOnRecommended),
	}
	// Maintainerr keeps the stored value when a save leaves deleteAfterDays out
	// or null, so it can only be managed while it is set.
	if g.ActionAfterDays != nil {
		collection["deleteAfterDays"] = *g.ActionAfterDays
	}
	var radarrProfile, sonarrProfile any
	if qualityProfile != nil {
		if g.RadarrServer != "" {
			radarrProfile = *qualityProfile
		} else {
			sonarrProfile = *qualityProfile
		}
	}
	send = map[string]any{
		"name": g.Name, "description": g.Description, "libraryId": library["id"], "dataType": dataType,
		"isActive": g.Active == nil || *g.Active, "useRules": true, "ruleHandlerCronSchedule": schedule,
		"arrAction": action, "listExclusions": isTrue(g.ListExclusions),
		"cleanupLeftoverFolders": isTrue(g.CleanupLeftoverFolders), "forceSeerr": isTrue(g.ForceSeerr),
		"tagInArr": isTrue(g.TagInArr), "keepInMaintainerrOnly": isTrue(g.KeepInMaintainerrOnly),
		"radarrSettingsId": radarr, "sonarrSettingsId": sonarr,
		"radarrQualityProfileId": radarrProfile, "sonarrQualityProfileId": sonarrProfile,
		"collection": collection, "rules": sendRules,
	}
	compare = maps.Clone(send)
	compare["rules"] = compareRules
	return send, compare, nil
}

// buildRules flattens sections into Maintainerr's rule list, where the first
// rule of each later section carries the operator that joins the section.
func buildRules(sections []curationv1alpha1.MaintainerrRuleSection, env ruleEnv) (send, compare []any, err error) {
	for si, section := range sections {
		if (si == 0) != (section.Operator == "") {
			return nil, nil, fmt.Errorf("sections[%d]: %s", si, operatorPlacement("section", si))
		}
		for ri, rule := range section.Rules {
			at := fmt.Sprintf("sections[%d].rules[%d]", si, ri)
			if (ri == 0) != (rule.Operator == "") {
				return nil, nil, fmt.Errorf("%s: %s", at, operatorPlacement("rule in a section", ri))
			}
			var operator any
			switch {
			case ri > 0:
				operator = maintainerrOperators[rule.Operator]
			case si > 0:
				operator = maintainerrOperators[section.Operator]
			}
			s, c, err := buildRule(rule, si, operator, env)
			if err != nil {
				return nil, nil, fmt.Errorf("%s: %w", at, err)
			}
			send, compare = append(send, s), append(compare, c)
		}
	}
	return send, compare, nil
}

func operatorPlacement(what string, index int) string {
	if index == 0 {
		return "the first " + what + " takes no operator"
	}
	return "operator is required on every " + what + " after the first"
}

func buildRule(rule curationv1alpha1.MaintainerrRule, section int, operator any, env ruleEnv) (send, compare map[string]any, err error) {
	action := slices.Index(maintainerrComparisons, rule.Action)
	if action < 0 {
		return nil, nil, fmt.Errorf("unknown action %q", rule.Action)
	}
	first, err := env.resolve(rule.FirstValue)
	if err != nil {
		return nil, nil, err
	}
	send = map[string]any{"operator": operator, "action": action, "firstVal": first, "section": section}
	compare = map[string]any{
		"operator": operator, "action": action, "firstVal": first, "section": section,
		"lastVal": nil, "customVal": nil, "arrDiskPath": nil, "username": nil,
	}
	needsValue := rule.Action != "EXISTS" && rule.Action != "NOT_EXISTS"
	switch {
	case rule.LastValue != "" && rule.CustomValue != nil:
		return nil, nil, fmt.Errorf("set lastValue or customValue, not both")
	case rule.LastValue != "":
		last, err := env.resolve(rule.LastValue)
		if err != nil {
			return nil, nil, err
		}
		send["lastVal"], compare["lastVal"] = last, last
	case rule.CustomValue != nil:
		custom, err := customValue(rule.CustomValue)
		if err != nil {
			return nil, nil, err
		}
		send["customVal"], compare["customVal"] = custom, custom
	case needsValue:
		return nil, nil, fmt.Errorf("%s needs lastValue or customValue", rule.Action)
	}
	if rule.ArrDiskPath != "" {
		send["arrDiskPath"], compare["arrDiskPath"] = rule.ArrDiskPath, rule.ArrDiskPath
	}
	if rule.Username != "" {
		send["username"], compare["username"] = rule.Username, rule.Username
	}
	return send, compare, nil
}

// customValue converts a literal as Maintainerr's rule YAML import does:
// custom_days becomes seconds, and booleans become "1" and "0".
func customValue(cv *curationv1alpha1.MaintainerrCustomValue) (map[string]any, error) {
	value := cv.Value.String()
	number := func() (float64, error) {
		n, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return 0, fmt.Errorf("customValue %q is not a number", value)
		}
		return n, nil
	}
	var typeID int
	switch cv.Type {
	case "number":
		if _, err := number(); err != nil {
			return nil, err
		}
	case "custom_days":
		days, err := number()
		if err != nil {
			return nil, err
		}
		value = strconv.FormatFloat(days*86400, 'f', -1, 64)
	case "date":
		typeID = 1
	case "text":
		typeID = 2
	case "boolean":
		typeID = 3
		switch value {
		case "true":
			value = "1"
		case "false":
			value = "0"
		default:
			return nil, fmt.Errorf("boolean customValue must be \"true\" or \"false\", not %q", value)
		}
	case "text_list":
		typeID = 4
	default:
		return nil, fmt.Errorf("unknown customValue type %q", cv.Type)
	}
	return map[string]any{"ruleTypeId": typeID, "value": value}, nil
}

// resolve turns Application.property into Maintainerr's [application, property] ids.
func (env ruleEnv) resolve(identifier string) ([]int, error) {
	appName, propName, ok := strings.Cut(identifier, ".")
	if !ok {
		return nil, fmt.Errorf("%q is not Application.property", identifier)
	}
	appKey := strings.ToLower(appName)
	if slices.Contains(maintainerrMediaServerApps, appKey) && appKey != env.mediaServer {
		server := env.mediaServer
		if server == "" {
			server = "not set"
		}
		return nil, fmt.Errorf("%s reads %s, but Maintainerr's media server is %s", identifier, appName, server)
	}
	app, ok := env.apps[appKey]
	if !ok {
		return nil, fmt.Errorf("maintainerr lists no application %q; it lists only the apps it is connected to", appName)
	}
	candidates := app.props[strings.ToLower(propName)]
	if len(candidates) == 0 {
		return nil, fmt.Errorf("maintainerr has no %s property %q", appName, propName)
	}
	chosen := candidates[0]
	for _, p := range candidates {
		if !p.deprecated {
			chosen = p
			break
		}
	}
	return []int{app.id, chosen.id}, nil
}

// ruleApps indexes /api/rules/constants by lowercase application and property name.
func ruleApps(constants map[string]any) map[string]ruleApp {
	apps := map[string]ruleApp{}
	list, _ := constants["applications"].([]any)
	for _, raw := range list {
		a, _ := raw.(map[string]any)
		name, _ := a["name"].(string)
		id, ok := a["id"].(float64)
		if name == "" || !ok {
			continue
		}
		app := ruleApp{id: int(id), props: map[string][]ruleProp{}}
		props, _ := a["props"].([]any)
		for _, rawProp := range props {
			p, _ := rawProp.(map[string]any)
			propName, _ := p["name"].(string)
			propID, ok := p["id"].(float64)
			if propName == "" || !ok {
				continue
			}
			human, _ := p["humanName"].(string)
			key := strings.ToLower(propName)
			app.props[key] = append(app.props[key], ruleProp{
				id: int(propID), deprecated: strings.Contains(strings.ToLower(human), "(deprecated)"),
			})
		}
		apps[strings.ToLower(name)] = app
	}
	return apps
}

func findLibrary(libraries []map[string]any, title string) (map[string]any, error) {
	var match map[string]any
	var titles []string
	for _, lib := range libraries {
		t, _ := lib["title"].(string)
		titles = append(titles, t)
		if t != title {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("the media server has more than one library titled %q", title)
		}
		match = lib
	}
	if match == nil {
		return nil, fmt.Errorf("the media server has no library titled %q (it has %s)", title, strings.Join(titles, ", "))
	}
	return match, nil
}

// ruleDataType is the collection type Maintainerr stores: always movie for a
// movie library, and the group's dataType (show by default) otherwise.
func ruleDataType(g *curationv1alpha1.MaintainerrRuleGroup, library map[string]any) (string, error) {
	if library["type"] == dataMovie {
		if g.DataType != "" && g.DataType != dataMovie {
			return "", fmt.Errorf("library %q holds movies, so dataType must be movie", g.Library)
		}
		return dataMovie, nil
	}
	switch g.DataType {
	case "":
		return dataShow, nil
	case dataMovie:
		return "", fmt.Errorf("library %q holds shows, so dataType cannot be movie", g.Library)
	}
	return g.DataType, nil
}

// leftoverCleanupAllowed mirrors Maintainerr's leftoverCleanupScope: the action
// and media type combinations that can leave a folder behind.
func leftoverCleanupAllowed(dataType, action string) bool {
	switch action {
	case "unmonitorDeleteAll":
		return dataType == dataMovie || dataType == dataShow
	case "unmonitorDeleteExisting":
		return dataType == dataShow || dataType == dataSeason
	case "delete", "deleteShowIfEmpty":
		return dataType == dataSeason
	}
	return false
}

func serverID(env ruleEnv, kind maintainerrclient.Arr, name string) (any, error) {
	if name == "" {
		return nil, nil
	}
	id, ok := env.servers[kind][name]
	if !ok {
		return nil, fmt.Errorf("maintainerr has no %s server named %q", kind, name)
	}
	return id, nil
}

// storedRuleGroup reshapes a rule group as Maintainerr lists it into the shape a
// save takes, keeping the settings the CR does not manage so an update carries
// them over.
func storedRuleGroup(g map[string]any) map[string]any {
	col, _ := g["collection"].(map[string]any)
	description, _ := g["description"].(string)
	schedule := g["ruleHandlerCronSchedule"]
	if schedule == "" {
		schedule = nil
	}
	body := map[string]any{
		"id": g["id"], "name": g["name"], "description": description, "libraryId": g["libraryId"],
		"dataType": g["dataType"], "isActive": g["isActive"], "useRules": g["useRules"],
		"ruleHandlerCronSchedule": schedule,
	}
	for _, k := range maintainerrGroupFieldsOnCollection {
		body[k] = col[k]
	}
	collection := map[string]any{}
	for _, k := range maintainerrCollectionFields {
		if v, ok := col[k]; ok {
			collection[k] = v
		}
	}
	body["collection"] = collection

	notifications := []any{}
	agents, _ := g["notifications"].([]any)
	for _, raw := range agents {
		if agent, ok := raw.(map[string]any); ok && agent["id"] != nil {
			notifications = append(notifications, map[string]any{"id": agent["id"]})
		}
	}
	body["notifications"] = notifications
	body["rules"] = storedRules(g["rules"])
	return body
}

// storedRules returns the rules as they were saved. Maintainerr keeps each one
// as the JSON it was sent, in rows that are recreated in order on every save.
func storedRules(raw any) []any {
	rows, _ := raw.([]any)
	type row struct {
		id   float64
		rule any
	}
	parsed := make([]row, 0, len(rows))
	for _, r := range rows {
		m, _ := r.(map[string]any)
		text, _ := m["ruleJson"].(string)
		var rule any
		if json.Unmarshal([]byte(text), &rule) != nil {
			rule = text
		}
		id, _ := m["id"].(float64)
		parsed = append(parsed, row{id: id, rule: rule})
	}
	sort.SliceStable(parsed, func(i, j int) bool { return parsed[i].id < parsed[j].id })
	rules := make([]any, 0, len(parsed))
	for _, r := range parsed {
		rules = append(rules, r.rule)
	}
	return rules
}

// overlayRuleGroup is the update for a stored group: the stored settings with
// the managed ones over them.
func overlayRuleGroup(current, send map[string]any) map[string]any {
	body := maps.Clone(current)
	for k, v := range send {
		if k == "collection" {
			continue
		}
		body[k] = v
	}
	collection := map[string]any{}
	if c, ok := current["collection"].(map[string]any); ok {
		maps.Copy(collection, c)
	}
	maps.Copy(collection, send["collection"].(map[string]any))
	body["collection"] = collection
	return body
}

// newRuleGroupBody is the save for a new group, with Maintainerr's defaults for
// what the CR does not manage.
func newRuleGroupBody(send map[string]any) map[string]any {
	body := maps.Clone(send)
	collection := map[string]any{"manualCollection": false, "manualCollectionName": "", "keepLogsForMonths": 6}
	maps.Copy(collection, send["collection"].(map[string]any))
	body["collection"] = collection
	body["notifications"] = []any{}
	return body
}

func isTrue(b *bool) bool { return b != nil && *b }

// mustJSON round-trips a value through JSON so it compares like a decoded
// response: numbers as float64, slices as []any.
func mustJSON(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if json.Unmarshal(raw, &out) != nil {
		return v
	}
	return out
}
