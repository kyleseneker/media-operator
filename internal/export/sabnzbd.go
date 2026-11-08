package export

import (
	"fmt"
	"strconv"

	sabnzbdclient "github.com/kyleseneker/media-operator/internal/client/sabnzbd"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

func exportSabnzbd(r *reader) (map[string]any, error) {
	current, err := sabnzbdclient.NewClient(r.hc, r.apiKey).GetConfig(r.ctx)
	if err != nil {
		return nil, err
	}
	misc, _ := current["misc"].(map[string]any)
	spec := map[string]any{}

	folders := map[string]any{}
	for field, key := range sabnzbdclient.Folders {
		if s := sabText(misc[key]); s != "" {
			folders[field] = s
		}
	}
	if len(folders) > 0 {
		spec["folders"] = folders
	}

	general := map[string]any{}
	for field, key := range sabnzbdclient.Texts {
		if s := sabText(misc[key]); s != "" {
			general[field] = s
		}
	}
	for field, key := range sabnzbdclient.Numbers {
		if n := sabNumber(misc[key]); n != nil {
			general[field] = n
		}
	}
	for field, key := range sabnzbdclient.Switches {
		if v, ok := misc[key]; ok {
			general[field] = sabSwitch(v)
		}
	}
	for field, key := range sabnzbdclient.Lists {
		if items := sabList(misc[key]); len(items) > 0 {
			general[field] = items
		}
	}
	for field, choice := range sabnzbdclient.Choices {
		if name, ok := sabChoice(choice.Values, misc[choice.Key]); ok {
			general[field] = name
		} else if v, set := misc[choice.Key]; set {
			r.warn("SabnzbdConfig can't express %s %v; %s is left out", choice.Key, v, field)
		}
	}
	switch sort := sabText(misc["auto_sort"]); sort {
	case "", "0":
		general["autoSort"] = false
	case "1", "avg_age asc":
		general["autoSort"] = true
	default:
		r.warn("SabnzbdConfig can't express the queue sort %q; autoSort is left out", sort)
	}
	if len(general) > 0 {
		spec["general"] = general
	}

	var servers []any
	for _, s := range list(current["servers"]) {
		name := sabText(s["name"])
		server := map[string]any{
			"name": name, "host": s["host"], "port": sabNumber(s["port"]), "ssl": sabSwitch(s["ssl"]),
			"connections": sabNumber(s["connections"]), "priority": sabNumber(s["priority"]),
			"retention": sabNumber(s["retention"]), "enable": sabSwitch(s["enable"]),
			"optional": sabSwitch(s["optional"]), "required": sabSwitch(s["required"]), "timeout": sabNumber(s["timeout"]),
		}
		if verify, ok := sabChoice(sabnzbdclient.SSLVerify, s["ssl_verify"]); ok {
			server["sslVerify"] = verify
		}
		if username := sabText(s["username"]); username != "" {
			server["usernameSecretRef"] = r.creds.ref(username, "server", name, "username")
		}
		if password := sabText(s["password"]); password != "" {
			if reconciler.IsMasked(password) {
				password = ""
			}
			server["passwordSecretRef"] = r.creds.ref(password, "server", name, "password")
		}
		servers = append(servers, server)
	}
	if len(servers) > 0 {
		spec["servers"] = servers
	}

	var categories []any
	for _, c := range list(current["categories"]) {
		category := map[string]any{"name": sabText(c["name"]), "priority": sabNumber(c["priority"])}
		if dir := sabText(c["dir"]); dir != "" {
			category["dir"] = dir
		}
		if script := sabText(c["script"]); script != "" {
			category["script"] = script
		}
		if pp, ok := sabChoice(sabnzbdclient.PostProcessing, c["pp"]); ok {
			category["postProcessing"] = pp
		}
		categories = append(categories, category)
	}
	if len(categories) > 0 {
		spec["categories"] = categories
	}
	return spec, nil
}

func sabText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return fmt.Sprint(x)
	}
}

func sabNumber(v any) any {
	if n, err := strconv.Atoi(sabText(v)); err == nil {
		return n
	}
	return nil
}

func sabSwitch(v any) bool {
	return v == true || sabText(v) == "1"
}

func sabList(v any) []string {
	raw, _ := v.([]any)
	items := make([]string, 0, len(raw))
	for _, item := range raw {
		items = append(items, sabText(item))
	}
	return items
}

// sabChoice names the value SABnzbd stores for a setting it numbers.
func sabChoice(names map[string]string, v any) (string, bool) {
	value := sabText(v)
	for name, n := range names {
		if n == value {
			return name, true
		}
	}
	return "", false
}
