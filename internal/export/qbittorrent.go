package export

import (
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	qbclient "github.com/kyleseneker/media-operator/internal/client/qbittorrent"
)

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
	for field, key := range qbclient.Preferences {
		if v, ok := prefs[key]; ok && v != nil {
			preferences[field] = v
		}
	}
	for field, key := range map[string]string{"savePath": "save_path", "tempPath": "temp_path"} {
		if v, _ := prefs[key].(string); v != "" {
			preferences[field] = v
		}
	}
	for field, key := range qbclient.SpeedLimits {
		if limit, ok := prefs[key].(float64); ok {
			if math.Mod(limit, 1024) != 0 {
				r.warn("qBittorrent's %s is %v bytes per second, which isn't a whole number of KiB/s; exported rounded down", field, limit)
			}
			preferences[field] = int(limit) / 1024
		}
	}
	if days, ok := prefs["scheduler_days"].(float64); ok {
		for name, value := range qbclient.SchedulerDays {
			if float64(value) == days {
				preferences["schedulerDays"] = name
			}
		}
	}
	if names, _ := prefs["excluded_file_names"].(string); names != "" {
		preferences["excludedFileNames"] = strings.Split(names, "\n")
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
