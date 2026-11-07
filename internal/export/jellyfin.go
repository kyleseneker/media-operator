package export

import (
	"reflect"
	"strings"

	mediaserversv1alpha1 "github.com/kyleseneker/media-operator/api/mediaservers/v1alpha1"
	jellyfinclient "github.com/kyleseneker/media-operator/internal/client/jellyfin"
)

func exportJellyfin(r *reader) (map[string]any, error) {
	jf := jellyfinclient.NewClient(r.hc)
	if err := jf.Authenticate(r.ctx, r.username, r.password); err != nil {
		return nil, err
	}
	server, err := jf.GetConfig(r.ctx, "/System/Configuration")
	if err != nil {
		return nil, err
	}
	encoding, err := jf.GetConfig(r.ctx, "/System/Configuration/encoding")
	if err != nil {
		return nil, err
	}
	spec := map[string]any{
		"adminUser": map[string]any{
			"usernameSecretRef": r.creds.ref(r.username, "username"),
			"passwordSecretRef": r.creds.ref(r.password, "password"),
		},
		"server":   jellyfinSettings(server, reflect.TypeFor[mediaserversv1alpha1.JellyfinServer]()),
		"encoding": jellyfinSettings(encoding, reflect.TypeFor[mediaserversv1alpha1.JellyfinEncoding]()),
	}

	folders, err := jf.ListLibraries(r.ctx)
	if err != nil {
		return nil, err
	}
	var libraries []any
	for _, f := range folders {
		collection, _ := f["CollectionType"].(string)
		if collection == "" {
			collection = "mixed"
		}
		library := map[string]any{"name": f["Name"], "collectionType": collection, "paths": f["Locations"]}
		options, _ := f["LibraryOptions"].(map[string]any)
		for _, field := range []string{
			"enableRealtimeMonitor", "enableTrickplayImageExtraction", "automaticRefreshIntervalDays",
			"preferredMetadataLanguage", "metadataCountryCode",
		} {
			if v, ok := options[pascal(field)]; ok && v != nil {
				library[field] = v
			}
		}
		libraries = append(libraries, library)
	}
	if len(libraries) > 0 {
		spec["libraries"] = libraries
	}
	return spec, nil
}

func jellyfinSettings(values map[string]any, t reflect.Type) map[string]any {
	out := map[string]any{}
	for field := range t.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if v, ok := values[pascal(name)]; ok && v != nil {
			out[name] = v
		}
	}
	return out
}

func pascal(name string) string {
	return strings.ToUpper(name[:1]) + name[1:]
}
