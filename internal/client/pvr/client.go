package pvr

import (
	"context"
	"encoding/json"
	"fmt"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/metrics"
)

// Definition returns the AppDefinition for any Servarr-family app.
// apiVersion is "v3" for Sonarr/Radarr, "v1" for Lidarr/Readarr.
func Definition(apiVersion string) engine.AppDefinition {
	prefix := fmt.Sprintf("/api/%s", apiVersion)
	return engine.AppDefinition{
		HealthPath: prefix + "/system/status",
		Settings: []engine.SettingEndpoint{
			{Name: "mediaManagement", Path: prefix + "/config/mediamanagement"},
			{Name: "naming", Path: prefix + "/config/naming"},
			{Name: "indexerConfig", Path: prefix + "/config/indexer"},
			{Name: "downloadClientConfig", Path: prefix + "/config/downloadclient"},
			{Name: "ui", Path: prefix + "/config/ui"},
		},
		Resources: []engine.ResourceEndpoint{
			// Tags first — other resources reference tag IDs. Not prunable (may be shared).
			{Name: "tags", Path: prefix + "/tag", MatchField: "label", Policy: engine.CreateOrUpdate},
			// Root folders not prunable (CreateOnly, destructive to remove).
			{Name: "rootFolders", Path: prefix + "/rootfolder", MatchField: "path", Policy: engine.CreateOnly},
			{Name: "downloadClients", Path: prefix + "/downloadclient", MatchField: "name", Policy: engine.CreateOrUpdate, Prunable: true},
			{Name: "indexers", Path: prefix + "/indexer", MatchField: "name", Policy: engine.CreateOrUpdate, Prunable: true},
			// customFormats before qualityProfiles so IDs can be resolved for formatItems.
			{Name: "customFormats", Path: prefix + "/customformat", MatchField: "name", Policy: engine.CreateOrUpdate, Prunable: true},
			{Name: "qualityProfiles", Path: prefix + "/qualityprofile", MatchField: "name", Policy: engine.CreateOrUpdate, Prunable: true},
			{Name: "notifications", Path: prefix + "/notification", MatchField: "name", Policy: engine.CreateOrUpdate, Prunable: true},
			// importLists last — may reference quality profiles by name.
			{Name: "importLists", Path: prefix + "/importlist", MatchField: "name", Policy: engine.CreateOrUpdate, Prunable: true},
		},
	}
}

// Sections builds the sections map from a spec that follows the Servarr pattern.
// The spec must have the fields: MediaManagement, Naming, IndexerConfig, DownloadClientConfig, UI.
func Sections(spec any) (map[string]any, error) {
	data, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("marshaling spec to JSON: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("unmarshaling spec from JSON: %w", err)
	}

	sections := make(map[string]any)
	fieldMap := map[string]string{
		"mediaManagement":      "mediaManagement",
		"naming":               "naming",
		"indexerConfig":        "indexerConfig",
		"downloadClientConfig": "downloadClientConfig",
		"ui":                   "ui",
	}
	for jsonKey, sectionName := range fieldMap {
		if v, ok := m[jsonKey]; ok && v != nil {
			sections[sectionName] = v
		}
	}
	return sections, nil
}

// DownloadClientResolvedSecrets holds resolved secret values for a download client.
type DownloadClientResolvedSecrets struct {
	Username string
	Password string
	APIKey   string
}

// Options holds all the inputs needed to reconcile a Servarr-family app.
type Options struct {
	RootFolders     []commonv1alpha1.RootFolder
	DownloadClients []commonv1alpha1.DownloadClient
	DCSecrets       map[string]DownloadClientResolvedSecrets
	CategoryField   string
	QualityProfiles []commonv1alpha1.QualityProfile
	CustomFormats   []commonv1alpha1.CustomFormat
	Tags            []commonv1alpha1.Tag
	Indexers        []commonv1alpha1.Indexer
	Notifications   []commonv1alpha1.Notification
	ImportLists     []commonv1alpha1.ImportList
}

// Resources builds the resources map from the options.
func Resources(ctx context.Context, client *engine.HTTPClient, apiVersion string, opts Options) (map[string][]map[string]any, error) {
	resources := make(map[string][]map[string]any)

	if len(opts.Tags) > 0 {
		tags := make([]map[string]any, 0, len(opts.Tags))
		for _, t := range opts.Tags {
			tags = append(tags, map[string]any{"label": t.Label})
		}
		resources["tags"] = tags
	}

	if len(opts.RootFolders) > 0 {
		rfs := make([]map[string]any, 0, len(opts.RootFolders))
		for _, rf := range opts.RootFolders {
			rfs = append(rfs, map[string]any{"path": rf.Path})
		}
		resources["rootFolders"] = rfs
	}

	if len(opts.DownloadClients) > 0 {
		dcs := make([]map[string]any, 0, len(opts.DownloadClients))
		for _, dc := range opts.DownloadClients {
			secrets := opts.DCSecrets[dc.Name]
			dcs = append(dcs, BuildDownloadClientPayload(dc, secrets, opts.CategoryField))
		}
		resources["downloadClients"] = dcs
	}

	if len(opts.Indexers) > 0 {
		idxs := make([]map[string]any, 0, len(opts.Indexers))
		for _, idx := range opts.Indexers {
			idxs = append(idxs, BuildIndexerPayload(idx))
		}
		resources["indexers"] = idxs
	}

	if len(opts.CustomFormats) > 0 {
		cfs := make([]map[string]any, 0, len(opts.CustomFormats))
		for _, cf := range opts.CustomFormats {
			cfs = append(cfs, BuildCustomFormatPayload(cf))
		}
		resources["customFormats"] = cfs
	}

	if len(opts.QualityProfiles) > 0 {
		// Resolve custom format name → ID mapping for formatItems references.
		cfIDs, err := resolveCustomFormatIDs(ctx, client, apiVersion)
		if err != nil {
			return nil, err
		}
		var qps []map[string]any
		for _, qp := range opts.QualityProfiles {
			qps = append(qps, BuildQualityProfilePayload(qp, cfIDs))
		}
		resources["qualityProfiles"] = qps
	}

	if len(opts.Notifications) > 0 {
		var notifs []map[string]any
		for _, n := range opts.Notifications {
			notifs = append(notifs, BuildNotificationPayload(n))
		}
		resources["notifications"] = notifs
	}

	if len(opts.ImportLists) > 0 {
		// Resolve quality profile name → ID mapping for qualityProfileName references.
		qpIDs, err := resolveQualityProfileIDs(ctx, client, apiVersion)
		if err != nil {
			return nil, err
		}
		var ils []map[string]any
		for _, il := range opts.ImportLists {
			ils = append(ils, BuildImportListPayload(il, qpIDs))
		}
		resources["importLists"] = ils
	}

	// Record managed resource counts for observability.
	app := client.AppLabel()
	for resourceType, items := range resources {
		metrics.ManagedResources.WithLabelValues(app, resourceType).Set(float64(len(items)))
	}

	return resources, nil
}

// BuildDownloadClientPayload builds the API payload for a download client.
func BuildDownloadClientPayload(dc commonv1alpha1.DownloadClient, secrets DownloadClientResolvedSecrets, categoryField string) map[string]any {
	enable := dc.Enable == nil || *dc.Enable
	removeCompleted := dc.RemoveCompletedDownloads == nil || *dc.RemoveCompletedDownloads
	removeFailed := dc.RemoveFailedDownloads == nil || *dc.RemoveFailedDownloads

	priority := 1
	if dc.Priority != nil {
		priority = *dc.Priority
	}

	useSsl := false
	if dc.UseSsl != nil {
		useSsl = *dc.UseSsl
	}

	configContract := dc.Implementation + "Settings"
	if dc.ConfigContract != "" {
		configContract = dc.ConfigContract
	}

	// Build the fields array from first-class fields
	fields := []map[string]any{
		{"name": "host", "value": dc.Host},
		{"name": "port", "value": dc.Port},
		{"name": "useSsl", "value": useSsl},
		{"name": categoryField, "value": dc.Category},
	}

	if dc.UrlBase != "" {
		fields = append(fields, map[string]any{"name": "urlBase", "value": dc.UrlBase})
	}

	// Add auth fields from resolved secrets
	if secrets.Username != "" {
		fields = append(fields, map[string]any{"name": "username", "value": secrets.Username})
	}
	if secrets.Password != "" {
		fields = append(fields, map[string]any{"name": "password", "value": secrets.Password})
	}
	if secrets.APIKey != "" {
		fields = append(fields, map[string]any{"name": "apiKey", "value": secrets.APIKey})
	}

	// Append any additional implementation-specific fields.
	// These override or supplement the first-class fields above.
	knownFields := map[string]bool{
		"host": true, "port": true, "useSsl": true, "urlBase": true,
		"username": true, "password": true, "apiKey": true,
		categoryField: true,
	}
	for _, f := range dc.Fields {
		if knownFields[f.Name] {
			// Skip fields already set by first-class fields to avoid duplicates.
			// Users should use the dedicated CRD fields instead.
			continue
		}
		var val any
		if f.Value != nil {
			val = f.Value.ToInterface()
		}
		fields = append(fields, map[string]any{"name": f.Name, "value": val})
	}

	tags := make([]any, len(dc.Tags))
	for i, t := range dc.Tags {
		tags[i] = t
	}

	return map[string]any{
		"name":                     dc.Name,
		"enable":                   enable,
		"protocol":                 dc.Protocol,
		"priority":                 priority,
		"removeCompletedDownloads": removeCompleted,
		"removeFailedDownloads":    removeFailed,
		"implementation":           dc.Implementation,
		"configContract":           configContract,
		"fields":                   fields,
		"tags":                     tags,
	}
}

// BuildIndexerPayload builds the API payload for an indexer.
func BuildIndexerPayload(idx commonv1alpha1.Indexer) map[string]any {
	enable := idx.Enable == nil || *idx.Enable
	enableRss := idx.EnableRss == nil || *idx.EnableRss
	enableAutoSearch := idx.EnableAutomaticSearch == nil || *idx.EnableAutomaticSearch
	enableInteractive := idx.EnableInteractiveSearch == nil || *idx.EnableInteractiveSearch

	configContract := idx.Implementation + "Settings"
	if idx.ConfigContract != "" {
		configContract = idx.ConfigContract
	}

	fields := BuildFieldsPayload(idx.Fields)
	tags := BuildTagsPayload(idx.Tags)

	payload := map[string]any{
		"name":                    idx.Name,
		"enable":                  enable,
		"protocol":                idx.Protocol,
		"implementation":          idx.Implementation,
		"configContract":          configContract,
		"enableRss":               enableRss,
		"enableAutomaticSearch":   enableAutoSearch,
		"enableInteractiveSearch": enableInteractive,
		"fields":                  fields,
		"tags":                    tags,
	}

	if idx.Priority != nil {
		payload["priority"] = *idx.Priority
	}

	return payload
}

// BuildNotificationPayload builds the API payload for a notification.
func BuildNotificationPayload(n commonv1alpha1.Notification) map[string]any {
	configContract := n.Implementation + "Settings"
	if n.ConfigContract != "" {
		configContract = n.ConfigContract
	}

	fields := BuildFieldsPayload(n.Fields)
	tags := BuildTagsPayload(n.Tags)

	payload := map[string]any{
		"name":           n.Name,
		"implementation": n.Implementation,
		"configContract": configContract,
		"fields":         fields,
		"tags":           tags,
	}

	// Add trigger booleans — only set non-nil values so the merge preserves API defaults.
	SetOptionalBool(payload, "onGrab", n.OnGrab)
	SetOptionalBool(payload, "onDownload", n.OnDownload)
	SetOptionalBool(payload, "onUpgrade", n.OnUpgrade)
	SetOptionalBool(payload, "onRename", n.OnRename)
	SetOptionalBool(payload, "onHealthIssue", n.OnHealthIssue)
	SetOptionalBool(payload, "onHealthRestored", n.OnHealthRestored)
	SetOptionalBool(payload, "onApplicationUpdate", n.OnApplicationUpdate)
	SetOptionalBool(payload, "onManualInteractionRequired", n.OnManualInteractionRequired)
	SetOptionalBool(payload, "includeHealthWarnings", n.IncludeHealthWarnings)
	// Sonarr-specific
	SetOptionalBool(payload, "onSeriesAdd", n.OnSeriesAdd)
	SetOptionalBool(payload, "onSeriesDelete", n.OnSeriesDelete)
	SetOptionalBool(payload, "onEpisodeFileDelete", n.OnEpisodeFileDelete)
	SetOptionalBool(payload, "onEpisodeFileDeleteForUpgrade", n.OnEpisodeFileDeleteForUpgrade)
	// Radarr-specific
	SetOptionalBool(payload, "onMovieAdded", n.OnMovieAdded)
	SetOptionalBool(payload, "onMovieDelete", n.OnMovieDelete)
	SetOptionalBool(payload, "onMovieFileDelete", n.OnMovieFileDelete)
	SetOptionalBool(payload, "onMovieFileDeleteForUpgrade", n.OnMovieFileDeleteForUpgrade)

	return payload
}

// BuildImportListPayload builds the API payload for an import list.
// qpIDs maps quality profile names to their IDs.
func BuildImportListPayload(il commonv1alpha1.ImportList, qpIDs map[string]int) map[string]any {
	enable := il.Enable == nil || *il.Enable

	configContract := il.Implementation + "Settings"
	if il.ConfigContract != "" {
		configContract = il.ConfigContract
	}

	fields := BuildFieldsPayload(il.Fields)
	tags := BuildTagsPayload(il.Tags)

	payload := map[string]any{
		"name":           il.Name,
		"enable":         enable,
		"implementation": il.Implementation,
		"configContract": configContract,
		"fields":         fields,
		"tags":           tags,
	}

	SetOptionalBool(payload, "enableAutomaticAdd", il.EnableAutomaticAdd)
	SetOptionalBool(payload, "shouldMonitor", il.ShouldMonitor)
	SetOptionalBool(payload, "seasonFolder", il.SeasonFolder)
	SetOptionalBool(payload, "searchOnAdd", il.SearchOnAdd)

	if il.Monitor != "" {
		payload["monitor"] = il.Monitor
	}
	if il.RootFolderPath != "" {
		payload["rootFolderPath"] = il.RootFolderPath
	}
	if il.SeriesType != "" {
		payload["seriesType"] = il.SeriesType
	}
	if il.MinimumAvailability != "" {
		payload["minimumAvailability"] = il.MinimumAvailability
	}
	if il.ListOrder != nil {
		payload["listOrder"] = *il.ListOrder
	}

	// Resolve quality profile name → ID
	if il.QualityProfileName != "" {
		if id, ok := qpIDs[il.QualityProfileName]; ok {
			payload["qualityProfileId"] = id
		}
	}

	return payload
}

// BuildFieldsPayload converts ConfigField slice to API fields format.
func BuildFieldsPayload(dcFields []commonv1alpha1.ConfigField) []map[string]any {
	fields := make([]map[string]any, len(dcFields))
	for i, f := range dcFields {
		var val any
		if f.Value != nil {
			val = f.Value.ToInterface()
		}
		fields[i] = map[string]any{
			"name":  f.Name,
			"value": val,
		}
	}
	return fields
}

// BuildTagsPayload converts an int slice to an interface slice for JSON.
func BuildTagsPayload(tagIDs []int) []any {
	tags := make([]any, len(tagIDs))
	for i, t := range tagIDs {
		tags[i] = t
	}
	return tags
}

// SetOptionalBool sets a key in the payload only if the pointer is non-nil.
func SetOptionalBool(payload map[string]any, key string, val *bool) {
	if val != nil {
		payload[key] = *val
	}
}

// resolveQualityProfileIDs fetches all quality profiles from the API and returns a name → ID map.
func resolveQualityProfileIDs(ctx context.Context, client *engine.HTTPClient, apiVersion string) (map[string]int, error) {
	path := fmt.Sprintf("/api/%s/qualityprofile", apiVersion)
	existing, err := client.GetJSONList(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("fetching quality profiles: %w", err)
	}

	ids := make(map[string]int, len(existing))
	for _, qp := range existing {
		name, _ := qp["name"].(string)
		id, _ := qp["id"].(float64)
		if name != "" {
			ids[name] = int(id)
		}
	}
	return ids, nil
}

// BuildCustomFormatPayload builds the API payload for a custom format.
func BuildCustomFormatPayload(cf commonv1alpha1.CustomFormat) map[string]any {
	includeWhenRenaming := false
	if cf.IncludeCustomFormatWhenRenaming != nil {
		includeWhenRenaming = *cf.IncludeCustomFormatWhenRenaming
	}

	specs := make([]map[string]any, len(cf.Specifications))
	for i, s := range cf.Specifications {
		negate := false
		if s.Negate != nil {
			negate = *s.Negate
		}
		required := false
		if s.Required != nil {
			required = *s.Required
		}

		fields := make([]map[string]any, len(s.Fields))
		for j, f := range s.Fields {
			var val any
			if f.Value != nil {
				val = f.Value.ToInterface()
			}
			fields[j] = map[string]any{
				"name":  f.Name,
				"value": val,
			}
		}

		specs[i] = map[string]any{
			"name":           s.Name,
			"implementation": s.Implementation,
			"negate":         negate,
			"required":       required,
			"fields":         fields,
		}
	}

	return map[string]any{
		"name":                            cf.Name,
		"includeCustomFormatWhenRenaming": includeWhenRenaming,
		"specifications":                  specs,
	}
}

// BuildQualityProfilePayload builds the API payload for a quality profile.
// cfIDs maps custom format names to their IDs for resolving formatItems references.
func BuildQualityProfilePayload(qp commonv1alpha1.QualityProfile, cfIDs map[string]int) map[string]any {
	payload := map[string]any{
		"name": qp.Name,
	}

	if qp.UpgradeAllowed != nil {
		payload["upgradeAllowed"] = *qp.UpgradeAllowed
	}
	if qp.MinFormatScore != nil {
		payload["minFormatScore"] = *qp.MinFormatScore
	}
	if qp.CutoffFormatScore != nil {
		payload["cutoffFormatScore"] = *qp.CutoffFormatScore
	}
	if qp.MinUpgradeFormatScore != nil {
		payload["minUpgradeFormatScore"] = *qp.MinUpgradeFormatScore
	}

	if len(qp.Items) > 0 {
		items := BuildQualityItems(qp.Items)
		payload["items"] = items

		// Resolve cutoff name to ID
		if qp.Cutoff != "" {
			if id, ok := FindCutoffID(qp.Items, qp.Cutoff); ok {
				payload["cutoff"] = id
			}
		}
	}

	if len(qp.FormatItems) > 0 {
		formatItems := make([]map[string]any, 0, len(qp.FormatItems))
		for _, fi := range qp.FormatItems {
			if id, ok := cfIDs[fi.Name]; ok {
				formatItems = append(formatItems, map[string]any{
					"format": id,
					"name":   fi.Name,
					"score":  fi.Score,
				})
			}
		}
		payload["formatItems"] = formatItems
	}

	return payload
}

// BuildQualityItems converts CRD quality profile items to API payload format.
func BuildQualityItems(items []commonv1alpha1.QualityProfileItem) []map[string]any {
	result := make([]map[string]any, len(items))
	for i, item := range items {
		m := map[string]any{}

		if item.Quality != nil {
			// Individual quality
			m["quality"] = map[string]any{
				"id":   item.Quality.ID,
				"name": item.Quality.Name,
			}
			m["items"] = []any{}
		} else {
			// Quality group
			m["name"] = item.Name
			m["quality"] = nil
			children := make([]map[string]any, len(item.Items))
			for j, child := range item.Items {
				childAllowed := child.Allowed == nil || *child.Allowed
				children[j] = map[string]any{
					"quality": map[string]any{
						"id":   child.Quality.ID,
						"name": child.Quality.Name,
					},
					"items":   []any{},
					"allowed": childAllowed,
				}
			}
			m["items"] = children
		}

		if item.Allowed != nil {
			m["allowed"] = *item.Allowed
		} else {
			m["allowed"] = true
		}

		result[i] = m
	}
	return result
}

// FindCutoffID resolves a cutoff name (quality name or group name) to the corresponding ID.
// For individual qualities, returns quality.id. For groups, returns the group item index + 1000
// (matching Servarr's convention for group IDs).
func FindCutoffID(items []commonv1alpha1.QualityProfileItem, cutoffName string) (int, bool) {
	for i, item := range items {
		if item.Quality != nil && item.Quality.Name == cutoffName {
			return item.Quality.ID, true
		}
		if item.Quality == nil && item.Name == cutoffName {
			// Group — use 1000+index as the group ID (Servarr convention)
			return 1000 + i, true
		}
	}
	return 0, false
}

// resolveCustomFormatIDs fetches all custom formats from the API and returns a name → ID map.
func resolveCustomFormatIDs(ctx context.Context, client *engine.HTTPClient, apiVersion string) (map[string]int, error) {
	path := fmt.Sprintf("/api/%s/customformat", apiVersion)
	existing, err := client.GetJSONList(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("fetching custom formats: %w", err)
	}

	ids := make(map[string]int, len(existing))
	for _, cf := range existing {
		name, _ := cf["name"].(string)
		id, _ := cf["id"].(float64)
		if name != "" {
			ids[name] = int(id)
		}
	}
	return ids, nil
}

// Reconcile runs the full reconciliation for any Servarr-family app.
func Reconcile(ctx context.Context, client *engine.HTTPClient, apiVersion string, spec any, opts Options, policy engine.SyncPolicy, managed map[string][]string) (engine.ReconcileResult, error) {
	def := Definition(apiVersion)
	sections, err := Sections(spec)
	if err != nil {
		return engine.ReconcileResult{}, fmt.Errorf("building sections: %w", err)
	}
	// Quality profiles reference custom formats by ID, and import lists reference
	// quality profiles by ID. Those IDs only exist once the referenced resources
	// have been written, so each dependent stage resolves after the prior apply
	// rather than all at once up front.
	fieldProblems := ValidateOptionFields(ctx, client, apiVersion, opts)

	base := opts
	base.QualityProfiles = nil
	base.ImportLists = nil
	resources, err := Resources(ctx, client, apiVersion, base)
	if err != nil {
		return engine.ReconcileResult{}, fmt.Errorf("building resources: %w", err)
	}
	result := engine.ReconcileApp(ctx, client, def, sections, resources, policy, managed)
	result.Errors = append(result.Errors, fieldProblems...)

	if len(opts.QualityProfiles) > 0 {
		stage := Options{QualityProfiles: opts.QualityProfiles}
		qpRes, qerr := Resources(ctx, client, apiVersion, stage)
		if qerr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("qualityProfiles: %v", qerr))
		} else {
			mergeResult(&result, engine.ReconcileApp(ctx, client, def, nil, qpRes, policy, managed))
		}
	}

	if len(opts.ImportLists) > 0 {
		stage := Options{ImportLists: opts.ImportLists}
		ilRes, ierr := Resources(ctx, client, apiVersion, stage)
		if ierr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("importLists: %v", ierr))
		} else {
			mergeResult(&result, engine.ReconcileApp(ctx, client, def, nil, ilRes, policy, managed))
		}
	}

	return result, nil
}

func mergeResult(dst *engine.ReconcileResult, src engine.ReconcileResult) {
	dst.Synced = append(dst.Synced, src.Synced...)
	dst.Errors = append(dst.Errors, src.Errors...)
	dst.Pruned = append(dst.Pruned, src.Pruned...)
	if dst.Managed == nil {
		dst.Managed = map[string][]string{}
	}
	for k, v := range src.Managed {
		dst.Managed[k] = append(dst.Managed[k], v...)
	}
}
