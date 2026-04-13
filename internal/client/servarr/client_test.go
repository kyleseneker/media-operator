package servarr

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
)

func boolPtr(b bool) *bool { return &b }
func intPtr(i int) *int    { return &i }

func fieldValue(raw string) *commonv1alpha1.FieldValue {
	fv := &commonv1alpha1.FieldValue{}
	_ = fv.UnmarshalJSON([]byte(raw))
	return fv
}

func TestServarrDefinition(t *testing.T) {
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
			def := ServarrDefinition(tt.apiVersion)
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

func TestServarrDefinition_ResourceOrder(t *testing.T) {
	def := ServarrDefinition("v3")
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

func TestServarrSections(t *testing.T) {
	type testSpec struct {
		MediaManagement      *map[string]interface{} `json:"mediaManagement,omitempty"`
		Naming               *map[string]interface{} `json:"naming,omitempty"`
		IndexerConfig        *map[string]interface{} `json:"indexerConfig,omitempty"`
		DownloadClientConfig *map[string]interface{} `json:"downloadClientConfig,omitempty"`
		UI                   *map[string]interface{} `json:"ui,omitempty"`
	}

	tests := []struct {
		name     string
		spec     interface{}
		wantKeys []string
	}{
		{
			name: "all sections",
			spec: testSpec{
				MediaManagement: &map[string]interface{}{"a": 1},
				Naming:          &map[string]interface{}{"b": 2},
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
			sections, err := ServarrSections(tt.spec)
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
		check         func(t *testing.T, p map[string]interface{})
	}{
		{
			name: "defaults",
			dc: commonv1alpha1.DownloadClient{
				Name: "qBit", Protocol: "torrent", Implementation: "QBittorrent",
				Host: "qbit", Port: 8080, Category: "tv",
			},
			categoryField: "tvCategory",
			check: func(t *testing.T, p map[string]interface{}) {
				assert.Equal(t, "qBit", p["name"])
				assert.Equal(t, true, p["enable"])
				assert.Equal(t, 1, p["priority"])
				assert.Equal(t, true, p["removeCompletedDownloads"])
				assert.Equal(t, true, p["removeFailedDownloads"])
				assert.Equal(t, "QBittorrentSettings", p["configContract"])

				fields := p["fields"].([]map[string]interface{})
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
				Enable: boolPtr(false), Priority: intPtr(5),
				RemoveCompletedDownloads: boolPtr(false),
				RemoveFailedDownloads:    boolPtr(false),
				UseSsl:                   boolPtr(true),
				UrlBase:                  "/qbt",
				ConfigContract:           "CustomContract",
				Tags:                     []int{1, 2},
			},
			categoryField: "movieCategory",
			check: func(t *testing.T, p map[string]interface{}) {
				assert.Equal(t, false, p["enable"])
				assert.Equal(t, 5, p["priority"])
				assert.Equal(t, false, p["removeCompletedDownloads"])
				assert.Equal(t, false, p["removeFailedDownloads"])
				assert.Equal(t, "CustomContract", p["configContract"])
				assert.Len(t, p["tags"], 2)

				fields := p["fields"].([]map[string]interface{})
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
			check: func(t *testing.T, p map[string]interface{}) {
				fields := p["fields"].([]map[string]interface{})
				names := make(map[string]interface{})
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
			check: func(t *testing.T, p map[string]interface{}) {
				fields := p["fields"].([]map[string]interface{})
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
		check func(t *testing.T, p map[string]interface{})
	}{
		{
			name: "defaults",
			idx: commonv1alpha1.Indexer{
				Name: "NZBgeek", Protocol: "usenet", Implementation: "Newznab",
			},
			check: func(t *testing.T, p map[string]interface{}) {
				assert.Equal(t, true, p["enable"])
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
				Enable: boolPtr(false), EnableRss: boolPtr(false),
				EnableAutomaticSearch: boolPtr(false), EnableInteractiveSearch: boolPtr(false),
				ConfigContract: "CustomContract", Priority: intPtr(10),
				Tags: []int{1}, Fields: []commonv1alpha1.ConfigField{
					{Name: "baseUrl", Value: fieldValue(`"http://nzbgeek.info"`)},
				},
			},
			check: func(t *testing.T, p map[string]interface{}) {
				assert.Equal(t, false, p["enable"])
				assert.Equal(t, false, p["enableRss"])
				assert.Equal(t, "CustomContract", p["configContract"])
				assert.Equal(t, 10, p["priority"])
				tags := p["tags"].([]interface{})
				assert.Len(t, tags, 1)
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
		check func(t *testing.T, p map[string]interface{})
	}{
		{
			name: "minimal",
			n:    commonv1alpha1.Notification{Name: "Discord", Implementation: "Discord"},
			check: func(t *testing.T, p map[string]interface{}) {
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
				OnGrab: boolPtr(true), OnDownload: boolPtr(false),
				OnMovieAdded: boolPtr(true),
			},
			check: func(t *testing.T, p map[string]interface{}) {
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
	tests := []struct {
		name  string
		il    commonv1alpha1.ImportList
		qpIDs map[string]int
		check func(t *testing.T, p map[string]interface{})
	}{
		{
			name: "minimal",
			il:   commonv1alpha1.ImportList{Name: "Trakt", Implementation: "TraktListImport"},
			check: func(t *testing.T, p map[string]interface{}) {
				assert.Equal(t, true, p["enable"])
				assert.Equal(t, "TraktListImportSettings", p["configContract"])
			},
		},
		{
			name: "with quality profile resolution",
			il: commonv1alpha1.ImportList{
				Name: "Trakt", Implementation: "TraktListImport",
				QualityProfileName: "HD-1080p", RootFolderPath: "/tv",
				Monitor: "all", ListOrder: intPtr(1),
			},
			qpIDs: map[string]int{"HD-1080p": 5},
			check: func(t *testing.T, p map[string]interface{}) {
				assert.Equal(t, 5, p["qualityProfileId"])
				assert.Equal(t, "/tv", p["rootFolderPath"])
				assert.Equal(t, "all", p["monitor"])
				assert.Equal(t, 1, p["listOrder"])
			},
		},
		{
			name: "quality profile not found",
			il: commonv1alpha1.ImportList{
				Name: "Trakt", Implementation: "TraktListImport",
				QualityProfileName: "NonExistent",
			},
			qpIDs: map[string]int{"HD-1080p": 5},
			check: func(t *testing.T, p map[string]interface{}) {
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
			p := BuildImportListPayload(tt.il, qpIDs)
			tt.check(t, p)
		})
	}
}

func TestBuildCustomFormatPayload(t *testing.T) {
	tests := []struct {
		name  string
		cf    commonv1alpha1.CustomFormat
		check func(t *testing.T, p map[string]interface{})
	}{
		{
			name: "minimal",
			cf:   commonv1alpha1.CustomFormat{Name: "BR-DISK"},
			check: func(t *testing.T, p map[string]interface{}) {
				assert.Equal(t, "BR-DISK", p["name"])
				assert.Equal(t, false, p["includeCustomFormatWhenRenaming"])
				specs := p["specifications"].([]map[string]interface{})
				assert.Empty(t, specs)
			},
		},
		{
			name: "with specifications",
			cf: commonv1alpha1.CustomFormat{
				Name:                            "x265",
				IncludeCustomFormatWhenRenaming: boolPtr(true),
				Specifications: []commonv1alpha1.CustomFormatSpecification{
					{
						Name: "x265", Implementation: "ReleaseTitleSpecification",
						Negate: boolPtr(false), Required: boolPtr(true),
						Fields: []commonv1alpha1.ConfigField{
							{Name: "value", Value: fieldValue(`"x265"`)},
						},
					},
				},
			},
			check: func(t *testing.T, p map[string]interface{}) {
				assert.Equal(t, true, p["includeCustomFormatWhenRenaming"])
				specs := p["specifications"].([]map[string]interface{})
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
		check func(t *testing.T, p map[string]interface{})
	}{
		{
			name:  "name only",
			qp:    commonv1alpha1.QualityProfile{Name: "HD-1080p"},
			cfIDs: map[string]int{},
			check: func(t *testing.T, p map[string]interface{}) {
				assert.Equal(t, "HD-1080p", p["name"])
				_, hasUpgrade := p["upgradeAllowed"]
				assert.False(t, hasUpgrade)
			},
		},
		{
			name: "with all optional fields",
			qp: commonv1alpha1.QualityProfile{
				Name: "HD", UpgradeAllowed: boolPtr(true),
				MinFormatScore: intPtr(10), CutoffFormatScore: intPtr(100),
				MinUpgradeFormatScore: intPtr(5),
			},
			cfIDs: map[string]int{},
			check: func(t *testing.T, p map[string]interface{}) {
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
			check: func(t *testing.T, p map[string]interface{}) {
				items := p["formatItems"].([]map[string]interface{})
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
			check: func(t *testing.T, p map[string]interface{}) {
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
		check  func(t *testing.T, result []map[string]interface{})
	}{
		{
			name:   "empty",
			fields: []commonv1alpha1.ConfigField{},
			check: func(t *testing.T, result []map[string]interface{}) {
				assert.Empty(t, result)
			},
		},
		{
			name: "with values",
			fields: []commonv1alpha1.ConfigField{
				{Name: "baseUrl", Value: fieldValue(`"http://example.com"`)},
				{Name: "apiKey", Value: fieldValue(`"secret"`)},
			},
			check: func(t *testing.T, result []map[string]interface{}) {
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
			check: func(t *testing.T, result []map[string]interface{}) {
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
		want []interface{}
	}{
		{"empty", []int{}, []interface{}{}},
		{"multiple", []int{1, 5, 10}, []interface{}{1, 5, 10}},
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
		{"true", boolPtr(true), true, true},
		{"false", boolPtr(false), true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := map[string]interface{}{}
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

func TestBuildQualityItems(t *testing.T) {
	tests := []struct {
		name  string
		items []commonv1alpha1.QualityProfileItem
		check func(t *testing.T, result []map[string]interface{})
	}{
		{
			name: "individual quality",
			items: []commonv1alpha1.QualityProfileItem{
				{Quality: &commonv1alpha1.QualityReference{ID: 7, Name: "Bluray-1080p"}},
			},
			check: func(t *testing.T, result []map[string]interface{}) {
				require.Len(t, result, 1)
				q := result[0]["quality"].(map[string]interface{})
				assert.Equal(t, 7, q["id"])
				assert.Equal(t, "Bluray-1080p", q["name"])
				assert.Equal(t, true, result[0]["allowed"])
				assert.Equal(t, []interface{}{}, result[0]["items"])
			},
		},
		{
			name: "group with children",
			items: []commonv1alpha1.QualityProfileItem{
				{
					Name:    "HD Group",
					Allowed: boolPtr(true),
					Items: []commonv1alpha1.QualityProfileGroupItem{
						{Quality: commonv1alpha1.QualityReference{ID: 4, Name: "HDTV"}, Allowed: boolPtr(false)},
					},
				},
			},
			check: func(t *testing.T, result []map[string]interface{}) {
				require.Len(t, result, 1)
				assert.Nil(t, result[0]["quality"])
				assert.Equal(t, "HD Group", result[0]["name"])
				children := result[0]["items"].([]map[string]interface{})
				require.Len(t, children, 1)
				assert.Equal(t, false, children[0]["allowed"])
			},
		},
		{
			name: "allowed defaults to true",
			items: []commonv1alpha1.QualityProfileItem{
				{Quality: &commonv1alpha1.QualityReference{ID: 1, Name: "SDTV"}},
			},
			check: func(t *testing.T, result []map[string]interface{}) {
				assert.Equal(t, true, result[0]["allowed"])
			},
		},
		{
			name: "allowed explicitly false",
			items: []commonv1alpha1.QualityProfileItem{
				{Quality: &commonv1alpha1.QualityReference{ID: 1, Name: "SDTV"}, Allowed: boolPtr(false)},
			},
			check: func(t *testing.T, result []map[string]interface{}) {
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

	for name, payload := range map[string]interface{}{
		"downloadClient": dc, "indexer": idx, "notification": notif, "customFormat": cf,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := json.Marshal(payload)
			assert.NoError(t, err)
		})
	}
}
