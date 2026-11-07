package plex

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kyleseneker/media-operator/internal/engine"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	hc, err := engine.NewHTTPClient(srv.URL, engine.AuthPlexToken,
		engine.WithPlexToken("test-token"),
		engine.WithTransport(&http.Transport{}),
	)
	require.NoError(t, err)
	return NewClient(hc)
}

func TestPing(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	assert.NoError(t, c.Ping(context.Background()))
}

func TestGetPreferences(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/:/prefs", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"FriendlyName": "Plex"})
	})
	prefs, err := c.GetPreferences(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "Plex", prefs["FriendlyName"])
}

func TestSetPreferences(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Contains(t, r.URL.RawQuery, "FriendlyName=MyPlex")
		w.WriteHeader(http.StatusOK)
	})
	err := c.SetPreferences(context.Background(), map[string]string{"FriendlyName": "MyPlex"})
	assert.NoError(t, err)
}

func TestListLibraries(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/library/sections", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"MediaContainer": map[string]any{
				"Directory": []any{
					map[string]any{"title": "Movies", "type": "movie"},
					map[string]any{"title": "TV", "type": "show"},
				},
			},
		})
	})
	libs, err := c.ListLibraries(context.Background())
	require.NoError(t, err)
	assert.Len(t, libs, 2)
	assert.Equal(t, "Movies", libs[0]["title"])
}

func TestCreateLibrary(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.RawQuery, "name=Movies")
		assert.Contains(t, r.URL.RawQuery, "type=movie")
		w.WriteHeader(http.StatusCreated)
	})
	err := c.CreateLibrary(context.Background(), "Movies", "movie", "com.plexapp.agents.imdb", "Plex Movie Scanner", "en", []string{"/media/movies"})
	assert.NoError(t, err)
}

func TestGetSettingsRendersValuesAsPlexAcceptsThem(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"MediaContainer":{"Setting":[` +
			`{"id":"FriendlyName","value":"plex"},{"id":"autoEmptyTrash","value":true},{"id":"TranscodeCountLimit","value":2}]}}`))
	})
	settings, err := c.GetSettings(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"FriendlyName": "plex", "autoEmptyTrash": "1", "TranscodeCountLimit": "2"}, settings)
}

// A Plex without libraries leaves Directory out; that is an empty list.
func TestListLibrariesOnAFreshServer(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"MediaContainer":{"size":0}}`))
	})
	libs, err := c.ListLibraries(context.Background())
	require.NoError(t, err)
	assert.Empty(t, libs)
}

func TestCreateLibrarySendsOneLocationPerFolder(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, []string{"/media/movies", "/media/more"}, r.URL.Query()["location"])
		assert.Equal(t, "en-US", r.URL.Query().Get("language"))
		w.WriteHeader(http.StatusCreated)
	})
	require.NoError(t, c.CreateLibrary(context.Background(), "Movies", "movie", "tv.plex.agents.movie", "Plex Movie", "en-US",
		[]string{"/media/movies", "/media/more"}))
}
