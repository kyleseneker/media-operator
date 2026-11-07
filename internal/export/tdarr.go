package export

import (
	"fmt"
	"maps"
	"slices"
	"strings"

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

	spec := map[string]any{}
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

func (r *reader) tdarrLibrary(doc map[string]any, variables []map[string]any) map[string]any {
	lib := pick(doc, "name", "folder", "cache", "container", "containerFilter", "foldersToIgnore", "folderWatching",
		"processLibrary", "processTranscodes", "processHealthChecks", "scanOnStart", "scheduledScanFindNew",
		"scannerThreadCount", "ffmpeg", "priority", "flowId", "pluginStack")
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
	for _, hour := range list(doc["schedule"]) {
		if hour["checked"] != true {
			r.warn("library %q has a processing schedule; TdarrConfig turns every hour on, so applying the config would reset it", name)
			break
		}
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
