package export

import (
	"context"
	"reflect"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	pvrclient "github.com/kyleseneker/media-operator/internal/client/pvr"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

func pvrApp(a pvrclient.App, newObj func() client.Object) app {
	return app{
		auth:   engine.AuthAPIKey,
		newObj: newObj,
		export: func(ctx context.Context, hc *engine.HTTPClient, creds *credentials) (map[string]any, error) {
			e := &pvrExport{ctx: ctx, hc: hc, app: a, creds: creds}
			return e.spec()
		},
	}
}

type pvrExport struct {
	ctx              context.Context
	hc               *engine.HTTPClient
	app              pvrclient.App
	creds            *credentials
	tags             map[float64]string
	qualityProfiles  map[float64]string
	metadataProfiles map[float64]string
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

	resources := []struct {
		key    string
		path   string
		schema string
		build  func(item map[string]any, defaults map[string]map[string]any) map[string]any
	}{
		{"tags", "/tag", "", func(item map[string]any, _ map[string]map[string]any) map[string]any {
			return map[string]any{"label": item["label"]}
		}},
		{"rootFolders", "/rootfolder", "", e.rootFolder},
		{"remotePathMappings", "/remotepathmapping", "", func(item map[string]any, _ map[string]map[string]any) map[string]any {
			return pick(item, "host", "remotePath", "localPath")
		}},
		{"downloadClients", "/downloadclient", "/downloadclient/schema", e.downloadClient},
		{"indexers", "/indexer", "/indexer/schema", e.indexer},
		{"customFormats", "/customformat", "", customFormat},
		{"qualityProfiles", "/qualityprofile", "", qualityProfile},
		{"notifications", "/notification", "/notification/schema", e.notification},
		{"importLists", "/importlist", "/importlist/schema", e.importList},
	}
	for _, r := range resources {
		items, err := e.list(r.path)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			continue
		}
		var defaults map[string]map[string]any
		if r.schema != "" {
			if defaults, err = e.defaults(r.schema); err != nil {
				return nil, err
			}
		}
		out := make([]any, 0, len(items))
		for _, item := range items {
			out = append(out, r.build(item, defaults))
		}
		spec[r.key] = out
	}
	return spec, nil
}

func (e *pvrExport) list(path string) ([]map[string]any, error) {
	return e.hc.GetJSONList(e.ctx, "/api/"+e.app.APIVersion+path)
}

func (e *pvrExport) names(path, field string) (map[float64]string, error) {
	items, err := e.list(path)
	if err != nil {
		return nil, err
	}
	out := map[float64]string{}
	for _, item := range items {
		id, _ := item["id"].(float64)
		out[id], _ = item[field].(string)
	}
	return out, nil
}

func (e *pvrExport) defaults(path string) (map[string]map[string]any, error) {
	schemas, err := e.list(path)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]any{}
	for _, schema := range schemas {
		impl, _ := schema["implementation"].(string)
		values := map[string]any{}
		for _, f := range fieldList(schema) {
			name, _ := f["name"].(string)
			values[name] = f["value"]
		}
		out[impl] = values
	}
	return out, nil
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
	values := map[string]any{}
	for _, f := range fieldList(item) {
		key, _ := f["name"].(string)
		values[key] = f["value"]
	}
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
	dc["fields"] = e.fields(item, defaults, skip, "")
	dc["tagLabels"] = e.tagLabels(item)
	return dc
}

func (e *pvrExport) indexer(item map[string]any, defaults map[string]map[string]any) map[string]any {
	idx := pick(item, "name", "protocol", "implementation", "priority", "enableRss", "enableAutomaticSearch", "enableInteractiveSearch")
	contract(idx, item)
	protocol(idx)
	idx["fields"] = e.fields(item, defaults, nil, "indexer")
	idx["tagLabels"] = e.tagLabels(item)
	return idx
}

var notificationEvents = func() []string {
	var events []string
	t := reflect.TypeFor[commonv1alpha1.Notification]()
	for field := range t.Fields() {
		if field.Type == reflect.TypeFor[*bool]() {
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			events = append(events, name)
		}
	}
	return events
}()

func (e *pvrExport) notification(item map[string]any, defaults map[string]map[string]any) map[string]any {
	n := pick(item, "name", "implementation")
	for _, event := range notificationEvents {
		v, ok := item[event]
		if !ok {
			continue
		}
		if supported, known := item["supports"+strings.ToUpper(event[:1])+event[1:]].(bool); known && !supported {
			continue
		}
		n[event] = v
	}
	contract(n, item)
	n["fields"] = e.fields(item, defaults, nil, "notification")
	n["tagLabels"] = e.tagLabels(item)
	return n
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
	il["fields"] = e.fields(item, defaults, nil, "import-list")
	il["tagLabels"] = e.tagLabels(item)
	return il
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

func protocol(out map[string]any) {
	if p, ok := out["protocol"].(string); ok {
		out["protocol"] = strings.ToLower(strings.TrimSuffix(p, "DownloadProtocol"))
	}
}

func contract(out, item map[string]any) {
	impl, _ := item["implementation"].(string)
	if contract, _ := item["configContract"].(string); contract != "" && contract != impl+"Settings" {
		out["configContract"] = contract
	}
}

// fields keeps provider fields that differ from the implementation's defaults.
// Download client fields cannot read from a Secret, so with no secretKind a
// masked field is dropped.
func (e *pvrExport) fields(item map[string]any, defaults map[string]map[string]any, skip map[string]bool, secretKind string) []any {
	impl, _ := item["implementation"].(string)
	name, _ := item["name"].(string)
	var out []any
	for _, f := range fieldList(item) {
		key, _ := f["name"].(string)
		value := f["value"]
		switch {
		case skip[key]:
		case reconciler.IsMasked(value):
			if secretKind != "" {
				out = append(out, map[string]any{"name": key, "valueFrom": e.creds.ref("", secretKind, name, key)})
			}
		case reflect.DeepEqual(value, defaults[impl][key]):
		default:
			out = append(out, map[string]any{"name": key, "value": value})
		}
	}
	return out
}

func (e *pvrExport) tagLabels(item map[string]any) []any {
	var labels []any
	ids, _ := item["tags"].([]any)
	for _, id := range ids {
		if label, ok := e.tags[num(id)]; ok {
			labels = append(labels, label)
		}
	}
	return labels
}

func pick(item map[string]any, keys ...string) map[string]any {
	out := map[string]any{}
	for _, k := range keys {
		if v, ok := item[k]; ok {
			out[k] = v
		}
	}
	return out
}

func list(v any) []map[string]any {
	raw, _ := v.([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func fieldList(item map[string]any) []map[string]any {
	return list(item["fields"])
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}
