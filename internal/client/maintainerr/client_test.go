package maintainerr

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
	hc, err := engine.NewHTTPClient(srv.URL, engine.AuthAPIKey,
		engine.WithAPIKey("test-key"),
		engine.WithTransport(&http.Transport{}),
	)
	require.NoError(t, err)
	return srv, NewClient(hc)
}

func TestPing(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/status", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	assert.NoError(t, c.Ping(context.Background()))
}

func TestGetSettings(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/settings", r.URL.Path)
		json.NewEncoder(w).Encode(map[string]interface{}{"autoClean": true})
	})
	s, err := c.GetSettings(context.Background())
	require.NoError(t, err)
	assert.Equal(t, true, s["autoClean"])
}

func TestUpdateSettings(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/api/settings", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	assert.NoError(t, c.UpdateSettings(context.Background(), map[string]interface{}{"autoClean": false}))
}

func TestUpdatePlexSettings(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/settings/plex", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	assert.NoError(t, c.UpdatePlexSettings(context.Background(), map[string]interface{}{}))
}

func TestUpdateSonarrSettings(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/settings/sonarr", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	assert.NoError(t, c.UpdateSonarrSettings(context.Background(), map[string]interface{}{}))
}

func TestListRules(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/rules", r.URL.Path)
		json.NewEncoder(w).Encode([]map[string]interface{}{{"name": "rule1"}})
	})
	rules, err := c.ListRules(context.Background())
	require.NoError(t, err)
	assert.Len(t, rules, 1)
}

func TestCreateRule(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/rules", r.URL.Path)
		w.WriteHeader(http.StatusCreated)
	})
	assert.NoError(t, c.CreateRule(context.Background(), map[string]interface{}{"name": "rule1"}))
}

func TestDeleteRule(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/api/rules/5", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	assert.NoError(t, c.DeleteRule(context.Background(), 5))
}
