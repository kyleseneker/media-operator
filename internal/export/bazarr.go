package export

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	subtitlesv1alpha1 "github.com/kyleseneker/media-operator/api/subtitles/v1alpha1"
	bazarrclient "github.com/kyleseneker/media-operator/internal/client/bazarr"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

var bazarrSecretSettings = []string{"password", "apikey", "api_key", "token", "secret", "passkey", "cookie"}

func exportBazarr(r *reader) (map[string]any, error) {
	bc := bazarrclient.NewClient(r.hc)
	settings, err := bc.GetSettings(r.ctx)
	if err != nil {
		return nil, err
	}
	section := func(name string) map[string]any {
		m, _ := settings[name].(map[string]any)
		return m
	}
	general := section("general")
	spec := map[string]any{
		"general": bazarrSettings("general", general, reflect.TypeFor[subtitlesv1alpha1.BazarrGeneral]()),
		"subSync": bazarrSettings("subsync", section("subsync"), reflect.TypeFor[subtitlesv1alpha1.BazarrSubSync]()),
	}
	for _, c := range []struct{ key, section, use string }{
		{"sonarrConnection", "sonarr", "use_sonarr"},
		{"radarrConnection", "radarr", "use_radarr"},
	} {
		if use, _ := general[c.use].(bool); !use {
			continue
		}
		values := section(c.section)
		conn := bazarrSettings(c.section, values, reflect.TypeFor[subtitlesv1alpha1.BazarrAppConnection]())
		key, _ := values["apikey"].(string)
		if reconciler.IsMasked(key) {
			key = ""
		}
		conn["apiKeySecretRef"] = r.creds.ref(key, c.section, "api-key")
		spec[c.key] = conn
	}

	var providers []any
	for _, name := range texts(general["enabled_providers"]) {
		provider := map[string]any{"name": name}
		if values := r.bazarrProviderSettings(name, section(name)); len(values) > 0 {
			provider["settings"] = values
		}
		providers = append(providers, provider)
	}
	if len(providers) > 0 {
		spec["providers"] = providers
	}

	languages := map[string]any{}
	if enabled := texts(general["enabled_languages"]); len(enabled) > 0 {
		languages["enabled"] = enabled
	}
	profiles, err := bc.GetLanguageProfiles(r.ctx)
	if err != nil {
		return nil, err
	}
	if len(profiles) > 0 {
		languages["profiles"] = r.bazarrProfiles(profiles)
	}
	if len(languages) > 0 {
		spec["languages"] = languages
	}
	return spec, nil
}

func bazarrSettings(section string, values map[string]any, t reflect.Type) map[string]any {
	out := map[string]any{}
	for field := range t.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if k := field.Type.Kind(); k == reflect.Struct || k == reflect.Slice || k == reflect.Map {
			continue
		}
		if v, ok := values[bazarrclient.SettingName(section, name)]; ok && v != nil {
			out[name] = v
		}
	}
	return out
}

func (r *reader) bazarrProviderSettings(provider string, values map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range slices.Sorted(maps.Keys(values)) {
		var text string
		switch v := values[key].(type) {
		case nil:
			continue
		case string:
			text = v
		case float64:
			text = strconv.FormatFloat(v, 'f', -1, 64)
		case bool:
			text = fmt.Sprint(v)
		case []any:
			raw, _ := json.Marshal(v)
			text = string(raw)
		default:
			r.warn("provider %q: BazarrConfig provider settings hold text, so %s stays as set in Bazarr", provider, key)
			continue
		}
		if text == "" || text == "[]" {
			continue
		}
		if slices.ContainsFunc(bazarrSecretSettings, func(s string) bool { return strings.Contains(key, s) }) {
			if reconciler.IsMasked(text) {
				text = ""
			}
			ref := r.creds.ref(text, "provider", provider, key)
			text = fmt.Sprintf("ENV:%s:%s", ref["name"], ref["key"])
		}
		out[key] = text
	}
	return out
}

func (r *reader) bazarrProfiles(profiles []map[string]any) []any {
	out := make([]any, 0, len(profiles))
	for _, p := range profiles {
		name, _ := p["name"].(string)
		if p["cutoff"] != nil || len(texts(p["mustContain"])) > 0 || len(texts(p["mustNotContain"])) > 0 || p["originalFormat"] == true {
			r.warn("language profile %q: BazarrConfig can't express its cutoff, must-contain, must-not-contain or "+
				"original-format setting, and applying the config would clear it", name)
		}
		var items []any
		for j, it := range list(p["items"]) {
			if num(it["id"]) != float64(j+1) {
				r.warn("language profile %q: Bazarr numbers its items differently than BazarrConfig, so applying "+
					"the config would renumber them", name)
			}
			items = append(items, map[string]any{
				"language":     it["language"],
				"hi":           bazarrFlag(it["hi"]),
				"forced":       bazarrFlag(it["forced"]),
				"audioExclude": bazarrFlag(it["audio_exclude"]),
			})
		}
		out = append(out, map[string]any{"profileId": p["profileId"], "name": name, "items": items})
	}
	return out
}

func bazarrFlag(v any) bool {
	return v == true || v == "True"
}

func texts(v any) []string {
	raw, _ := v.([]any)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
