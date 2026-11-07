package export

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	mediaserversv1alpha1 "github.com/kyleseneker/media-operator/api/mediaservers/v1alpha1"
	plexclient "github.com/kyleseneker/media-operator/internal/client/plex"
)

var plexSections = map[string]reflect.Type{
	"server":     reflect.TypeFor[mediaserversv1alpha1.PlexServer](),
	"transcoder": reflect.TypeFor[mediaserversv1alpha1.PlexTranscoder](),
	"network":    reflect.TypeFor[mediaserversv1alpha1.PlexNetwork](),
}

func exportPlex(r *reader) (map[string]any, error) {
	pc := plexclient.NewClient(r.hc)
	settings, err := pc.GetSettings(r.ctx)
	if err != nil {
		return nil, err
	}
	spec := map[string]any{
		"connection": map[string]any{"tokenSecretRef": r.creds.ref(r.token, "token")},
	}
	for section, typ := range plexSections {
		out := map[string]any{}
		for field := range typ.Fields() {
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			id := plexclient.Preferences[section][name]
			value, listed := settings[id]
			if !listed {
				r.unreadable = append(r.unreadable, section+"."+name)
				r.warn("Plex doesn't list its %s preference, so %s.%s isn't exported; set it by hand if you manage it",
					id, section, name)
				continue
			}
			switch field.Type.Kind() {
			case reflect.String:
				if value != "" {
					out[name] = value
				}
			case reflect.Pointer:
				switch field.Type.Elem().Kind() {
				case reflect.Bool:
					out[name] = value == "1"
				case reflect.Int:
					if n, err := strconv.Atoi(value); err == nil {
						out[name] = n
					}
				default:
					return nil, fmt.Errorf("plex %s.%s: unsupported type %s", section, name, field.Type)
				}
			}
		}
		if len(out) > 0 {
			spec[section] = out
		}
	}

	sections, err := pc.ListLibraries(r.ctx)
	if err != nil {
		return nil, err
	}
	var libraries []any
	for _, s := range sections {
		var paths []any
		for _, location := range list(s["Location"]) {
			paths = append(paths, location["path"])
		}
		library := map[string]any{"name": s["title"], "type": s["type"], "paths": paths}
		for _, key := range []string{"language", "scanner", "agent"} {
			if v, _ := s[key].(string); v != "" {
				library[key] = v
			}
		}
		libraries = append(libraries, library)
	}
	if len(libraries) > 0 {
		spec["libraries"] = libraries
	}
	return spec, nil
}
