package export

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mediaserversv1alpha1 "github.com/kyleseneker/media-operator/api/mediaservers/v1alpha1"
)

func TestPlexExportsListedPreferencesAndLibraries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") != "plex-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/:/prefs":
			_, _ = w.Write([]byte(`{"MediaContainer":{"Setting":[
				{"id":"FriendlyName","value":"home"},{"id":"logDebug","value":false},
				{"id":"FSEventLibraryUpdatesEnabled","value":true},{"id":"TranscodeCountLimit","value":4},
				{"id":"allowedNetworks","value":""}]}}`))
		case "/library/sections":
			_, _ = w.Write([]byte(`{"MediaContainer":{"Directory":[{"title":"Movies","type":"movie","agent":"tv.plex.agents.movie",
				"scanner":"Plex Movie","language":"en-US","Location":[{"id":1,"path":"/media/movies"}]}]}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	res, err := Run(context.Background(), Options{App: "plex", URL: srv.URL, Token: "plex-token"})
	require.NoError(t, err)
	cfg, ok := res.Config.(*mediaserversv1alpha1.PlexConfig)
	require.True(t, ok)
	assert.Equal(t, "home", cfg.Spec.Server.FriendlyName)
	assert.False(t, *cfg.Spec.Server.LogDebug)
	assert.True(t, *cfg.Spec.Server.ScanMyLibraryAutomatically)
	assert.Equal(t, 4, *cfg.Spec.Transcoder.MaxSimultaneousVideoTranscodes)
	assert.Nil(t, cfg.Spec.Network, "every network preference is empty or unlisted")
	assert.Equal(t, []mediaserversv1alpha1.PlexLibrary{{
		Name: "Movies", Type: "movie", Paths: []string{"/media/movies"},
		Language: "en-US", Scanner: "Plex Movie", Agent: "tv.plex.agents.movie",
	}}, cfg.Spec.Libraries)
	assert.Equal(t, map[string]string{"token": "plex-token"}, res.Secret.StringData)
	assert.Contains(t, res.Unreadable, "network.secureConnections")
	assert.Contains(t, res.Warnings,
		"Plex doesn't list its secureConnections preference, so network.secureConnections isn't exported; set it by hand if you manage it")
	assert.NotContains(t, res.Unreadable, "server.friendlyName")
}
