package export

import (
	"errors"
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
		exported := make([]any, 0, len(servers))
		for _, s := range servers {
			exported = append(exported, r.seerrService(service, s))
		}
		spec[service] = exported
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
		options, secrets := r.seerrOptions(agent, settings["options"])
		if len(options) > 0 {
			notification["options"] = options
		}
		if len(secrets) > 0 {
			notification["secretOptions"] = secrets
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
	name, _ := s["name"].(string)
	out["apiKeySecretRef"] = r.creds.ref(key, service, name, "api-key")
	return out
}

// seerrOptions splits an agent's options into plain ones and ones that look
// like credentials, which go to the Secret.
func (r *reader) seerrOptions(agent string, v any) (map[string]any, map[string]any) {
	raw, _ := v.(map[string]any)
	options, secrets := map[string]any{}, map[string]any{}
	for _, key := range slices.Sorted(maps.Keys(raw)) {
		switch x := raw[key].(type) {
		case nil:
		case string:
			lower := strings.ToLower(key)
			switch {
			case x == "":
			case slices.ContainsFunc(seerrCredentialOptions, func(s string) bool { return strings.Contains(lower, s) }):
				secrets[key] = r.creds.ref(x, "notification", agent, key)
			default:
				options[key] = x
			}
		case float64, bool:
			options[key] = x
		default:
			r.warn("notification %q: SeerrConfig can't express %s, so it stays as set in Seerr", agent, key)
		}
	}
	return options, secrets
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
