package export

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	transcodev1alpha1 "github.com/kyleseneker/media-operator/api/transcode/v1alpha1"
	tdarrclient "github.com/kyleseneker/media-operator/internal/client/tdarr"
)

var tdarrWorkerTypes = map[string]string{
	"transcodeGPU": "transcodegpu", "transcodeCPU": "transcodecpu",
	"healthcheckGPU": "healthcheckgpu", "healthcheckCPU": "healthcheckcpu",
}

func exportTdarr(r *reader) (map[string]any, error) {
	tc := tdarrclient.NewClient(r.hc)
	libraries, err := tc.GetAll(r.ctx, "LibrarySettingsJSONDB")
	if err != nil {
		return nil, err
	}
	variables, err := tc.GetAll(r.ctx, "VariablesJSONDB")
	if err != nil {
		return nil, err
	}
	flows, err := tc.GetAll(r.ctx, "FlowsJSONDB")
	if err != nil {
		return nil, err
	}
	nodes, err := tc.GetNodes(r.ctx)
	if err != nil {
		return nil, err
	}
	globals, err := tc.GetByID(r.ctx, "SettingsGlobalJSONDB", "globalsettings")
	if err != nil {
		return nil, err
	}

	spec := map[string]any{}
	if settings := r.tdarrSettings(globals); len(settings) > 0 {
		spec["settings"] = settings
	}
	var libs []any
	for _, doc := range libraries {
		libs = append(libs, r.tdarrLibrary(doc, variables))
	}
	if len(libs) > 0 {
		spec["libraries"] = libs
	}

	var fs []any
	for _, doc := range flows {
		flow := pick(doc, "name", "flowPlugins", "flowEdges")
		flow["id"] = doc["_id"]
		if d, _ := doc["description"].(string); d != "" {
			flow["description"] = d
		}
		fs = append(fs, flow)
	}
	if len(fs) > 0 {
		spec["flows"] = fs
	}

	if workers := r.tdarrWorkers(nodes); workers != nil {
		spec["workers"] = workers
	}
	return spec, nil
}

// tdarrSettings exports the global options TdarrSettings holds, under the
// same names, with the Discord webhook moved to the Secret.
func (r *reader) tdarrSettings(doc map[string]any) map[string]any {
	out := map[string]any{}
	for field := range reflect.TypeFor[transcodev1alpha1.TdarrSettings]().Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if v, ok := doc[name]; ok && v != nil && v != "" {
			out[name] = v
		}
	}
	if webhook, _ := doc["notificationsDiscordWebhook"].(string); webhook != "" {
		out["notificationsDiscordWebhookSecretRef"] = r.creds.ref(webhook, "discord-webhook")
	}
	return out
}

// tdarrPlugins exports a library's classic plugin stack in priority order.
func (r *reader) tdarrPlugins(library string, entries []map[string]any) []string {
	slices.SortStableFunc(entries, func(a, b map[string]any) int { return int(num(a["priority"]) - num(b["priority"])) })
	var ids []string
	for _, e := range entries {
		inputs, _ := e["InputsDB"].(map[string]any)
		if e["checked"] != true || e["source"] != "Community" || len(inputs) > 0 {
			r.warn("library %q: plugin %v is disabled, local or has options, which pluginStack can't express", library, e["id"])
			continue
		}
		if id, _ := e["id"].(string); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func (r *reader) tdarrLibrary(doc map[string]any, variables []map[string]any) map[string]any {
	lib := pick(doc, "name", "folder", "cache", "container", "containerFilter", "foldersToIgnore", "folderWatching",
		"processLibrary", "processTranscodes", "processHealthChecks", "scanOnStart", "scheduledScanFindNew",
		"scannerThreadCount", "ffmpeg", "priority", "flowId", "foldersToIgnoreCaseInsensitive", "filterResolutionsSkip",
		"filterCodecsSkip", "filterContainersSkip", "filterHardlinked", "useFsEvents", "folderWatchScanInterval")
	id, _ := doc["_id"].(string)
	name, _ := doc["name"].(string)
	lib["id"] = id
	switch {
	case doc["handbrakescan"] == true:
		lib["healthCheckMode"] = "quick"
	case doc["ffmpegscan"] == true:
		lib["healthCheckMode"] = "thorough"
	}
	if decision, ok := doc["decisionMaker"].(map[string]any); ok {
		switch {
		case decision["settingsFlows"] == true:
			lib["decisionMode"] = "flows"
		case decision["settingsPlugin"] == true:
			lib["decisionMode"] = "plugins"
		default:
			r.warn("library %q decides by video and audio settings, which TdarrConfig can't express, so they stay as set in Tdarr", name)
		}
	}
	if plugins := r.tdarrPlugins(name, list(doc["pluginIDs"])); len(plugins) > 0 {
		lib["pluginStack"] = plugins
	}

	vars := map[string]any{}
	if v, ok := doc["variables"].(map[string]any); ok {
		user, _ := v["user"].(map[string]any)
		for key, value := range user {
			vars[key] = fmt.Sprint(value)
		}
	}
	for _, v := range variables {
		if t, _ := v["type"].(string); t == "library:"+id {
			if key, _ := v["key"].(string); key != "" {
				vars[key] = fmt.Sprint(v["value"])
			}
		}
	}
	if len(vars) > 0 {
		lib["variables"] = vars
	}
	return lib
}

func (r *reader) tdarrWorkers(nodes map[string]any) map[string]any {
	if len(nodes) == 0 {
		return nil
	}
	byName := map[string]map[string]any{}
	for _, raw := range nodes {
		node, _ := raw.(map[string]any)
		name, _ := node["nodeName"].(string)
		byName[name] = node
	}
	names := slices.Sorted(maps.Keys(byName))
	for _, other := range names[1:] {
		r.warn("Tdarr has another node, %q; TdarrConfig sets workers on one node, so only %q is exported", other, names[0])
	}
	limits, _ := byName[names[0]]["workerLimits"].(map[string]any)
	workers := map[string]any{"nodeName": names[0]}
	for field, key := range tdarrWorkerTypes {
		for k, v := range limits {
			if strings.EqualFold(k, key) {
				workers[field] = v
			}
		}
	}
	return workers
}
