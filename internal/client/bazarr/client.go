package bazarr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"

	servarrv1alpha1 "github.com/kyleseneker/media-operator/api/servarr/v1alpha1"
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

// PostSettings serializes a struct to form data under the given section and posts it to /api/system/settings.
func (c *Client) PostSettings(ctx context.Context, section string, settings interface{}) error {
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
func (c *Client) ReconcileLanguages(ctx context.Context, langs *servarrv1alpha1.BazarrLanguages) error {
	form := url.Values{}

	if len(langs.Enabled) > 0 {
		enabledJSON, err := json.Marshal(langs.Enabled)
		if err != nil {
			return fmt.Errorf("marshaling enabled languages: %w", err)
		}
		form.Set("settings-general-enabled_languages", string(enabledJSON))
	}

	if len(langs.Profiles) > 0 {
		profilesJSON, err := json.Marshal(langs.Profiles)
		if err != nil {
			return fmt.Errorf("marshaling language profiles: %w", err)
		}
		form.Set("settings-general-language_profiles", string(profilesJSON))
	}

	if len(form) == 0 {
		return nil
	}

	return c.PostForm(ctx, "/api/system/settings", form)
}

// StructToFormData converts a struct to form data with "settings-{section}-{field}=value" keys.
// Uses json tags for field names. Skips nil pointers and empty strings.
func StructToFormData(section string, obj interface{}) url.Values {
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

		key := fmt.Sprintf("settings-%s-%s", section, jsonName)
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
