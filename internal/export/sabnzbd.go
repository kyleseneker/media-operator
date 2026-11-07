package export

import (
	"fmt"
	"strconv"

	sabnzbdclient "github.com/kyleseneker/media-operator/internal/client/sabnzbd"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

var sabnzbdFolders = map[string]string{
	"completeDir": "complete_dir", "incompleteDir": "download_dir", "tempDownloadDir": "tmp_dir",
	"nzbBackupDir": "nzb_backup_dir", "scriptDir": "script_dir",
}

var sabnzbdSwitches = map[string]string{
	"preCheck": "pre_check", "pauseOnPostProcessing": "pause_on_post_processing",
}

func exportSabnzbd(r *reader) (map[string]any, error) {
	current, err := sabnzbdclient.NewClient(r.hc, r.apiKey).GetConfig(r.ctx)
	if err != nil {
		return nil, err
	}
	misc, _ := current["misc"].(map[string]any)
	spec := map[string]any{}

	folders := map[string]any{}
	for field, key := range sabnzbdFolders {
		if s := sabText(misc[key]); s != "" {
			folders[field] = s
		}
	}
	if len(folders) > 0 {
		spec["folders"] = folders
	}

	general := map[string]any{}
	if s := sabText(misc["bandwidth_max"]); s != "" {
		general["downloadSpeedLimit"] = s
	}
	for field, key := range sabnzbdSwitches {
		if v, ok := misc[key]; ok {
			general[field] = sabSwitch(v)
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
