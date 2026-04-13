package prowlarr

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	servarrv1alpha1 "github.com/kyleseneker/media-operator/api/servarr/v1alpha1"
)

func boolPtr(b bool) *bool { return &b }
func intPtr(i int) *int    { return &i }
func strPtr(s string) *string { return &s }

func TestProwlarrDefinition(t *testing.T) {
	def := ProwlarrDefinition()
	assert.Equal(t, "/api/v1/system/status", def.HealthPath)
	assert.Empty(t, def.Settings, "Prowlarr has no settings endpoints")
	assert.True(t, len(def.Resources) >= 6)

	// Tags should be first
	assert.Equal(t, "tags", def.Resources[0].Name)

	// Verify all paths use v1
	for _, r := range def.Resources {
		assert.Contains(t, r.Path, "/api/v1/")
	}
}

func TestProwlarrResources(t *testing.T) {
	tests := []struct {
		name     string
		opts     ProwlarrOptions
		wantKeys []string
	}{
		{
			name:     "empty options",
			opts:     ProwlarrOptions{},
			wantKeys: []string{},
		},
		{
			name: "all resource types",
			opts: ProwlarrOptions{
				Tags:         []commonv1alpha1.Tag{{Label: "test"}},
				Applications: []map[string]interface{}{{"name": "sonarr"}},
				Indexers:     []servarrv1alpha1.ProwlarrIndexer{{Name: "idx"}},
				Proxies:      []servarrv1alpha1.ProwlarrProxy{{Name: "proxy"}},
				DownloadClients: []servarrv1alpha1.ProwlarrDownloadClient{
					{Name: "dc", Protocol: "torrent", Implementation: "QBittorrent"},
				},
				Notifications: []commonv1alpha1.Notification{{Name: "notif", Implementation: "Discord"}},
			},
			wantKeys: []string{"tags", "applications", "indexers", "proxies", "downloadClients", "notifications"},
		},
		{
			name: "tags only",
			opts: ProwlarrOptions{
				Tags: []commonv1alpha1.Tag{{Label: "tag1"}, {Label: "tag2"}},
			},
			wantKeys: []string{"tags"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resources := ProwlarrResources(tt.opts)
			assert.Len(t, resources, len(tt.wantKeys))
			for _, k := range tt.wantKeys {
				assert.Contains(t, resources, k)
			}
		})
	}
}

func TestBuildProwlarrApplicationPayload(t *testing.T) {
	app := servarrv1alpha1.ProwlarrApplication{
		Name:           "Sonarr",
		SyncLevel:      "fullSync",
		Implementation: "Sonarr",
		ConfigContract: "SonarrSettings",
		ProwlarrUrl:    "http://prowlarr:9696",
		BaseUrl:        "http://sonarr:8989",
		SyncCategories: []int{5030, 5040},
		Tags:           []int{1, 2},
	}

	p := BuildProwlarrApplicationPayload(app, "sonarr-api-key")

	assert.Equal(t, "Sonarr", p["name"])
	assert.Equal(t, "fullSync", p["syncLevel"])

	fields := p["fields"].([]map[string]interface{})
	require.Len(t, fields, 4)

	fieldMap := map[string]interface{}{}
	for _, f := range fields {
		fieldMap[f["name"].(string)] = f["value"]
	}
	assert.Equal(t, "sonarr-api-key", fieldMap["apiKey"])
	assert.Equal(t, "http://prowlarr:9696", fieldMap["prowlarrUrl"])

	syncCats := fieldMap["syncCategories"].([]interface{})
	assert.Len(t, syncCats, 2)

	tags := p["tags"].([]interface{})
	assert.Len(t, tags, 2)
}

func TestBuildProwlarrIndexerPayload(t *testing.T) {
	tests := []struct {
		name  string
		idx   servarrv1alpha1.ProwlarrIndexer
		check func(t *testing.T, p map[string]interface{})
	}{
		{
			name: "defaults",
			idx: servarrv1alpha1.ProwlarrIndexer{
				Name: "NZBgeek", Implementation: "Newznab", ConfigContract: "NewznabSettings",
			},
			check: func(t *testing.T, p map[string]interface{}) {
				assert.Equal(t, true, p["enable"])
				_, hasAppProfile := p["appProfileId"]
				assert.False(t, hasAppProfile)
				_, hasPriority := p["priority"]
				assert.False(t, hasPriority)
			},
		},
		{
			name: "all options",
			idx: servarrv1alpha1.ProwlarrIndexer{
				Name: "NZBgeek", Implementation: "Newznab", ConfigContract: "NewznabSettings",
				Enable: boolPtr(false), AppProfileId: intPtr(1), Priority: intPtr(25),
				Fields: []servarrv1alpha1.ProwlarrField{
					{Name: "baseUrl", Value: strPtr("http://nzbgeek.info")},
					{Name: "apiKey", Value: nil},
				},
				Tags: []int{1},
			},
			check: func(t *testing.T, p map[string]interface{}) {
				assert.Equal(t, false, p["enable"])
				assert.Equal(t, 1, p["appProfileId"])
				assert.Equal(t, 25, p["priority"])

				fields := p["fields"].([]map[string]interface{})
				require.Len(t, fields, 2)
				assert.Equal(t, "http://nzbgeek.info", fields[0]["value"])
				assert.Equal(t, "", fields[1]["value"]) // nil Value becomes ""
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := BuildProwlarrIndexerPayload(tt.idx)
			tt.check(t, p)
		})
	}
}

func TestBuildProwlarrProxyPayload(t *testing.T) {
	tests := []struct {
		name  string
		proxy servarrv1alpha1.ProwlarrProxy
		check func(t *testing.T, p map[string]interface{})
	}{
		{
			name:  "minimal",
			proxy: servarrv1alpha1.ProwlarrProxy{Name: "Proxy", Implementation: "Http", ConfigContract: "HttpSettings", Host: "proxy.local"},
			check: func(t *testing.T, p map[string]interface{}) {
				assert.Equal(t, "Proxy", p["name"])
				fields := p["fields"].([]map[string]interface{})
				require.Len(t, fields, 1)
				assert.Equal(t, "proxy.local", fields[0]["value"])
			},
		},
		{
			name: "with timeout",
			proxy: servarrv1alpha1.ProwlarrProxy{
				Name: "Proxy", Implementation: "Socks5", ConfigContract: "Socks5Settings",
				Host: "proxy.local", RequestTimeout: intPtr(30),
			},
			check: func(t *testing.T, p map[string]interface{}) {
				fields := p["fields"].([]map[string]interface{})
				require.Len(t, fields, 2)
				assert.Equal(t, "requestTimeout", fields[1]["name"])
				assert.Equal(t, 30, fields[1]["value"])
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := BuildProwlarrProxyPayload(tt.proxy)
			tt.check(t, p)
		})
	}
}

func TestBuildProwlarrDownloadClientPayload(t *testing.T) {
	tests := []struct {
		name  string
		dc    servarrv1alpha1.ProwlarrDownloadClient
		check func(t *testing.T, p map[string]interface{})
	}{
		{
			name: "defaults",
			dc: servarrv1alpha1.ProwlarrDownloadClient{
				Name: "qBit", Protocol: "torrent", Implementation: "QBittorrent",
			},
			check: func(t *testing.T, p map[string]interface{}) {
				assert.Equal(t, true, p["enable"])
				assert.Equal(t, "QBittorrentSettings", p["configContract"])
				_, hasPriority := p["priority"]
				assert.False(t, hasPriority)
			},
		},
		{
			name: "with categories and custom contract",
			dc: servarrv1alpha1.ProwlarrDownloadClient{
				Name: "qBit", Protocol: "torrent", Implementation: "QBittorrent",
				ConfigContract: "CustomContract", Priority: intPtr(5),
				Categories: []servarrv1alpha1.ProwlarrDownloadClientCategory{
					{ClientCategory: "tv", Categories: []int{5030, 5040}},
				},
				Fields: []servarrv1alpha1.ProwlarrField{
					{Name: "host", Value: strPtr("qbit.local")},
				},
			},
			check: func(t *testing.T, p map[string]interface{}) {
				assert.Equal(t, "CustomContract", p["configContract"])
				assert.Equal(t, 5, p["priority"])

				cats := p["categories"].([]map[string]interface{})
				require.Len(t, cats, 1)
				assert.Equal(t, "tv", cats[0]["clientCategory"])
				catIDs := cats[0]["categories"].([]interface{})
				assert.Len(t, catIDs, 2)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := BuildProwlarrDownloadClientPayload(tt.dc)
			tt.check(t, p)
		})
	}
}

func TestIntSliceToInterface(t *testing.T) {
	tests := []struct {
		name  string
		input []int
		want  []interface{}
	}{
		{"empty", []int{}, []interface{}{}},
		{"values", []int{1, 2, 3}, []interface{}{1, 2, 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, intSliceToInterface(tt.input))
		})
	}
}
