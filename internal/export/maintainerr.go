package export

import (
	"encoding/json"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"

	maintainerrclient "github.com/kyleseneker/media-operator/internal/client/maintainerr"
)

var maintainerrDayComparisons = []string{"BEFORE", "AFTER", "IN_LAST", "IN_NEXT"}

const secondsPerDay = 86400

type maintainerrExport struct {
	*reader
	mc        *maintainerrclient.Client
	props     map[[2]float64]string
	libraries map[any]string
	servers   map[maintainerrclient.Arr]map[float64]string
}

func exportMaintainerr(r *reader) (map[string]any, error) {
	e := &maintainerrExport{reader: r, mc: maintainerrclient.NewClient(r.hc)}
	settings, err := e.mc.GetSettings(r.ctx)
	if err != nil {
		return nil, err
	}
	spec := map[string]any{}

	switch settings["media_server_type"] {
	case "jellyfin":
		jf, err := e.mc.GetJellyfin(r.ctx)
		if err != nil {
			return nil, err
		}
		key, _ := jf["jellyfin_api_key"].(string)
		jellyfin := map[string]any{"url": jf["jellyfin_url"], "apiKeySecretRef": r.creds.ref(unmasked(key), "jellyfin", "api-key")}
		setText(jellyfin, "userId", jf["jellyfin_user_id"])
		spec["mediaServer"] = map[string]any{"jellyfin": jellyfin}
	case "plex":
		plex := map[string]any{
			"hostname": settings["plex_hostname"], "port": settings["plex_port"], "ssl": num(settings["plex_ssl"]) == 1,
			"tokenSecretRef": r.creds.ref("", "plex", "token"),
		}
		spec["mediaServer"] = map[string]any{"plex": plex}
	}

	e.servers = map[maintainerrclient.Arr]map[float64]string{}
	for _, kind := range []maintainerrclient.Arr{maintainerrclient.Sonarr, maintainerrclient.Radarr} {
		stored, err := e.mc.ListArr(r.ctx, kind)
		if err != nil {
			return nil, err
		}
		e.servers[kind] = map[float64]string{}
		var servers []any
		for _, s := range stored {
			name, _ := s["serverName"].(string)
			e.servers[kind][num(s["id"])] = name
			key, _ := s["apiKey"].(string)
			server := map[string]any{"name": name, "url": s["url"], "apiKeySecretRef": r.creds.ref(unmasked(key), string(kind), name, "api-key")}
			for _, field := range []string{"tagExclusions", "untagOnUnexclude"} {
				if v, ok := s[field]; ok && v != nil {
					server[field] = v
				}
			}
			setText(server, "exclusionTag", s["exclusionTag"])
			servers = append(servers, server)
		}
		setList(spec, string(kind), servers)
	}

	for _, svc := range []maintainerrclient.Service{maintainerrclient.Seerr, maintainerrclient.Tautulli} {
		stored, err := e.mc.GetService(r.ctx, svc)
		if err != nil {
			return nil, err
		}
		if url, _ := stored["url"].(string); url != "" {
			key, _ := stored["api_key"].(string)
			spec[string(svc)] = map[string]any{"url": url, "apiKeySecretRef": r.creds.ref(unmasked(key), string(svc), "api-key")}
		}
	}

	jobs := map[string]any{}
	setText(jobs, "rules", settings["rules_handler_job_cron"])
	setText(jobs, "collections", settings["collection_handler_job_cron"])
	if len(jobs) > 0 {
		spec["jobs"] = jobs
	}

	groups, err := e.mc.ListRuleGroups(r.ctx)
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return spec, nil
	}
	if err := e.indexRuleReferences(); err != nil {
		return nil, err
	}
	out := make([]any, 0, len(groups))
	for _, g := range groups {
		out = append(out, e.ruleGroup(g))
	}
	spec["ruleGroups"] = out
	return spec, nil
}

func (e *maintainerrExport) indexRuleReferences() error {
	constants, err := e.mc.RuleConstants(e.ctx)
	if err != nil {
		return err
	}
	e.props = map[[2]float64]string{}
	for _, a := range list(constants["applications"]) {
		app, _ := a["name"].(string)
		for _, p := range list(a["props"]) {
			prop, _ := p["name"].(string)
			e.props[[2]float64{num(a["id"]), num(p["id"])}] = app + "." + prop
		}
	}
	libraries, err := e.mc.ListLibraries(e.ctx)
	if err != nil {
		return err
	}
	e.libraries = map[any]string{}
	for _, lib := range libraries {
		title, _ := lib["title"].(string)
		e.libraries[lib["id"]] = title
	}
	return nil
}

func (e *maintainerrExport) ruleGroup(g map[string]any) map[string]any {
	name, _ := g["name"].(string)
	col, _ := g["collection"].(map[string]any)
	group := map[string]any{
		"name": name, "library": e.libraries[g["libraryId"]], "dataType": g["dataType"], "active": g["isActive"],
	}
	setText(group, "description", g["description"])
	setText(group, "schedule", g["ruleHandlerCronSchedule"])
	if action := int(num(col["arrAction"])); action >= 0 && action < len(maintainerrclient.ArrActions) {
		group["arrAction"] = maintainerrclient.ArrActions[action]
	}
	if days, ok := col["deleteAfterDays"].(float64); ok {
		group["actionAfterDays"] = days
	}
	for _, field := range []string{
		"listExclusions", "cleanupLeftoverFolders", "forceSeerr", "tagInArr", "keepInMaintainerrOnly", "visibleOnHome", "visibleOnRecommended",
	} {
		if v, ok := col[field].(bool); ok {
			group[field] = v
		}
	}
	for _, s := range []struct {
		kind               maintainerrclient.Arr
		field, id, profile string
	}{
		{maintainerrclient.Radarr, "radarrServer", "radarrSettingsId", "radarrQualityProfileId"},
		{maintainerrclient.Sonarr, "sonarrServer", "sonarrSettingsId", "sonarrQualityProfileId"},
	} {
		id, ok := col[s.id].(float64)
		if !ok {
			continue
		}
		group[s.field] = e.servers[s.kind][id]
		if profile, ok := col[s.profile].(float64); ok {
			group["qualityProfile"] = e.qualityProfileName(s.kind, int(id), profile, name)
		}
	}
	group["sections"] = e.ruleSections(g["rules"], name)
	return group
}

func (e *maintainerrExport) qualityProfileName(kind maintainerrclient.Arr, server int, id float64, group string) any {
	profiles, err := e.mc.QualityProfiles(e.ctx, kind, server)
	if err == nil {
		for _, p := range profiles {
			if num(p["id"]) == id {
				return p["name"]
			}
		}
	}
	e.warn("rule group %q: couldn't name its quality profile %v, so qualityProfile is left out", group, id)
	return nil
}

func (e *maintainerrExport) ruleSections(raw any, group string) []any {
	type row struct {
		id   float64
		rule map[string]any
	}
	var rows []row
	for _, r := range list(raw) {
		text, _ := r["ruleJson"].(string)
		var rule map[string]any
		if json.Unmarshal([]byte(text), &rule) == nil {
			rows = append(rows, row{id: num(r["id"]), rule: rule})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].id < rows[j].id })

	type section struct {
		operator any
		rules    []any
	}
	sections := make([]*section, 0, len(rows))
	current := -1
	for _, r := range rows {
		index := int(num(r.rule["section"]))
		rule := e.rule(r.rule, group)
		if index != current {
			s := &section{}
			if current >= 0 {
				s.operator = rule["operator"]
			}
			delete(rule, "operator")
			sections = append(sections, s)
			current = index
		}
		last := sections[len(sections)-1]
		last.rules = append(last.rules, rule)
	}
	out := make([]any, 0, len(sections))
	for _, s := range sections {
		m := map[string]any{"rules": s.rules}
		if s.operator != nil {
			m["operator"] = s.operator
		}
		out = append(out, m)
	}
	return out
}

func (e *maintainerrExport) rule(r map[string]any, group string) map[string]any {
	rule := map[string]any{}
	if op, ok := r["operator"].(float64); ok && int(op) < len(maintainerrclient.Operators) {
		rule["operator"] = maintainerrclient.Operators[int(op)]
	}
	action := ""
	if a := int(num(r["action"])); a >= 0 && a < len(maintainerrclient.Comparisons) {
		action = maintainerrclient.Comparisons[a]
		rule["action"] = action
	}
	rule["firstValue"] = e.propName(r["firstVal"], group)
	if last, ok := r["lastVal"].([]any); ok && len(last) == 2 {
		rule["lastValue"] = e.propName(last, group)
	}
	if custom, ok := r["customVal"].(map[string]any); ok {
		rule["customValue"] = maintainerrCustomValue(custom, action)
	}
	setText(rule, "arrDiskPath", r["arrDiskPath"])
	setText(rule, "username", r["username"])
	return rule
}

func (e *maintainerrExport) propName(v any, group string) any {
	ids, _ := v.([]any)
	if len(ids) == 2 {
		if name, ok := e.props[[2]float64{num(ids[0]), num(ids[1])}]; ok {
			return name
		}
	}
	e.warn("rule group %q refers to property %v, which Maintainerr no longer lists", group, v)
	return v
}

func maintainerrCustomValue(custom map[string]any, action string) map[string]any {
	value := sabText(custom["value"])
	switch int(num(custom["ruleTypeId"])) {
	case 1:
		return map[string]any{"type": "date", "value": value}
	case 2:
		return map[string]any{"type": "text", "value": value}
	case 3:
		return map[string]any{"type": "boolean", "value": strconv.FormatBool(value == "1")}
	case 4:
		return map[string]any{"type": "text_list", "value": value}
	}
	n, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return map[string]any{"type": "number", "value": value}
	}
	if slices.Contains(maintainerrDayComparisons, action) && n != 0 && math.Mod(n, secondsPerDay) == 0 {
		return map[string]any{"type": "custom_days", "value": int(n / secondsPerDay)}
	}
	if n == math.Trunc(n) {
		return map[string]any{"type": "number", "value": int(n)}
	}
	return map[string]any{"type": "number", "value": value}
}

func unmasked(s string) string {
	if strings.Contains(s, "...") || strings.Trim(s, "*") == "" {
		return ""
	}
	return s
}
