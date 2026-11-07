package pvr

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
)

func fieldValue(raw string) *commonv1alpha1.FieldValue {
	fv := &commonv1alpha1.FieldValue{}
	_ = fv.UnmarshalJSON([]byte(raw))
	return fv
}

func TestDefinition(t *testing.T) {
	tests := []struct {
		name       string
		apiVersion string
		wantPrefix string
	}{
		{"v3 for Sonarr/Radarr", "v3", "/api/v3"},
		{"v1 for Lidarr/Readarr", "v1", "/api/v1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := Definition(tt.apiVersion)
			assert.Equal(t, tt.wantPrefix+"/system/status", def.HealthPath)
			assert.Len(t, def.Settings, 5)
			assert.True(t, len(def.Resources) >= 8)

			for _, s := range def.Settings {
				assert.Contains(t, s.Path, tt.wantPrefix)
			}
			for _, r := range def.Resources {
				assert.Contains(t, r.Path, tt.wantPrefix)
			}
		})
	}
}

func TestDefinition_ResourceOrder(t *testing.T) {
	def := Definition("v3")
	names := make([]string, len(def.Resources))
	for i, r := range def.Resources {
		names[i] = r.Name
	}
	// Tags must come first, importLists must come last
	assert.Equal(t, "tags", names[0])
	assert.Equal(t, "importLists", names[len(names)-1])
	// customFormats before qualityProfiles
	cfIdx, qpIdx := -1, -1
	for i, n := range names {
		if n == "customFormats" {
			cfIdx = i
		}
		if n == "qualityProfiles" {
			qpIdx = i
		}
	}
	assert.Greater(t, qpIdx, cfIdx)
}

func TestSections(t *testing.T) {
	type testSpec struct {
		MediaManagement      *map[string]any `json:"mediaManagement,omitempty"`
		Naming               *map[string]any `json:"naming,omitempty"`
		IndexerConfig        *map[string]any `json:"indexerConfig,omitempty"`
		DownloadClientConfig *map[string]any `json:"downloadClientConfig,omitempty"`
		UI                   *map[string]any `json:"ui,omitempty"`
	}

	tests := []struct {
		name     string
		spec     any
		wantKeys []string
	}{
		{
			name: "all sections",
			spec: testSpec{
				MediaManagement: &map[string]any{"a": 1},
				Naming:          &map[string]any{"b": 2},
			},
			wantKeys: []string{"mediaManagement", "naming"},
		},
		{
			name:     "empty spec",
			spec:     testSpec{},
			wantKeys: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sections, err := Sections(tt.spec)
			require.NoError(t, err)
			assert.Len(t, sections, len(tt.wantKeys))
			for _, k := range tt.wantKeys {
				assert.Contains(t, sections, k)
			}
		})
	}
}

func TestBuildDownloadClientPayload(t *testing.T) {
	tests := []struct {
		name          string
		dc            commonv1alpha1.DownloadClient
		secrets       DownloadClientResolvedSecrets
		categoryField string
		check         func(t *testing.T, p map[string]any)
	}{
		{
			name: "defaults",
			dc: commonv1alpha1.DownloadClient{
				Name: "qBit", Protocol: "torrent", Implementation: "QBittorrent",
				Host: "qbit", Port: 8080, Category: "tv",
			},
			categoryField: "tvCategory",
			check: func(t *testing.T, p map[string]any) {
				assert.Equal(t, "qBit", p["name"])
				assert.Equal(t, true, p["enable"])
				assert.Equal(t, 1, p["priority"])
				assert.Equal(t, true, p["removeCompletedDownloads"])
				assert.Equal(t, true, p["removeFailedDownloads"])
				assert.Equal(t, "QBittorrentSettings", p["configContract"])

				fields := p["fields"].([]map[string]any)
				hostField := fields[0]
				assert.Equal(t, "host", hostField["name"])
				assert.Equal(t, "qbit", hostField["value"])
			},
		},
		{
			name: "all options set",
			dc: commonv1alpha1.DownloadClient{
				Name: "qBit", Protocol: "torrent", Implementation: "QBittorrent",
				Host: "qbit", Port: 8080, Category: "movies",
				Enable: new(false), Priority: new(5),
				RemoveCompletedDownloads: new(false),
				RemoveFailedDownloads:    new(false),
				UseSsl:                   new(true),
				UrlBase:                  "/qbt",
				ConfigContract:           "CustomContract",
				Tags:                     []int{1, 2},
			},
			categoryField: "movieCategory",
			check: func(t *testing.T, p map[string]any) {
				assert.Equal(t, false, p["enable"])
				assert.Equal(t, 5, p["priority"])
				assert.Equal(t, false, p["removeCompletedDownloads"])
				assert.Equal(t, false, p["removeFailedDownloads"])
				assert.Equal(t, "CustomContract", p["configContract"])
				assert.Len(t, p["tags"], 2)

				fields := p["fields"].([]map[string]any)
				found := false
				for _, f := range fields {
					if f["name"] == "useSsl" {
						assert.Equal(t, true, f["value"])
						found = true
					}
					if f["name"] == "urlBase" {
						assert.Equal(t, "/qbt", f["value"])
					}
				}
				assert.True(t, found, "useSsl field not found")
			},
		},
		{
			name: "with secrets",
			dc: commonv1alpha1.DownloadClient{
				Name: "qBit", Protocol: "torrent", Implementation: "QBittorrent",
				Host: "qbit", Port: 8080, Category: "tv",
			},
			secrets:       DownloadClientResolvedSecrets{Username: "admin", Password: "pass123", APIKey: "key123"},
			categoryField: "tvCategory",
			check: func(t *testing.T, p map[string]any) {
				fields := p["fields"].([]map[string]any)
				names := make(map[string]any)
				for _, f := range fields {
					names[f["name"].(string)] = f["value"]
				}
				assert.Equal(t, "admin", names["username"])
				assert.Equal(t, "pass123", names["password"])
				assert.Equal(t, "key123", names["apiKey"])
			},
		},
		{
			name: "extra fields skip known fields",
			dc: commonv1alpha1.DownloadClient{
				Name: "qBit", Protocol: "torrent", Implementation: "QBittorrent",
				Host: "qbit", Port: 8080, Category: "tv",
				Fields: []commonv1alpha1.ConfigField{
					{Name: "host", Value: fieldValue(`"should-be-ignored"`)},
					{Name: "initialState", Value: fieldValue(`0`)},
				},
			},
			categoryField: "tvCategory",
			check: func(t *testing.T, p map[string]any) {
				fields := p["fields"].([]map[string]any)
				hostCount := 0
				foundInitial := false
				for _, f := range fields {
					if f["name"] == "host" {
						hostCount++
						assert.Equal(t, "qbit", f["value"], "host should use first-class field, not extra field")
					}
					if f["name"] == "initialState" {
						foundInitial = true
						assert.Equal(t, float64(0), f["value"])
					}
				}
				assert.Equal(t, 1, hostCount, "host should appear exactly once")
				assert.True(t, foundInitial, "initialState field should be appended")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := BuildDownloadClientPayload(tt.dc, tt.secrets, tt.categoryField)
			tt.check(t, p)
		})
	}
}

func TestBuildIndexerPayload(t *testing.T) {
	tests := []struct {
		name  string
		idx   commonv1alpha1.Indexer
		check func(t *testing.T, p map[string]any)
	}{
		{
			name: "defaults",
			idx: commonv1alpha1.Indexer{
				Name: "NZBgeek", Protocol: "usenet", Implementation: "Newznab",
			},
			check: func(t *testing.T, p map[string]any) {
				assert.NotContains(t, p, "enable", "indexers have no enable property; sending one is permanent drift")
				assert.Equal(t, true, p["enableRss"])
				assert.Equal(t, true, p["enableAutomaticSearch"])
				assert.Equal(t, true, p["enableInteractiveSearch"])
				assert.Equal(t, "NewznabSettings", p["configContract"])
				_, hasPriority := p["priority"]
				assert.False(t, hasPriority)
			},
		},
		{
			name: "all options set",
			idx: commonv1alpha1.Indexer{
				Name: "NZBgeek", Protocol: "usenet", Implementation: "Newznab",
				Enable: new(false), EnableRss: new(false),
				EnableAutomaticSearch: new(false), EnableInteractiveSearch: new(false),
				ConfigContract: "CustomContract", Priority: new(10),
				Tags: []int{1}, Fields: []commonv1alpha1.ConfigField{
					{Name: "baseUrl", Value: fieldValue(`"http://nzbgeek.info"`)},
				},
			},
			check: func(t *testing.T, p map[string]any) {
				assert.Equal(t, false, p["enableRss"])
				assert.Equal(t, "CustomContract", p["configContract"])
				assert.Equal(t, 10, p["priority"])
				tags := p["tags"].([]any)
				assert.Len(t, tags, 1)
			},
		},
		{
			name: "enable false turns off every use",
			idx: commonv1alpha1.Indexer{
				Name: "Off", Protocol: "torrent", Implementation: "Torznab",
				Enable: new(false), EnableRss: new(true), EnableAutomaticSearch: new(true), EnableInteractiveSearch: new(true),
			},
			check: func(t *testing.T, p map[string]any) {
				assert.Equal(t, false, p["enableRss"])
				assert.Equal(t, false, p["enableAutomaticSearch"])
				assert.Equal(t, false, p["enableInteractiveSearch"])
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := BuildIndexerPayload(tt.idx)
			tt.check(t, p)
		})
	}
}

func TestBuildNotificationPayload(t *testing.T) {
	tests := []struct {
		name  string
		n     commonv1alpha1.Notification
		check func(t *testing.T, p map[string]any)
	}{
		{
			name: "minimal",
			n:    commonv1alpha1.Notification{Name: "Discord", Implementation: "Discord"},
			check: func(t *testing.T, p map[string]any) {
				assert.Equal(t, "Discord", p["name"])
				assert.Equal(t, "DiscordSettings", p["configContract"])
				_, hasOnGrab := p["onGrab"]
				assert.False(t, hasOnGrab, "nil bools should not be set")
			},
		},
		{
			name: "with triggers",
			n: commonv1alpha1.Notification{
				Name: "Slack", Implementation: "Slack",
				OnGrab: new(true), OnDownload: new(false),
				OnMovieAdded: new(true),
			},
			check: func(t *testing.T, p map[string]any) {
				assert.Equal(t, true, p["onGrab"])
				assert.Equal(t, false, p["onDownload"])
				assert.Equal(t, true, p["onMovieAdded"])
				_, hasOnRename := p["onRename"]
				assert.False(t, hasOnRename)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := BuildNotificationPayload(tt.n)
			tt.check(t, p)
		})
	}
}

func TestBuildImportListPayload(t *testing.T) {
	sonarr := ImportListFields{AutomaticAdd: "enableAutomaticAdd", Monitor: "shouldMonitor", MonitorAll: "all"}
	radarr := ImportListFields{Enable: "enabled", AutomaticAdd: "enableAuto", Monitor: "monitor", MonitorAll: "movieOnly"}
	tests := []struct {
		name    string
		il      commonv1alpha1.ImportList
		qpIDs   map[string]int
		monitor ImportListFields
		check   func(t *testing.T, p map[string]any)
	}{
		{
			name: "minimal",
			il:   commonv1alpha1.ImportList{Name: "Trakt", Implementation: "TraktListImport"},
			check: func(t *testing.T, p map[string]any) {
				assert.NotContains(t, p, "enable")
				assert.Equal(t, "TraktListImportSettings", p["configContract"])
			},
		},
		{
			name:    "sonarr has no enable property",
			il:      commonv1alpha1.ImportList{Name: "List", Implementation: "CustomImport", EnableAutomaticAdd: new(true)},
			monitor: sonarr,
			check: func(t *testing.T, p map[string]any) {
				assert.NotContains(t, p, "enable", "Sonarr drops it, so it reads back missing and is rewritten forever")
				assert.Equal(t, true, p["enableAutomaticAdd"])
			},
		},
		{
			name:    "radarr names enable and automatic add differently",
			il:      commonv1alpha1.ImportList{Name: "List", Implementation: "RadarrListImport", Enable: new(false), EnableAutomaticAdd: new(true)},
			monitor: radarr,
			check: func(t *testing.T, p map[string]any) {
				assert.Equal(t, false, p["enabled"])
				assert.Equal(t, true, p["enableAuto"])
				assert.NotContains(t, p, "enableAutomaticAdd")
			},
		},
		{
			name: "with quality profile resolution",
			il: commonv1alpha1.ImportList{
				Name: "Trakt", Implementation: "TraktListImport",
				QualityProfileName: "HD-1080p", RootFolderPath: "/tv",
				Monitor: "all", ListOrder: new(1),
			},
			qpIDs:   map[string]int{"HD-1080p": 5},
			monitor: sonarr,
			check: func(t *testing.T, p map[string]any) {
				assert.Equal(t, 5, p["qualityProfileId"])
				assert.Equal(t, "/tv", p["rootFolderPath"])
				assert.Equal(t, "all", p["shouldMonitor"], "Sonarr reads the monitor enum from shouldMonitor")
				assert.NotContains(t, p, "monitor")
				assert.Equal(t, 1, p["listOrder"])
			},
		},
		{
			name:    "radarr names the monitor enum monitor",
			il:      commonv1alpha1.ImportList{Name: "List", Implementation: "RadarrListImport", Monitor: "movieAndCollection"},
			monitor: radarr,
			check: func(t *testing.T, p map[string]any) {
				assert.Equal(t, "movieAndCollection", p["monitor"])
				assert.NotContains(t, p, "shouldMonitor")
			},
		},
		{
			name:    "legacy shouldMonitor true monitors everything",
			il:      commonv1alpha1.ImportList{Name: "List", Implementation: "CustomImport", ShouldMonitor: new(true)},
			monitor: sonarr,
			check: func(t *testing.T, p map[string]any) {
				assert.Equal(t, "all", p["shouldMonitor"], "a bool is rejected by Sonarr 4")
			},
		},
		{
			name:    "legacy shouldMonitor false monitors nothing",
			il:      commonv1alpha1.ImportList{Name: "List", Implementation: "RadarrListImport", ShouldMonitor: new(false)},
			monitor: radarr,
			check: func(t *testing.T, p map[string]any) {
				assert.Equal(t, "none", p["monitor"])
			},
		},
		{
			name: "monitor wins over legacy shouldMonitor",
			il: commonv1alpha1.ImportList{
				Name: "List", Implementation: "CustomImport", Monitor: "future", ShouldMonitor: new(true),
			},
			monitor: sonarr,
			check: func(t *testing.T, p map[string]any) {
				assert.Equal(t, "future", p["shouldMonitor"])
			},
		},
		{
			name: "quality profile not found",
			il: commonv1alpha1.ImportList{
				Name: "Trakt", Implementation: "TraktListImport",
				QualityProfileName: "NonExistent",
			},
			qpIDs: map[string]int{"HD-1080p": 5},
			check: func(t *testing.T, p map[string]any) {
				_, has := p["qualityProfileId"]
				assert.False(t, has, "should not set ID for unresolved profile")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qpIDs := tt.qpIDs
			if qpIDs == nil {
				qpIDs = map[string]int{}
			}
			p := BuildImportListPayload(tt.il, qpIDs, tt.monitor)
			tt.check(t, p)
		})
	}
}

func TestBuildCustomFormatPayload(t *testing.T) {
	tests := []struct {
		name  string
		cf    commonv1alpha1.CustomFormat
		check func(t *testing.T, p map[string]any)
	}{
		{
			name: "minimal",
			cf:   commonv1alpha1.CustomFormat{Name: "BR-DISK"},
			check: func(t *testing.T, p map[string]any) {
				assert.Equal(t, "BR-DISK", p["name"])
				assert.Equal(t, false, p["includeCustomFormatWhenRenaming"])
				specs := p["specifications"].([]map[string]any)
				assert.Empty(t, specs)
			},
		},
		{
			name: "with specifications",
			cf: commonv1alpha1.CustomFormat{
				Name:                            "x265",
				IncludeCustomFormatWhenRenaming: new(true),
				Specifications: []commonv1alpha1.CustomFormatSpecification{
					{
						Name: "x265", Implementation: "ReleaseTitleSpecification",
						Negate: new(false), Required: new(true),
						Fields: []commonv1alpha1.ConfigField{
							{Name: "value", Value: fieldValue(`"x265"`)},
						},
					},
				},
			},
			check: func(t *testing.T, p map[string]any) {
				assert.Equal(t, true, p["includeCustomFormatWhenRenaming"])
				specs := p["specifications"].([]map[string]any)
				require.Len(t, specs, 1)
				assert.Equal(t, "x265", specs[0]["name"])
				assert.Equal(t, false, specs[0]["negate"])
				assert.Equal(t, true, specs[0]["required"])
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := BuildCustomFormatPayload(tt.cf)
			tt.check(t, p)
		})
	}
}

func TestBuildQualityProfilePayload(t *testing.T) {
	tests := []struct {
		name  string
		qp    commonv1alpha1.QualityProfile
		cfIDs map[string]int
		check func(t *testing.T, p map[string]any)
	}{
		{
			name:  "name only",
			qp:    commonv1alpha1.QualityProfile{Name: "HD-1080p"},
			cfIDs: map[string]int{},
			check: func(t *testing.T, p map[string]any) {
				assert.Equal(t, "HD-1080p", p["name"])
				_, hasUpgrade := p["upgradeAllowed"]
				assert.False(t, hasUpgrade)
			},
		},
		{
			name: "with all optional fields",
			qp: commonv1alpha1.QualityProfile{
				Name: "HD", UpgradeAllowed: new(true),
				MinFormatScore: new(10), CutoffFormatScore: new(100),
				MinUpgradeFormatScore: new(5),
			},
			cfIDs: map[string]int{},
			check: func(t *testing.T, p map[string]any) {
				assert.Equal(t, true, p["upgradeAllowed"])
				assert.Equal(t, 10, p["minFormatScore"])
				assert.Equal(t, 100, p["cutoffFormatScore"])
				assert.Equal(t, 5, p["minUpgradeFormatScore"])
			},
		},
		{
			name: "with format items",
			qp: commonv1alpha1.QualityProfile{
				Name: "HD",
				FormatItems: []commonv1alpha1.QualityProfileFormatItem{
					{Name: "x265", Score: 100},
					{Name: "BR-DISK", Score: -1000},
					{Name: "Unknown", Score: 50},
				},
			},
			cfIDs: map[string]int{"x265": 1, "BR-DISK": 2},
			check: func(t *testing.T, p map[string]any) {
				items := p["formatItems"].([]map[string]any)
				assert.Len(t, items, 2)
				assert.Equal(t, 1, items[0]["format"])
				assert.Equal(t, 100, items[0]["score"])
			},
		},
		{
			name: "with cutoff resolution",
			qp: commonv1alpha1.QualityProfile{
				Name:   "HD",
				Cutoff: "Bluray-1080p",
				Items: []commonv1alpha1.QualityProfileItem{
					{Quality: &commonv1alpha1.QualityReference{ID: 7, Name: "Bluray-1080p"}},
				},
			},
			cfIDs: map[string]int{},
			check: func(t *testing.T, p map[string]any) {
				assert.Equal(t, 7, p["cutoff"])
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := BuildQualityProfilePayload(tt.qp, tt.cfIDs)
			tt.check(t, p)
		})
	}
}

func TestBuildFieldsPayload(t *testing.T) {
	tests := []struct {
		name   string
		fields []commonv1alpha1.ConfigField
		check  func(t *testing.T, result []map[string]any)
	}{
		{
			name:   "empty",
			fields: []commonv1alpha1.ConfigField{},
			check: func(t *testing.T, result []map[string]any) {
				assert.Empty(t, result)
			},
		},
		{
			name: "with values",
			fields: []commonv1alpha1.ConfigField{
				{Name: "baseUrl", Value: fieldValue(`"http://example.com"`)},
				{Name: "apiKey", Value: fieldValue(`"secret"`)},
			},
			check: func(t *testing.T, result []map[string]any) {
				require.Len(t, result, 2)
				assert.Equal(t, "baseUrl", result[0]["name"])
				assert.Equal(t, "http://example.com", result[0]["value"])
			},
		},
		{
			name: "nil value",
			fields: []commonv1alpha1.ConfigField{
				{Name: "optional"},
			},
			check: func(t *testing.T, result []map[string]any) {
				require.Len(t, result, 1)
				assert.Nil(t, result[0]["value"])
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildFieldsPayload(tt.fields)
			tt.check(t, result)
		})
	}
}

func TestBuildTagsPayload(t *testing.T) {
	tests := []struct {
		name string
		tags []int
		want []any
	}{
		{"empty", []int{}, []any{}},
		{"multiple", []int{1, 5, 10}, []any{1, 5, 10}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildTagsPayload(tt.tags)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestSetOptionalBool(t *testing.T) {
	tests := []struct {
		name    string
		val     *bool
		wantSet bool
		want    bool
	}{
		{"nil not set", nil, false, false},
		{"true", new(true), true, true},
		{"false", new(false), true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := map[string]any{}
			SetOptionalBool(p, "key", tt.val)
			if tt.wantSet {
				assert.Equal(t, tt.want, p["key"])
			} else {
				_, exists := p["key"]
				assert.False(t, exists)
			}
		})
	}
}

func TestFindCutoffID(t *testing.T) {
	items := []commonv1alpha1.QualityProfileItem{
		{Quality: &commonv1alpha1.QualityReference{ID: 7, Name: "Bluray-1080p"}},
		{Name: "HD Group", Items: []commonv1alpha1.QualityProfileGroupItem{
			{Quality: commonv1alpha1.QualityReference{ID: 4, Name: "HDTV-1080p"}},
		}},
		{Quality: &commonv1alpha1.QualityReference{ID: 3, Name: "WEBDL-1080p"}},
	}

	tests := []struct {
		name   string
		cutoff string
		wantID int
		wantOK bool
	}{
		{"individual quality", "Bluray-1080p", 7, true},
		{"group", "HD Group", 1001, true},
		{"no match", "NonExistent", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := FindCutoffID(items, tt.cutoff)
			assert.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.wantID, id)
			}
		})
	}
}

func TestCompleteQualityItemsAddsUndeclaredQualitiesAsDisallowed(t *testing.T) {
	definition := func(id int, name string) map[string]any {
		return map[string]any{"quality": map[string]any{"id": float64(id), "name": name, "source": "web", "resolution": float64(1080)}}
	}
	definitions := []map[string]any{
		definition(0, "Unknown"), definition(1, "SDTV"), definition(3, "WEBDL-1080p"),
		definition(15, "WEBRip-1080p"), definition(7, "Bluray-1080p"),
	}
	declared := []commonv1alpha1.QualityProfileItem{
		{Name: "WEB 1080p", Items: []commonv1alpha1.QualityProfileGroupItem{
			{Quality: commonv1alpha1.QualityReference{ID: 3, Name: "WEBDL-1080p"}},
			{Quality: commonv1alpha1.QualityReference{ID: 15, Name: "WEBRip-1080p"}},
		}},
		{Quality: &commonv1alpha1.QualityReference{ID: 7, Name: "Bluray-1080p"}},
	}

	items := CompleteQualityItems(BuildQualityItems(declared), declared, definitions)

	require.Len(t, items, 4, "Unknown and SDTV are added; grouped and listed qualities are not repeated")
	for i, name := range []string{"Unknown", "SDTV"} {
		quality := items[i]["quality"].(map[string]any)
		assert.Equal(t, name, quality["name"])
		assert.Equal(t, false, items[i]["allowed"], "an undeclared quality must not become allowed")
	}
	assert.Equal(t, "WEB 1080p", items[2]["name"], "declared items keep their order after the added ones")
	bluray := items[3]["quality"].(map[string]any)
	assert.Equal(t, float64(7), bluray["id"], "Bluray-1080p stays last")
	assert.Equal(t, "web", bluray["source"], "qualities carry the app's own keys, or every write strips them")
	child := items[2]["items"].([]map[string]any)[0]["quality"].(map[string]any)
	assert.Equal(t, float64(1080), child["resolution"], "grouped qualities are resolved too")
}

func TestBuildQualityItems(t *testing.T) {
	tests := []struct {
		name  string
		items []commonv1alpha1.QualityProfileItem
		check func(t *testing.T, result []map[string]any)
	}{
		{
			name: "individual quality",
			items: []commonv1alpha1.QualityProfileItem{
				{Quality: &commonv1alpha1.QualityReference{ID: 7, Name: "Bluray-1080p"}},
			},
			check: func(t *testing.T, result []map[string]any) {
				require.Len(t, result, 1)
				q := result[0]["quality"].(map[string]any)
				assert.Equal(t, 7, q["id"])
				assert.Equal(t, "Bluray-1080p", q["name"])
				assert.Equal(t, true, result[0]["allowed"])
				assert.Equal(t, []any{}, result[0]["items"])
			},
		},
		{
			name: "group with children",
			items: []commonv1alpha1.QualityProfileItem{
				{
					Name:    "HD Group",
					Allowed: new(true),
					Items: []commonv1alpha1.QualityProfileGroupItem{
						{Quality: commonv1alpha1.QualityReference{ID: 4, Name: "HDTV"}, Allowed: new(false)},
					},
				},
			},
			check: func(t *testing.T, result []map[string]any) {
				require.Len(t, result, 1)
				assert.NotContains(t, result[0], "quality", "the apps omit quality on a group; null reads back as a change")
				assert.Equal(t, 1000, result[0]["id"], "the PVR apps treat an item without a group id as a single quality")
				assert.Equal(t, "HD Group", result[0]["name"])
				children := result[0]["items"].([]map[string]any)
				require.Len(t, children, 1)
				assert.Equal(t, false, children[0]["allowed"])
			},
		},
		{
			name: "allowed defaults to true",
			items: []commonv1alpha1.QualityProfileItem{
				{Quality: &commonv1alpha1.QualityReference{ID: 1, Name: "SDTV"}},
			},
			check: func(t *testing.T, result []map[string]any) {
				assert.Equal(t, true, result[0]["allowed"])
			},
		},
		{
			name: "allowed explicitly false",
			items: []commonv1alpha1.QualityProfileItem{
				{Quality: &commonv1alpha1.QualityReference{ID: 1, Name: "SDTV"}, Allowed: new(false)},
			},
			check: func(t *testing.T, result []map[string]any) {
				assert.Equal(t, false, result[0]["allowed"])
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildQualityItems(tt.items)
			tt.check(t, result)
		})
	}
}

// TestPayloadsAreJSONSerializable verifies all payload builders produce JSON-safe maps.
func TestPayloadsAreJSONSerializable(t *testing.T) {
	dc := BuildDownloadClientPayload(
		commonv1alpha1.DownloadClient{Name: "test", Protocol: "torrent", Implementation: "QBittorrent", Host: "h", Port: 80},
		DownloadClientResolvedSecrets{}, "tvCategory",
	)
	idx := BuildIndexerPayload(commonv1alpha1.Indexer{Name: "test", Protocol: "usenet", Implementation: "Newznab"})
	notif := BuildNotificationPayload(commonv1alpha1.Notification{Name: "test", Implementation: "Discord"})
	cf := BuildCustomFormatPayload(commonv1alpha1.CustomFormat{Name: "test"})

	for name, payload := range map[string]any{
		"downloadClient": dc, "indexer": idx, "notification": notif, "customFormat": cf,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := json.Marshal(payload)
			assert.NoError(t, err)
		})
	}
}
