package prowlarr

import (
	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	servarrv1alpha1 "github.com/kyleseneker/media-operator/api/servarr/v1alpha1"
	servarrclient "github.com/kyleseneker/media-operator/internal/client/servarr"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/metrics"
)

// appLabel is the constant metrics label used for Prowlarr.
const appLabel = "prowlarr"

// ProwlarrDefinition returns the AppDefinition for Prowlarr.
func ProwlarrDefinition() engine.AppDefinition {
	return engine.AppDefinition{
		HealthPath: "/api/v1/system/status",
		Resources: []engine.ResourceEndpoint{
			// Tags first — other resources reference tag IDs.
			{Name: "tags", Path: "/api/v1/tag", MatchField: "label", Policy: engine.CreateOrUpdate},
			{Name: "applications", Path: "/api/v1/applications", MatchField: "name", Policy: engine.CreateOrUpdate, Prunable: true},
			{Name: "indexers", Path: "/api/v1/indexer", MatchField: "name", Policy: engine.CreateOrUpdate, Prunable: true},
			{Name: "proxies", Path: "/api/v1/indexerproxy", MatchField: "name", Policy: engine.CreateOrUpdate, Prunable: true},
			{Name: "downloadClients", Path: "/api/v1/downloadclient", MatchField: "name", Policy: engine.CreateOrUpdate, Prunable: true},
			{Name: "notifications", Path: "/api/v1/notification", MatchField: "name", Policy: engine.CreateOrUpdate, Prunable: true},
		},
	}
}

// ProwlarrOptions holds all the inputs needed to reconcile Prowlarr.
type ProwlarrOptions struct {
	Tags            []commonv1alpha1.Tag
	Applications    []map[string]interface{}
	Indexers        []servarrv1alpha1.ProwlarrIndexer
	Proxies         []servarrv1alpha1.ProwlarrProxy
	DownloadClients []servarrv1alpha1.ProwlarrDownloadClient
	Notifications   []commonv1alpha1.Notification
}

// ProwlarrResources builds the resources map from the options.
func ProwlarrResources(opts ProwlarrOptions) map[string][]map[string]interface{} {
	resources := make(map[string][]map[string]interface{})

	if len(opts.Tags) > 0 {
		var tags []map[string]interface{}
		for _, t := range opts.Tags {
			tags = append(tags, map[string]interface{}{"label": t.Label})
		}
		resources["tags"] = tags
	}

	// Applications are pre-built by the controller (require secret resolution).
	if len(opts.Applications) > 0 {
		resources["applications"] = opts.Applications
	}

	if len(opts.Indexers) > 0 {
		var idxs []map[string]interface{}
		for _, idx := range opts.Indexers {
			idxs = append(idxs, BuildProwlarrIndexerPayload(idx))
		}
		resources["indexers"] = idxs
	}

	if len(opts.Proxies) > 0 {
		var proxies []map[string]interface{}
		for _, p := range opts.Proxies {
			proxies = append(proxies, BuildProwlarrProxyPayload(p))
		}
		resources["proxies"] = proxies
	}

	if len(opts.DownloadClients) > 0 {
		var dcs []map[string]interface{}
		for _, dc := range opts.DownloadClients {
			dcs = append(dcs, BuildProwlarrDownloadClientPayload(dc))
		}
		resources["downloadClients"] = dcs
	}

	if len(opts.Notifications) > 0 {
		var notifs []map[string]interface{}
		for _, n := range opts.Notifications {
			notifs = append(notifs, servarrclient.BuildNotificationPayload(n))
		}
		resources["notifications"] = notifs
	}

	// Record managed resource counts for observability.
	for resourceType, items := range resources {
		metrics.ManagedResources.WithLabelValues(appLabel, resourceType).Set(float64(len(items)))
	}

	return resources
}

// BuildProwlarrApplicationPayload builds the API payload for a Prowlarr application.
// apiKey is the resolved secret value.
func BuildProwlarrApplicationPayload(app servarrv1alpha1.ProwlarrApplication, apiKey string) map[string]interface{} {
	syncCats := make([]interface{}, len(app.SyncCategories))
	for i, c := range app.SyncCategories {
		syncCats[i] = c
	}
	tags := make([]interface{}, len(app.Tags))
	for i, t := range app.Tags {
		tags[i] = t
	}
	return map[string]interface{}{
		"name": app.Name, "syncLevel": app.SyncLevel,
		"implementation": app.Implementation, "configContract": app.ConfigContract,
		"fields": []map[string]interface{}{
			{"name": "prowlarrUrl", "value": app.ProwlarrUrl},
			{"name": "baseUrl", "value": app.BaseUrl},
			{"name": "apiKey", "value": apiKey},
			{"name": "syncCategories", "value": syncCats},
		},
		"tags": tags,
	}
}

// BuildProwlarrIndexerPayload builds the API payload for a Prowlarr indexer.
func BuildProwlarrIndexerPayload(idx servarrv1alpha1.ProwlarrIndexer) map[string]interface{} {
	enable := idx.Enable == nil || *idx.Enable
	fields := make([]map[string]interface{}, 0, len(idx.Fields))
	for _, f := range idx.Fields {
		val := ""
		if f.Value != nil {
			val = *f.Value
		}
		fields = append(fields, map[string]interface{}{"name": f.Name, "value": val})
	}
	desired := map[string]interface{}{
		"name": idx.Name, "enable": enable,
		"implementation": idx.Implementation, "configContract": idx.ConfigContract,
		"fields": fields, "tags": intSliceToInterface(idx.Tags),
	}
	if idx.AppProfileId != nil {
		desired["appProfileId"] = *idx.AppProfileId
	}
	if idx.Priority != nil {
		desired["priority"] = *idx.Priority
	}
	return desired
}

// BuildProwlarrProxyPayload builds the API payload for a Prowlarr proxy.
func BuildProwlarrProxyPayload(proxy servarrv1alpha1.ProwlarrProxy) map[string]interface{} {
	fields := []map[string]interface{}{{"name": "host", "value": proxy.Host}}
	if proxy.RequestTimeout != nil {
		fields = append(fields, map[string]interface{}{"name": "requestTimeout", "value": *proxy.RequestTimeout})
	}
	return map[string]interface{}{
		"name": proxy.Name, "implementation": proxy.Implementation,
		"configContract": proxy.ConfigContract, "fields": fields,
		"tags": intSliceToInterface(proxy.Tags),
	}
}

// BuildProwlarrDownloadClientPayload builds the API payload for a Prowlarr download client.
func BuildProwlarrDownloadClientPayload(dc servarrv1alpha1.ProwlarrDownloadClient) map[string]interface{} {
	enable := dc.Enable == nil || *dc.Enable

	configContract := dc.Implementation + "Settings"
	if dc.ConfigContract != "" {
		configContract = dc.ConfigContract
	}

	fields := make([]map[string]interface{}, 0, len(dc.Fields))
	for _, f := range dc.Fields {
		val := ""
		if f.Value != nil {
			val = *f.Value
		}
		fields = append(fields, map[string]interface{}{"name": f.Name, "value": val})
	}

	categories := make([]map[string]interface{}, 0, len(dc.Categories))
	for _, cat := range dc.Categories {
		cats := make([]interface{}, len(cat.Categories))
		for i, c := range cat.Categories {
			cats[i] = c
		}
		categories = append(categories, map[string]interface{}{
			"clientCategory": cat.ClientCategory,
			"categories":     cats,
		})
	}

	desired := map[string]interface{}{
		"name":           dc.Name,
		"enable":         enable,
		"protocol":       dc.Protocol,
		"implementation": dc.Implementation,
		"configContract": configContract,
		"fields":         fields,
		"categories":     categories,
		"tags":           intSliceToInterface(dc.Tags),
	}

	if dc.Priority != nil {
		desired["priority"] = *dc.Priority
	}

	return desired
}

// intSliceToInterface converts an int slice to an interface slice for JSON serialization.
func intSliceToInterface(s []int) []interface{} {
	r := make([]interface{}, len(s))
	for i, v := range s {
		r[i] = v
	}
	return r
}
