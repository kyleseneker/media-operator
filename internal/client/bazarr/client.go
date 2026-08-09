package bazarr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
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

// PostSettings serializes a struct to form data under the given section and posts it to /api/system/settings.
func (c *Client) PostSettings(ctx context.Context, section string, settings any) error {
	form := StructToFormData(section, settings)
	return c.PostForm(ctx, "/api/system/settings", form)
}

// PostForm posts a raw form to the given path.
func (c *Client) PostForm(ctx context.Context, path string, form url.Values) error {
	_, err := c.hc.DoForm(ctx, path, form)
	return err
}

// ReconcileLanguages posts language configuration to /api/system/settings.
// Returns nil without calling the API if neither enabled languages nor profiles are set.
func (c *Client) ReconcileLanguages(ctx context.Context, langs *subtitlesv1alpha1.BazarrLanguages) error {
	form := url.Values{}

	if len(langs.Enabled) > 0 {
		enabledJSON, err := json.Marshal(langs.Enabled)
		if err != nil {
			return fmt.Errorf("marshaling enabled languages: %w", err)
		}
		form.Set("settings-general-enabled_languages", string(enabledJSON))
	}

	if len(langs.Profiles) > 0 {
		profilesJSON, err := json.Marshal(buildLanguageProfiles(langs.Profiles))
		if err != nil {
			return fmt.Errorf("marshaling language profiles: %w", err)
		}
		form.Set("languages-profiles", string(profilesJSON))
	}

	if len(form) == 0 {
		return nil
	}

	return c.PostForm(ctx, "/api/system/settings", form)
}

// settingNameOverrides maps a spec field to the Bazarr setting it drives where
// snake_case alone does not get there.
var settingNameOverrides = map[string]map[string]string{
	"sonarr": {"host": "ip", "basePath": "base_url", "syncInterval": "series_sync"},
	"radarr": {"host": "ip", "basePath": "base_url", "syncInterval": "movies_sync"},
}

// settingName converts a spec field name to the key Bazarr stores it under.
// Bazarr accepts and persists unknown keys without complaint, so a wrong name
// is silently inert rather than an error.
func settingName(section, jsonName string) string {
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
	if v.Kind() == reflect.Ptr {
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
		jsonName := strings.Split(jsonTag, ",")[0]

		// Skip zero/nil values
		if fv.Kind() == reflect.Ptr && fv.IsNil() {
			continue
		}
		if fv.Kind() == reflect.String && fv.String() == "" {
			continue
		}

		key := fmt.Sprintf("settings-%s-%s", section, settingName(section, jsonName))
		var val string
		switch fv.Kind() {
		case reflect.Ptr:
			val = fmt.Sprintf("%v", fv.Elem().Interface())
		default:
			val = fmt.Sprintf("%v", fv.Interface())
		}
		form.Set(key, val)
	}
	return form
}
