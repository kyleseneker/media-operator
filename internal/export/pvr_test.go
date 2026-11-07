package export

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	pvrv1alpha1 "github.com/kyleseneker/media-operator/api/pvr/v1alpha1"
)

const fakeSonarrKey = "sonarr-key"

var fakeSonarrAPI = map[string]string{
	"/config/mediamanagement": `{"id":1,"recycleBin":"/trash","deleteEmptyFolders":true,"notInTheCRD":5}`,
	"/config/naming":          `{"id":1}`,
	"/config/indexer":         `{"id":1}`,
	"/config/downloadclient":  `{"id":1}`,
	"/config/ui":              `{"id":1}`,
	"/tag":                    `[{"id":1,"label":"anime"}]`,
	"/rootfolder":             `[{"id":1,"path":"/tv","freeSpace":123}]`,
	"/remotepathmapping":      `[]`,
	"/customformat": `[{"id":1,"name":"x265","includeCustomFormatWhenRenaming":false,"specifications":[
		{"name":"x265","implementation":"ReleaseTitleSpecification","negate":false,"required":true,"fields":[{"name":"value","value":"x265"}]}]}]`,
	"/qualityprofile": `[{"id":4,"name":"HD","upgradeAllowed":true,"cutoff":1001,"minFormatScore":0,"items":[
		{"quality":{"id":1,"name":"SDTV","source":"television"},"items":[],"allowed":false},
		{"id":1001,"name":"WEB 1080p","items":[{"quality":{"id":3,"name":"WEBDL-1080p"},"items":[],"allowed":true}],"allowed":true}],
		"formatItems":[{"format":1,"name":"x265","score":-100},{"format":2,"name":"Other","score":0}]}]`,
	"/downloadclient": `[{"id":1,"name":"qBit","enable":true,"protocol":"torrent","priority":1,"removeCompletedDownloads":true,
		"removeFailedDownloads":true,"implementation":"QBittorrent","configContract":"QBittorrentSettings","tags":[1],"fields":[
		{"name":"host","value":"qbit"},{"name":"port","value":8080},{"name":"useSsl","value":false},{"name":"urlBase","value":""},
		{"name":"username","value":"admin","privacy":"userName"},{"name":"password","value":"********","privacy":"password"},
		{"name":"tvCategory","value":"tv"},{"name":"initialState","value":0},{"name":"sequentialOrder","value":true},
		{"name":"rpcToken","value":"********","privacy":"password"}]}]`,
	"/downloadclient/schema": `[{"implementation":"QBittorrent","fields":[{"name":"initialState","value":0},{"name":"sequentialOrder","value":false}]}]`,
	"/indexer": `[{"id":1,"name":"Torznab","protocol":"TorrentDownloadProtocol","implementation":"Torznab","configContract":"TorznabSettings",
		"enableRss":true,"enableAutomaticSearch":true,"enableInteractiveSearch":false,"priority":25,"tags":[],"fields":[
		{"name":"baseUrl","value":"http://jackett"},{"name":"apiKey","value":"********","privacy":"apiKey"},{"name":"minimumSeeders","value":1}]}]`,
	"/indexer/schema": `[{"implementation":"Torznab","fields":[{"name":"baseUrl","value":""},{"name":"minimumSeeders","value":1}]}]`,
	"/notification": `[{"id":1,"name":"Hook","implementation":"Webhook","configContract":"WebhookSettings","onGrab":true,
		"supportsOnGrab":true,"onRename":false,"supportsOnRename":false,"includeHealthWarnings":true,"tags":[],
		"fields":[{"name":"url","value":"http://hook"}]}]`,
	"/notification/schema": `[{"implementation":"Webhook","fields":[{"name":"url","value":""}]}]`,
	"/importlist": `[{"id":1,"name":"Trakt","implementation":"TraktListImport","configContract":"TraktListSettings",
		"enableAutomaticAdd":true,"shouldMonitor":"all","qualityProfileId":4,"rootFolderPath":"/tv","seasonFolder":true,
		"seriesType":"standard","tags":[1],"fields":[{"name":"accessToken","value":"********","privacy":"password"}]}]`,
	"/importlist/schema": `[{"implementation":"TraktListImport","fields":[]}]`,
}

func fakeSonarr(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != fakeSonarrKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, ok := fakeSonarrAPI[r.URL.Path[len("/api/v3"):]]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func exportSonarr(t *testing.T, o Options) (*pvrv1alpha1.SonarrConfig, Result) {
	t.Helper()
	o.App, o.URL, o.APIKey = "sonarr", fakeSonarr(t), fakeSonarrKey
	res, err := Run(context.Background(), o)
	require.NoError(t, err)
	cfg, ok := res.Config.(*pvrv1alpha1.SonarrConfig)
	require.True(t, ok)
	return cfg, res
}

func TestPVRSettingsKeepOnlyFieldsTheCRDHas(t *testing.T) {
	cfg, _ := exportSonarr(t, Options{})
	require.NotNil(t, cfg.Spec.MediaManagement)
	assert.Equal(t, "/trash", cfg.Spec.MediaManagement.RecycleBin)
	assert.True(t, *cfg.Spec.MediaManagement.DeleteEmptyFolders)
	assert.Equal(t, []commonv1alpha1.Tag{{Label: "anime"}}, cfg.Spec.Tags)
	assert.Equal(t, []commonv1alpha1.RootFolder{{Path: "/tv"}}, cfg.Spec.RootFolders)
}

func TestPVRDownloadClientMovesCredentialsToTheSecret(t *testing.T) {
	cfg, res := exportSonarr(t, Options{})
	require.Len(t, cfg.Spec.DownloadClients, 1)
	dc := cfg.Spec.DownloadClients[0]
	assert.Equal(t, "qbit", dc.Host)
	assert.Equal(t, 8080, dc.Port)
	assert.Equal(t, "tv", dc.Category)
	assert.Empty(t, dc.ConfigContract, "the default contract is implied")
	assert.Equal(t, []string{"anime"}, dc.TagLabels)
	assert.Equal(t, "download-client-qbit-username", dc.UsernameSecretRef.Key)
	assert.Equal(t, "download-client-qbit-password", dc.PasswordSecretRef.Key)
	require.Len(t, dc.Fields, 1, "fields left at the schema default are omitted")
	assert.Equal(t, "sequentialOrder", dc.Fields[0].Name)

	assert.Equal(t, "admin", res.Secret.StringData["download-client-qbit-username"], "a visible username is filled in")
	assert.Equal(t, fakeSonarrKey, res.Secret.StringData["api-key"])
	assert.Equal(t, []string{"download-client-qbit-password", "indexer-torznab-apikey", "import-list-trakt-accesstoken"}, res.Unfilled)
}

func TestPVRDownloadClientWarnsAboutAHiddenFieldItCannotReference(t *testing.T) {
	cfg, res := exportSonarr(t, Options{})
	for _, f := range cfg.Spec.DownloadClients[0].Fields {
		assert.NotEqual(t, "rpcToken", f.Name)
	}
	assert.Contains(t, res.Warnings,
		`download-client "qBit": the app hides rpcToken and the config can't read it from a Secret; set it in the app`)
}

func TestPVRIndexerNormalizesProtocolAndReferencesMaskedFields(t *testing.T) {
	cfg, _ := exportSonarr(t, Options{})
	require.Len(t, cfg.Spec.Indexers, 1)
	idx := cfg.Spec.Indexers[0]
	assert.Equal(t, "torrent", idx.Protocol)
	assert.False(t, *idx.EnableInteractiveSearch)
	assert.Equal(t, 25, *idx.Priority)
	require.Len(t, idx.Fields, 2)
	assert.Equal(t, `"http://jackett"`, string(idx.Fields[0].Value.Raw))
	assert.Equal(t, "indexer-torznab-apikey", idx.Fields[1].ValueFrom.Key)
}

func TestPVRNotificationSkipsEventsTheImplementationLacks(t *testing.T) {
	cfg, _ := exportSonarr(t, Options{})
	require.Len(t, cfg.Spec.Notifications, 1)
	n := cfg.Spec.Notifications[0]
	assert.True(t, *n.OnGrab)
	assert.Nil(t, n.OnRename, "an unsupported event would fail validation")
	assert.True(t, *n.IncludeHealthWarnings)
}

func TestPVRImportListUsesTheAppsPropertyNames(t *testing.T) {
	cfg, _ := exportSonarr(t, Options{})
	require.Len(t, cfg.Spec.ImportLists, 1)
	il := cfg.Spec.ImportLists[0]
	assert.Equal(t, "all", il.Monitor)
	assert.True(t, *il.EnableAutomaticAdd)
	assert.Equal(t, "HD", il.QualityProfileName)
	assert.Equal(t, []string{"anime"}, il.TagLabels)
	assert.Equal(t, "import-list-trakt-accesstoken", il.Fields[0].ValueFrom.Key)
}

func TestPVRQualityProfileNamesItsCutoffAndScoredFormats(t *testing.T) {
	cfg, _ := exportSonarr(t, Options{})
	require.Len(t, cfg.Spec.QualityProfiles, 1)
	qp := cfg.Spec.QualityProfiles[0]
	assert.Equal(t, "WEB 1080p", qp.Cutoff)
	require.Len(t, qp.Items, 2)
	assert.Equal(t, "SDTV", qp.Items[0].Quality.Name)
	assert.False(t, *qp.Items[0].Allowed)
	assert.Equal(t, "WEB 1080p", qp.Items[1].Name)
	assert.Equal(t, 3, qp.Items[1].Items[0].Quality.ID)
	assert.Equal(t, []commonv1alpha1.QualityProfileFormatItem{{Name: "x265", Score: -100}}, qp.FormatItems)

	require.Len(t, cfg.Spec.CustomFormats, 1)
	raw, err := json.Marshal(cfg.Spec.CustomFormats[0].Specifications[0].Fields)
	require.NoError(t, err)
	assert.JSONEq(t, `[{"name":"value","value":"x265"}]`, string(raw))
}
