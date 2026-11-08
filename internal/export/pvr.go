package export

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"

	pvrclient "github.com/kyleseneker/media-operator/internal/client/pvr"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

func pvrApp(a pvrclient.App, newObj func() client.Object) app {
	return app{
		auth:   engine.AuthAPIKey,
		apiKey: true,
		newObj: newObj,
		export: func(r *reader) (map[string]any, error) {
			r.prefix = "/api/" + a.APIVersion
			return (&pvrExport{reader: r, app: a}).spec()
		},
	}
}

type pvrExport struct {
	*reader
	app              pvrclient.App
	qualityProfiles  map[float64]string
	metadataProfiles map[float64]string
	preferredSizes   bool
}

func (e *pvrExport) spec() (map[string]any, error) {
	spec := map[string]any{}
	for _, s := range pvrclient.Definition(e.app.APIVersion).Settings {
		section, err := e.hc.GetJSON(e.ctx, s.Path)
		if err != nil {
			return nil, err
		}
		delete(section, "id")
		spec[s.Name] = section
	}

	var err error
	if e.tags, err = e.names("/tag", "label"); err != nil {
		return nil, err
	}
	if e.qualityProfiles, err = e.names("/qualityprofile", "name"); err != nil {
		return nil, err
	}
	if e.app.MetadataProfiles {
		if e.metadataProfiles, err = e.names("/metadataprofile", "name"); err != nil {
			return nil, err
		}
	}

	if err := e.delayProfiles(spec); err != nil {
		return nil, err
	}
	definitions, err := e.list("/qualitydefinition")
	if err != nil {
		return nil, err
	}
	for _, d := range definitions {
		_, has := d["preferredSize"]
		e.preferredSizes = e.preferredSizes || has
	}
	return spec, e.resources(spec, []resource{
		{"tags", "/tag", "", tag},
		{"rootFolders", "/rootfolder", "", e.rootFolder},
		{"remotePathMappings", "/remotepathmapping", "", func(item map[string]any, _ map[string]map[string]any) map[string]any {
			return pick(item, "host", "remotePath", "localPath")
		}},
		{"downloadClients", "/downloadclient", "/downloadclient/schema", e.downloadClient},
		{"indexers", "/indexer", "/indexer/schema", e.indexer},
		{"customFormats", "/customformat", "", customFormat},
		{"qualityDefinitions", "/qualitydefinition", "", e.qualityDefinition},
		{"qualityProfiles", "/qualityprofile", "", qualityProfile},
		{"notifications", "/notification", "/notification/schema", e.notification},
		{"importLists", "/importlist", "/importlist/schema", e.importList},
	})
}

func (e *pvrExport) rootFolder(item map[string]any, _ map[string]map[string]any) map[string]any {
	rf := pick(item, "path")
	if e.app.MetadataProfiles {
		rf["name"] = item["name"]
		rf["defaultQualityProfileName"] = e.qualityProfiles[num(item["defaultQualityProfileId"])]
		rf["defaultMetadataProfileName"] = e.metadataProfiles[num(item["defaultMetadataProfileId"])]
	}
	return rf
}

func (e *pvrExport) downloadClient(item map[string]any, defaults map[string]map[string]any) map[string]any {
	dc := pick(item, "name", "enable", "protocol", "priority", "removeCompletedDownloads", "removeFailedDownloads", "implementation")
	contract(dc, item)
	protocol(dc)
	name, _ := item["name"].(string)
	values := fieldValues(item)
	for _, key := range []string{"host", "port", "useSsl"} {
		if v, ok := values[key]; ok {
			dc[key] = v
		}
	}
	if v, _ := values["urlBase"].(string); v != "" {
		dc["urlBase"] = v
	}
	if v, ok := values[e.app.CategoryField]; ok {
		dc["category"] = v
	}
	if v, _ := values["username"].(string); v != "" {
		dc["usernameSecretRef"] = e.creds.ref(v, "download-client", name, "username")
	}
	if reconciler.IsMasked(values["password"]) {
		dc["passwordSecretRef"] = e.creds.ref("", "download-client", name, "password")
	}
	if reconciler.IsMasked(values["apiKey"]) {
		dc["apiKeySecretRef"] = e.creds.ref("", "download-client", name, "api-key")
	}
	skip := map[string]bool{
		"host": true, "port": true, "useSsl": true, "urlBase": true,
		"username": true, "password": true, "apiKey": true, e.app.CategoryField: true,
	}
	dc["fields"] = e.fields(item, defaults, skip, "download-client", false)
	dc["tagLabels"] = e.tagLabels(item)
	return dc
}

func (e *pvrExport) indexer(item map[string]any, defaults map[string]map[string]any) map[string]any {
	idx := pick(item, "name", "protocol", "implementation", "priority", "enableRss", "enableAutomaticSearch", "enableInteractiveSearch")
	contract(idx, item)
	protocol(idx)
	idx["fields"] = e.fields(item, defaults, nil, "indexer", true)
	idx["tagLabels"] = e.tagLabels(item)
	return idx
}

func (e *pvrExport) importList(item map[string]any, defaults map[string]map[string]any) map[string]any {
	il := pick(item, "name", "implementation", "seasonFolder", "searchOnAdd", "rootFolderPath", "seriesType",
		"minimumAvailability", "listOrder")
	names := e.app.ImportListFields
	if names.Enable != "" {
		il["enable"] = item[names.Enable]
	}
	if names.AutomaticAdd != "" {
		il["enableAutomaticAdd"] = item[names.AutomaticAdd]
	}
	if v, ok := item[names.Monitor].(string); ok && names.Monitor != "" {
		il["monitor"] = v
	}
	if id, ok := item["qualityProfileId"].(float64); ok {
		il["qualityProfileName"] = e.qualityProfiles[id]
	}
	if id, ok := item["metadataProfileId"].(float64); ok && e.app.MetadataProfiles {
		il["metadataProfileName"] = e.metadataProfiles[id]
	}
	contract(il, item)
	il["fields"] = e.fields(item, defaults, nil, "import-list", true)
	il["tagLabels"] = e.tagLabels(item)
	return il
}

// qualityDefinition writes the app's unlimited, null or left out, as the
// config's 0. Readarr has no preferred size.
func (e *pvrExport) qualityDefinition(item map[string]any, _ map[string]map[string]any) map[string]any {
	quality, _ := item["quality"].(map[string]any)
	name, _ := quality["name"].(string)
	qd := map[string]any{"quality": name, "minSize": item["minSize"]}
	if title, _ := item["title"].(string); title != name {
		qd["title"] = title
	}
	qd["maxSize"] = item["maxSize"]
	if item["maxSize"] == nil {
		qd["maxSize"] = 0
	}
	preferred := item["preferredSize"]
	switch {
	case !e.preferredSizes:
	case preferred == nil:
		qd["preferredSize"] = 0
	case num(preferred) == 0:
		e.warn("quality definition %q prefers the smallest release, which the config can't express because 0 means unlimited there; set it in the app", name)
	default:
		qd["preferredSize"] = preferred
	}
	return qd
}

// delayProfiles exports profiles in the order the app checks them.
func (e *pvrExport) delayProfiles(spec map[string]any) error {
	profiles, err := e.list("/delayprofile")
	if err != nil {
		return err
	}
	slices.SortStableFunc(profiles, func(a, b map[string]any) int { return cmp.Compare(num(a["order"]), num(b["order"])) })
	out := make([]any, 0, len(profiles))
	for _, profile := range profiles {
		dp := pick(profile, "bypassIfHighestQuality", "bypassIfAboveCustomFormatScore", "minimumCustomFormatScore")
		if items, ok := profile["items"]; ok {
			for i, item := range list(items) {
				protocol, _ := item["protocol"].(string)
				key := strings.TrimSuffix(strings.ToLower(protocol), "downloadprotocol")
				if key != "usenet" && key != "torrent" {
					continue
				}
				dp["enable"+strings.ToUpper(key[:1])+key[1:]] = item["allowed"]
				dp[key+"Delay"] = item["delay"]
				if i == 0 {
					dp["preferredProtocol"] = key
				}
			}
		} else {
			maps.Copy(dp, pick(profile, "enableUsenet", "enableTorrent", "usenetDelay", "torrentDelay"))
			if p, _ := profile["preferredProtocol"].(string); p == "usenet" || p == "torrent" {
				dp["preferredProtocol"] = p
			}
		}
		if labels := e.tagLabels(profile); len(labels) > 0 {
			dp["tagLabels"] = labels
		}
		out = append(out, dp)
	}
	if len(out) > 0 {
		spec["delayProfiles"] = out
	}
	return nil
}

func customFormat(item map[string]any, _ map[string]map[string]any) map[string]any {
	cf := pick(item, "name", "includeCustomFormatWhenRenaming")
	specifications := list(item["specifications"])
	specs := make([]any, 0, len(specifications))
	for _, s := range specifications {
		spec := pick(s, "name", "implementation", "negate", "required")
		var fields []any
		for _, f := range fieldList(s) {
			if f["value"] != nil {
				fields = append(fields, pick(f, "name", "value"))
			}
		}
		spec["fields"] = fields
		specs = append(specs, spec)
	}
	cf["specifications"] = specs
	return cf
}

func qualityProfile(item map[string]any, _ map[string]map[string]any) map[string]any {
	qp := pick(item, "name", "upgradeAllowed", "minFormatScore", "cutoffFormatScore", "minUpgradeFormatScore")
	cutoff := num(item["cutoff"])
	var items []any
	for _, it := range list(item["items"]) {
		allowed := it["allowed"]
		if quality, ok := it["quality"].(map[string]any); ok {
			items = append(items, map[string]any{"quality": pick(quality, "id", "name"), "allowed": allowed})
			if num(quality["id"]) == cutoff {
				qp["cutoff"] = quality["name"]
			}
			continue
		}
		var children []any
		for _, child := range list(it["items"]) {
			quality, _ := child["quality"].(map[string]any)
			children = append(children, map[string]any{"quality": pick(quality, "id", "name"), "allowed": child["allowed"]})
		}
		items = append(items, map[string]any{"name": it["name"], "items": children, "allowed": allowed})
		if num(it["id"]) == cutoff {
			qp["cutoff"] = it["name"]
		}
	}
	qp["items"] = items
	var formats []any
	for _, f := range list(item["formatItems"]) {
		if num(f["score"]) != 0 {
			formats = append(formats, pick(f, "name", "score"))
		}
	}
	if len(formats) > 0 {
		qp["formatItems"] = formats
	}
	return qp
}
