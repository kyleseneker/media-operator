package bazarr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"slices"
	"strings"

	subtitlesv1alpha1 "github.com/kyleseneker/media-operator/api/subtitles/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/engine"
)

// Client wraps engine.HTTPClient for Bazarr's form-encoded API.
type Client struct {
	hc *engine.HTTPClient
}

// NewClient creates a new Bazarr API client.
func NewClient(hc *engine.HTTPClient) *Client {
	return &Client{hc: hc}
}

// Ping checks if Bazarr is reachable.
func (c *Client) Ping(ctx context.Context) error {
	return c.hc.Ping(ctx, "/api/system/health")
}

// buildLanguageProfiles renders profiles in the shape Bazarr's languages-profiles
// handler indexes into. cutoff, mustContain, mustNotContain and originalFormat are
// read unconditionally, so a profile missing any of them raises a KeyError and no
// profile is stored.
func buildLanguageProfiles(profiles []subtitlesv1alpha1.BazarrLanguageProfile) []map[string]any {
	out := make([]map[string]any, 0, len(profiles))
	for i, p := range profiles {
		id := i + 1
		if p.ProfileId != nil {
			id = *p.ProfileId
		}
		items := make([]map[string]any, 0, len(p.Items))
		for j, it := range p.Items {
			items = append(items, map[string]any{
				"id":            j + 1,
				"language":      it.Language,
				"hi":            boolStr(it.HI),
				"forced":        boolStr(it.Forced),
				"audio_exclude": boolStr(it.AudioExclude),
			})
		}
		out = append(out, map[string]any{
			"profileId":      id,
			"name":           p.Name,
			"items":          items,
			"cutoff":         nil,
			"mustContain":    []string{},
			"mustNotContain": []string{},
			"originalFormat": false,
		})
	}
	return out
}

// boolStr renders a bool the way Bazarr stores profile item flags.
func boolStr(b *bool) string {
	if b != nil && *b {
		return "True"
	}
	return "False"
}

// GetSettings returns every Bazarr setting, keyed by section.
func (c *Client) GetSettings(ctx context.Context) (map[string]any, error) {
	return c.hc.GetJSON(ctx, "/api/system/settings")
}

// EnabledLanguagesField is the settings form field that sets which languages
// are enabled, one value per language. Bazarr disables every language it
// doesn't list.
const EnabledLanguagesField = "languages-enabled"

// EnabledLanguages returns the ISO 639-1 codes of the enabled languages.
func (c *Client) EnabledLanguages(ctx context.Context) ([]string, error) {
	languages, err := c.hc.GetJSONList(ctx, "/api/system/languages")
	if err != nil {
		return nil, err
	}
	var enabled []string
	for _, l := range languages {
		if code, _ := l["code2"].(string); code != "" && l["enabled"] == true {
			enabled = append(enabled, code)
		}
	}
	return enabled, nil
}

// GetLanguageProfiles returns the stored language profiles.
func (c *Client) GetLanguageProfiles(ctx context.Context) ([]map[string]any, error) {
	return c.hc.GetJSONList(ctx, "/api/system/languages/profiles")
}

// PostForm posts a raw form to the given path.
func (c *Client) PostForm(ctx context.Context, path string, form url.Values) error {
	_, err := c.hc.DoForm(ctx, path, form)
	return err
}

// LanguagesForm renders enabled languages and profiles as the settings form
// fields Bazarr accepts. It is empty when neither is declared.
func LanguagesForm(langs *subtitlesv1alpha1.BazarrLanguages) (url.Values, error) {
	form := url.Values{}
	if len(langs.Enabled) > 0 {
		form[EnabledLanguagesField] = slices.Clone(langs.Enabled)
	}
	if len(langs.Profiles) > 0 {
		profilesJSON, err := json.Marshal(buildLanguageProfiles(langs.Profiles))
		if err != nil {
			return nil, fmt.Errorf("marshaling language profiles: %w", err)
		}
		form.Set("languages-profiles", string(profilesJSON))
	}
	return form, nil
}

// settingNameOverrides maps a spec field to the Bazarr setting it drives where
// snake_case alone does not get there.
var settingNameOverrides = map[string]map[string]string{
	"sonarr": {"host": "ip", "basePath": "base_url", "syncInterval": "series_sync"},
	"radarr": {"host": "ip", "basePath": "base_url", "syncInterval": "movies_sync"},
}

// SettingName converts a spec field name to the key Bazarr stores it under.
// Bazarr accepts and persists unknown keys without complaint, so a wrong name
// is silently inert rather than an error.
func SettingName(section, jsonName string) string {
	if o, ok := settingNameOverrides[section][jsonName]; ok {
		return o
	}
	var b strings.Builder
	for i, r := range jsonName {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r + 32)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// StructToFormData converts a struct to form data with "settings-{section}-{field}=value" keys.
// Skips nil pointers and empty strings.
func StructToFormData(section string, obj any) url.Values {
	form := url.Values{}
	v := reflect.ValueOf(obj)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return form
		}
		v = v.Elem()
	}
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fv := v.Field(i)

		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}
		jsonName, _, _ := strings.Cut(jsonTag, ",")

		// Skip zero/nil values
		if fv.Kind() == reflect.Pointer && fv.IsNil() {
			continue
		}
		if fv.Kind() == reflect.String && fv.String() == "" {
			continue
		}
		// Only scalars are settings. A struct such as a Secret reference would
		// otherwise be stored in Bazarr's config as its printed form.
		if k := reflect.Indirect(fv).Kind(); k == reflect.Struct || k == reflect.Slice || k == reflect.Map {
			continue
		}

		key := fmt.Sprintf("settings-%s-%s", section, SettingName(section, jsonName))
		var val string
		switch fv.Kind() {
		case reflect.Pointer:
			val = fmt.Sprintf("%v", fv.Elem().Interface())
		default:
			val = fmt.Sprintf("%v", fv.Interface())
		}
		form.Set(key, val)
	}
	return form
}
