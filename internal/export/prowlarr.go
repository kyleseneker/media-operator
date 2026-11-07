package export

import (
	"reflect"
	"strconv"

	"github.com/kyleseneker/media-operator/internal/reconciler"
)

type prowlarrExport struct {
	*reader
	appProfiles map[float64]string
}

func exportProwlarr(r *reader) (map[string]any, error) {
	r.prefix = "/api/v1"
	var err error
	if r.tags, err = r.names("/tag", "label"); err != nil {
		return nil, err
	}
	e := &prowlarrExport{reader: r}
	if e.appProfiles, err = r.names("/appprofile", "name"); err != nil {
		return nil, err
	}
	spec := map[string]any{}
	return spec, r.resources(spec, []resource{
		{"tags", "/tag", "", tag},
		{"appProfiles", "/appprofile", "", func(item map[string]any, _ map[string]map[string]any) map[string]any {
			return pick(item, "name", "enableRss", "enableAutomaticSearch", "enableInteractiveSearch", "minimumSeeders")
		}},
		{"applications", "/applications", "/applications/schema", e.application},
		{"indexers", "/indexer", "", e.indexer},
		{"proxies", "/indexerproxy", "", e.proxy},
		{"downloadClients", "/downloadclient", "/downloadclient/schema", e.downloadClient},
		{"notifications", "/notification", "/notification/schema", e.notification},
	})
}

func (e *prowlarrExport) application(item map[string]any, defaults map[string]map[string]any) map[string]any {
	a := pick(item, "name", "syncLevel", "implementation", "configContract")
	name, _ := item["name"].(string)
	values := fieldValues(item)
	a["prowlarrUrl"] = values["prowlarrUrl"]
	a["baseUrl"] = values["baseUrl"]
	a["syncCategories"] = values["syncCategories"]
	key, _ := values["apiKey"].(string)
	if reconciler.IsMasked(key) {
		key = ""
	}
	a["apiKeySecretRef"] = e.creds.ref(key, "application", name, "api-key")
	impl, _ := item["implementation"].(string)
	for field, value := range values {
		switch field {
		case "prowlarrUrl", "baseUrl", "syncCategories", "apiKey":
		default:
			if !reflect.DeepEqual(value, defaults[impl][field]) {
				e.warn("application %q: ProwlarrConfig has no %s, so it stays as set in Prowlarr", name, field)
			}
		}
	}
	a["tags"] = e.tagLabels(item)
	return a
}

func (e *prowlarrExport) indexer(item map[string]any, _ map[string]map[string]any) map[string]any {
	idx := pick(item, "name", "implementation", "configContract", "enable", "priority")
	if id, ok := item["appProfileId"].(float64); ok {
		idx["appProfileName"] = e.appProfiles[id]
	}
	idx["fields"] = e.textFields(item, nil, "indexer")
	idx["tags"] = e.tagLabels(item)
	return idx
}

func (e *prowlarrExport) proxy(item map[string]any, _ map[string]map[string]any) map[string]any {
	p := pick(item, "name", "implementation", "configContract")
	name, _ := item["name"].(string)
	for field, value := range fieldValues(item) {
		switch field {
		case "host", "requestTimeout":
			p[field] = value
		default:
			if value != nil && value != "" {
				e.warn("proxy %q: ProwlarrConfig proxies have only host and requestTimeout, so %s stays as set in Prowlarr", name, field)
			}
		}
	}
	p["tags"] = e.tagLabels(item)
	return p
}

func (e *prowlarrExport) downloadClient(item map[string]any, defaults map[string]map[string]any) map[string]any {
	dc := pick(item, "name", "enable", "protocol", "implementation", "configContract", "priority", "categories")
	protocol(dc)
	dc["fields"] = e.textFields(item, defaults, "download-client")
	dc["tags"] = e.tagLabels(item)
	return dc
}

// textFields renders provider fields as the strings ProwlarrField holds;
// Prowlarr reads them back typed, and the operator compares them as text.
// Indexers pass no defaults: Prowlarr's indexer schema lists every tracker.
func (e *prowlarrExport) textFields(item map[string]any, defaults map[string]map[string]any, kind string) []any {
	impl, _ := item["implementation"].(string)
	name, _ := item["name"].(string)
	var out []any
	for _, f := range fieldList(item) {
		key, _ := f["name"].(string)
		if def, ok := defaults[impl][key]; ok && reflect.DeepEqual(f["value"], def) {
			continue
		}
		var text string
		switch v := f["value"].(type) {
		case nil:
			continue
		case string:
			if reconciler.IsMasked(v) {
				out = append(out, map[string]any{"name": key, "valueFrom": e.creds.ref("", kind, name, key)})
				continue
			}
			if v == "" {
				continue
			}
			text = v
		case float64:
			text = strconv.FormatFloat(v, 'f', -1, 64)
		case bool:
			text = strconv.FormatBool(v)
		default:
			e.warn("%s %q: ProwlarrConfig fields hold text, so %s stays as set in Prowlarr", kind, name, key)
			continue
		}
		out = append(out, map[string]any{"name": key, "value": text})
	}
	return out
}
