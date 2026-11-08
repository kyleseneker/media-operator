package export

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	subtitlesv1alpha1 "github.com/kyleseneker/media-operator/api/subtitles/v1alpha1"
)

const fakeBazarrSettings = `{
	"general": {"use_sonarr": true, "use_radarr": false, "minimum_score": 90, "hi_extension": "hi",
		"enabled_providers": ["opensubtitlescom", "podnapisi"], "enabled_languages": ["fr"],
		"path_mappings": [["/tv", "/media/tv"]], "path_mappings_movie": [], "chmod": "0640",
		"serie_default_enabled": true, "serie_default_profile": 2, "movie_default_enabled": false, "movie_default_profile": 1},
	"sonarr": {"ip": "sonarr", "port": 8989, "base_url": "/", "apikey": "sonarr-key", "series_sync": 60, "ssl": false,
		"excluded_tags": ["no-subs"], "excluded_series_types": []},
	"radarr": {"ip": "radarr", "apikey": "radarr-key"},
	"subsync": {"use_subsync": true, "subsync_threshold": 90},
	"opensubtitlescom": {"username": "me", "password": "hunter2", "use_hash": true, "vip": false, "extra": {"nested": 1}}
}`

const fakeBazarrProfiles = `[
	{"profileId": 1, "name": "English", "cutoff": null, "mustContain": [], "mustNotContain": [], "originalFormat": false,
		"items": [{"id": 1, "language": "en", "hi": "False", "forced": "True", "audio_exclude": "False"}]},
	{"profileId": 2, "name": "Picky", "cutoff": 1, "mustContain": ["x"], "mustNotContain": [], "originalFormat": false,
		"items": [{"id": 3, "language": "es", "hi": "False", "forced": "False", "audio_exclude": "False"}]}
]`

func exportBazarrFrom(t *testing.T) (*subtitlesv1alpha1.BazarrConfig, Result) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-KEY") != "k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/system/settings":
			_, _ = w.Write([]byte(fakeBazarrSettings))
		case "/api/system/languages/profiles":
			_, _ = w.Write([]byte(fakeBazarrProfiles))
		case "/api/system/languages":
			_, _ = w.Write([]byte(`[{"code2":"en","enabled":true},{"code2":"es","enabled":true},{"code2":"fr","enabled":false}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	res, err := Run(context.Background(), Options{App: "bazarr", URL: srv.URL, APIKey: "k"})
	require.NoError(t, err)
	cfg, ok := res.Config.(*subtitlesv1alpha1.BazarrConfig)
	require.True(t, ok)
	return cfg, res
}

func TestBazarrSettingsMapBackToSpecFields(t *testing.T) {
	cfg, res := exportBazarrFrom(t)
	assert.Equal(t, 90, *cfg.Spec.General.MinimumScore)
	assert.Equal(t, "hi", cfg.Spec.General.HiExtension)
	assert.True(t, *cfg.Spec.SubSync.UseSubsync)

	require.NotNil(t, cfg.Spec.SonarrConnection)
	assert.Equal(t, "sonarr", cfg.Spec.SonarrConnection.Host)
	assert.Equal(t, 60, *cfg.Spec.SonarrConnection.SyncInterval)
	assert.Equal(t, "sonarr-api-key", cfg.Spec.SonarrConnection.APIKeySecretRef.Key)
	assert.Equal(t, "sonarr-key", res.Secret.StringData["sonarr-api-key"])
	assert.Nil(t, cfg.Spec.RadarrConnection, "Bazarr does not use Radarr")
	assert.Equal(t, []string{"en", "es"}, cfg.Spec.Languages.Enabled)

	assert.Equal(t, []subtitlesv1alpha1.BazarrPathMapping{{RemotePath: "/tv", LocalPath: "/media/tv"}}, cfg.Spec.General.PathMappings)
	assert.Empty(t, cfg.Spec.General.PathMappingsMovie)
	assert.Equal(t, "0640", cfg.Spec.General.Chmod)
	assert.Equal(t, "Picky", cfg.Spec.General.SeriesDefaultProfile, "the profile id is exported by name")
	assert.Empty(t, cfg.Spec.General.MovieDefaultProfile, "a default that is off isn't exported")
	assert.Equal(t, []string{"no-subs"}, cfg.Spec.SonarrConnection.ExcludedTags)
	assert.Empty(t, cfg.Spec.SonarrConnection.ExcludedSeriesTypes)
}

func TestBazarrProviderSecretsMoveToTheSecret(t *testing.T) {
	cfg, res := exportBazarrFrom(t)
	require.Len(t, cfg.Spec.Providers, 2)
	os := cfg.Spec.Providers[0]
	assert.Equal(t, "opensubtitlescom", os.Name)
	assert.Equal(t, map[string]string{
		"username": "me", "password": "ENV:bazarr-credentials:provider-opensubtitlescom-password",
		"use_hash": "true", "vip": "false",
	}, os.Settings)
	assert.Equal(t, "hunter2", res.Secret.StringData["provider-opensubtitlescom-password"])
	assert.Contains(t, res.Warnings, `provider "opensubtitlescom": BazarrConfig provider settings hold text, so extra stays as set in Bazarr`)
	assert.Empty(t, cfg.Spec.Providers[1].Settings)
}

func TestBazarrProfilesWarnAboutWhatTheConfigWouldClear(t *testing.T) {
	cfg, res := exportBazarrFrom(t)
	require.Len(t, cfg.Spec.Languages.Profiles, 2)
	english := cfg.Spec.Languages.Profiles[0]
	assert.Equal(t, 1, *english.ProfileId)
	assert.True(t, *english.Items[0].Forced)
	assert.False(t, *english.Items[0].HI)
	assert.Contains(t, res.Warnings, `language profile "Picky": BazarrConfig can't express its cutoff, must-contain, `+
		`must-not-contain or original-format setting, and applying the config would clear it`)
	assert.Contains(t, res.Warnings, `language profile "Picky": Bazarr numbers its items differently than BazarrConfig, `+
		`so applying the config would renumber them`)
}
