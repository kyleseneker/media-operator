package autobrr

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
	hc, err := engine.NewHTTPClient(srv.URL, engine.AuthAPIKey,
		engine.WithAPIKey("test-key"),
		engine.WithTransport(&http.Transport{}),
	)
	require.NoError(t, err)
	return NewClient(hc)
}

func TestPing(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/healthz/liveness", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	assert.NoError(t, c.Ping(context.Background()))
}

func TestListDownloadClients(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/download_clients", r.URL.Path)
		_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "qBit"}})
	})
	dcs, err := c.ListDownloadClients(context.Background())
	require.NoError(t, err)
	assert.Len(t, dcs, 1)
}

func TestCreateDownloadClient(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/download_clients", r.URL.Path)
		w.WriteHeader(http.StatusCreated)
	})
	assert.NoError(t, c.CreateDownloadClient(context.Background(), map[string]any{"name": "qBit"}))
}

func TestUpdateDownloadClient(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/api/download_clients/5", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	assert.NoError(t, c.UpdateDownloadClient(context.Background(), 5, map[string]any{"name": "qBit"}))
}

func TestListFilters(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/filters", r.URL.Path)
		_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "filter1"}})
	})
	filters, err := c.ListFilters(context.Background())
	require.NoError(t, err)
	assert.Len(t, filters, 1)
}

func TestCreateFilter(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": float64(1), "name": "new"})
	})
	result, err := c.CreateFilter(context.Background(), map[string]any{"name": "new"})
	require.NoError(t, err)
	assert.Equal(t, float64(1), result["id"])
}

func TestListIndexers(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/indexer", r.URL.Path)
		_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "idx1"}})
	})
	idxs, err := c.ListIndexers(context.Background())
	require.NoError(t, err)
	assert.Len(t, idxs, 1)
}

func TestListIRCNetworks(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/irc", r.URL.Path)
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	})
	nets, err := c.ListIRCNetworks(context.Background())
	require.NoError(t, err)
	assert.Empty(t, nets)
}
