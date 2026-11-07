package export

import (
	"maps"
	"slices"
	"strconv"

	qbclient "github.com/kyleseneker/media-operator/internal/client/qbittorrent"
)

var qbPreferences = map[string]string{
	"savePath": "save_path", "locale": "locale", "tempPathEnabled": "temp_path_enabled",
	"dht": "dht", "pex": "pex", "lsd": "lsd", "encryption": "encryption",
	"maxConnec": "max_connec", "maxConnecPerTorrent": "max_connec_per_torrent",
	"maxUploads": "max_uploads", "maxUploadsPerTorrent": "max_uploads_per_torrent",
	"maxRatioEnabled": "max_ratio_enabled", "maxSeedingTimeEnabled": "max_seeding_time_enabled",
	"maxSeedingTime": "max_seeding_time", "preallocateAll": "preallocate_all", "webUiPort": "web_ui_port",
	"bypassAuthSubnetWhitelistEnabled": "bypass_auth_subnet_whitelist_enabled",
	"bypassAuthSubnetWhitelist":        "bypass_auth_subnet_whitelist",
}

func exportQBittorrent(r *reader) (map[string]any, error) {
	qb := qbclient.NewClient(r.hc, r.username, r.password)
	if err := qb.Login(r.ctx); err != nil {
		return nil, err
	}
	prefs, err := qb.GetPreferences(r.ctx)
	if err != nil {
		return nil, err
	}
	preferences := map[string]any{}
	for field, key := range qbPreferences {
		if v, ok := prefs[key]; ok && v != nil {
			preferences[field] = v
		}
	}
	if ratio, ok := prefs["max_ratio"].(float64); ok {
		preferences["maxRatio"] = strconv.FormatFloat(ratio, 'f', -1, 64)
	}
	if act, ok := prefs["max_ratio_act"].(float64); ok {
		for name, value := range qbclient.MaxRatioActions {
			if float64(value) == act {
				preferences["maxRatioAction"] = name
			}
		}
	}

	categories, err := qb.ListCategories(r.ctx)
	if err != nil {
		return nil, err
	}
	var cats []any
	for _, name := range slices.Sorted(maps.Keys(categories)) {
		cat, _ := categories[name].(map[string]any)
		cats = append(cats, map[string]any{"name": name, "savePath": cat["savePath"]})
	}

	spec := map[string]any{
		"connection": map[string]any{
			"usernameSecretRef": r.creds.ref(r.username, "username"),
			"passwordSecretRef": r.creds.ref(r.password, "password"),
		},
		"preferences": preferences,
	}
	if len(cats) > 0 {
		spec["categories"] = cats
	}
	return spec, nil
}
