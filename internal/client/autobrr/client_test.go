package autobrr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	automationv1alpha1 "github.com/kyleseneker/media-operator/api/automation/v1alpha1"
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
		assert.Equal(t, "/api/downloaders", r.URL.Path)
		_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "qBit"}})
	})
	dcs, err := c.ListDownloadClients(context.Background())
	require.NoError(t, err)
	assert.Len(t, dcs, 1)
}

func TestCreateDownloadClient(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/downloaders", r.URL.Path)
		w.WriteHeader(http.StatusCreated)
	})
	assert.NoError(t, c.CreateDownloadClient(context.Background(), map[string]any{"name": "qBit"}))
}

// Autobrr updates a downloader at the collection path with the ID in the body.
func TestUpdateDownloadClient(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/api/downloaders", r.URL.Path)
		var body map[string]any
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, float64(5), body["id"])
		w.WriteHeader(http.StatusOK)
	})
	assert.NoError(t, c.UpdateDownloadClient(context.Background(), 5, map[string]any{"name": "qBit"}))
}

func TestUpdateIRCNetwork(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/api/irc/network/3", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	assert.NoError(t, c.UpdateIRCNetwork(context.Background(), 3, map[string]any{"name": "IPT"}))
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

func TestEveryFilterAndActionFieldReachesAutobrr(t *testing.T) {
	for _, tc := range []struct {
		typ     reflect.Type
		table   map[string]string
		special []string
	}{
		{reflect.TypeFor[automationv1alpha1.AutobrrFilter](), FilterFields, []string{"name", "enable", "tags", "exceptTags", "indexers", "actions"}},
		{reflect.TypeFor[automationv1alpha1.AutobrrFilterAction](), ActionFields, []string{"name", "enable", "type", "clientRef"}},
	} {
		for field := range tc.typ.Fields() {
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if slices.Contains(tc.special, name) {
				continue
			}
			assert.Contains(t, tc.table, name, "%s.%s has no Autobrr key, so Autobrr would never see it", tc.typ.Name(), name)
		}
		assert.Len(t, tc.table, tc.typ.NumField()-len(tc.special), "%s's table has an entry for a field it doesn't have", tc.typ.Name())
	}
}
