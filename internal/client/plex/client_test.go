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

func newTestClient(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	hc, err := engine.NewHTTPClient(srv.URL, engine.AuthPlexToken,
		engine.WithPlexToken("test-token"),
		engine.WithTransport(&http.Transport{}),
	)
	require.NoError(t, err)
	return srv, NewClient(hc)
}

func TestPing(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	assert.NoError(t, c.Ping(context.Background()))
}

func TestGetPreferences(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/:/prefs", r.URL.Path)
		json.NewEncoder(w).Encode(map[string]interface{}{"FriendlyName": "Plex"})
	})
	prefs, err := c.GetPreferences(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "Plex", prefs["FriendlyName"])
}

func TestSetPreferences(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Contains(t, r.URL.RawQuery, "FriendlyName=MyPlex")
		w.WriteHeader(http.StatusOK)
	})
	err := c.SetPreferences(context.Background(), map[string]string{"FriendlyName": "MyPlex"})
	assert.NoError(t, err)
}

func TestListLibraries(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/library/sections", r.URL.Path)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"MediaContainer": map[string]interface{}{
				"Directory": []interface{}{
					map[string]interface{}{"title": "Movies", "type": "movie"},
					map[string]interface{}{"title": "TV", "type": "show"},
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
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.RawQuery, "name=Movies")
		assert.Contains(t, r.URL.RawQuery, "type=movie")
		w.WriteHeader(http.StatusCreated)
	})
	err := c.CreateLibrary(context.Background(), "Movies", "movie", "com.plexapp.agents.imdb", "Plex Movie Scanner", "en", []string{"/media/movies"})
	assert.NoError(t, err)
}
