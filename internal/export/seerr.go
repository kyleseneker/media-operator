package export

import (
	"errors"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"slices"
	"strings"

	requestsv1alpha1 "github.com/kyleseneker/media-operator/api/requests/v1alpha1"
	seerrclient "github.com/kyleseneker/media-operator/internal/client/seerr"
	"github.com/kyleseneker/media-operator/internal/engine"
)

var seerrNotificationAgents = []string{
	"discord", "email", "gotify", "lunasea", "ntfy", "pushbullet", "pushover", "slack", "telegram", "webhook", "webpush",
}

var seerrCredentialOptions = []string{"token", "password", "key", "secret", "auth", "webhookurl"}

const (
	seerrPlex     = 1
	seerrJellyfin = 2
)

func exportSeerr(r *reader) (map[string]any, error) {
	sc := seerrclient.NewClient(r.hc)
	sc.SetAPIKey(r.apiKey)
	main, err := sc.Get(r.ctx, "/api/v1/settings/main")
	if err != nil {
		return nil, err
	}
	spec := map[string]any{"main": seerrFields(main, reflect.TypeFor[requestsv1alpha1.SeerrMain]())}

	switch int(num(main["mediaServerType"])) {
	case seerrJellyfin:
		jf, err := sc.Get(r.ctx, "/api/v1/settings/jellyfin")
		if err != nil {
			return nil, err
		}
		auth := pick(jf, "port", "useSsl", "urlBase")
		auth["hostname"] = jf["ip"]
		auth["usernameSecretRef"] = r.creds.ref("", "jellyfin", "username")
		auth["passwordSecretRef"] = r.creds.ref("", "jellyfin", "password")
		spec["jellyfinAuth"] = auth
	case seerrPlex:
		plex, err := sc.Get(r.ctx, "/api/v1/settings/plex")
		if err != nil {
			return nil, err
		}
		auth := pick(plex, "port", "useSsl")
		auth["hostname"] = plex["ip"]
		auth["tokenSecretRef"] = r.creds.ref("", "plex", "token")
		spec["plexAuth"] = auth
	}

	for _, service := range []string{"sonarr", "radarr"} {
		servers, err := sc.GetList(r.ctx, "/api/v1/settings/"+service)
		if err != nil {
			return nil, err
		}
		if len(servers) == 0 {
			continue
		}
		chosen := servers[0]
		for _, s := range servers {
			if s["isDefault"] == true && s["is4k"] != true {
				chosen = s
			}
		}
		for _, s := range servers {
			if s["id"] != chosen["id"] {
				r.warn("Seerr has another %s server, %q; SeerrConfig holds one, so only %q is exported", service, s["name"], chosen["name"])
			}
		}
		spec[service] = r.seerrService(service, chosen)
	}

	var notifications []any
	for _, agent := range seerrNotificationAgents {
		settings, err := sc.Get(r.ctx, "/api/v1/settings/notifications/"+agent)
		var apiErr *engine.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			continue
		}
		if err != nil {
			return nil, err
		}
		if settings["enabled"] != true {
			continue
		}
		notification := pick(settings, "enabled", "types")
		notification["agent"] = agent
		if options := r.seerrOptions(agent, settings["options"]); len(options) > 0 {
			notification["options"] = options
		}
		notifications = append(notifications, notification)
	}
	if len(notifications) > 0 {
		spec["notifications"] = notifications
	}
	return spec, nil
}

func (r *reader) seerrService(service string, s map[string]any) map[string]any {
	out := seerrFields(s, reflect.TypeFor[requestsv1alpha1.SeerrServiceConnection]())
	if name, _ := s["activeProfileName"].(string); name != "" {
		delete(out, "activeProfileId")
	}
	key, _ := s["apiKey"].(string)
	out["apiKeySecretRef"] = r.creds.ref(key, service, "api-key")
	return out
}

func (r *reader) seerrOptions(agent string, v any) map[string]any {
	raw, _ := v.(map[string]any)
	out := map[string]any{}
	for _, key := range slices.Sorted(maps.Keys(raw)) {
		var text string
		switch x := raw[key].(type) {
		case nil:
			continue
		case string:
			text = x
		case float64, bool:
			text = fmt.Sprint(x)
		default:
			r.warn("notification %q: SeerrConfig options hold text, so %s stays as set in Seerr", agent, key)
			continue
		}
		if text == "" {
			continue
		}
		lower := strings.ToLower(key)
		if slices.ContainsFunc(seerrCredentialOptions, func(s string) bool { return strings.Contains(lower, s) }) {
			r.warn("notification %q: %s looks like a credential, and SeerrConfig holds options as plain text", agent, key)
		}
		out[key] = text
	}
	return out
}

func seerrFields(values map[string]any, t reflect.Type) map[string]any {
	out := map[string]any{}
	for field := range t.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if field.Type.Kind() == reflect.Struct {
			continue
		}
		if v, ok := values[name]; ok && v != nil {
			out[name] = v
		}
	}
	return out
}
