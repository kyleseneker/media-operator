package tdarr

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
		engine.WithAPIKeyHeader("x-api-key"),
		engine.WithTransport(&http.Transport{}),
	)
	require.NoError(t, err)
	return srv, NewClient(hc)
}

func TestPing(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v2/status", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	assert.NoError(t, c.Ping(context.Background()))
}

func TestCrudDB(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v2/cruddb", r.URL.Path)
		json.NewEncoder(w).Encode(map[string]interface{}{"_id": "lib1", "name": "Movies"})
	})
	result, err := c.CrudDB(context.Background(), "LibraryDB", "getById", "lib1", nil)
	require.NoError(t, err)
	assert.Equal(t, "lib1", result["_id"])
}

func TestGetByID(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"_id": "lib1"})
	})
	result, err := c.GetByID(context.Background(), "LibraryDB", "lib1")
	require.NoError(t, err)
	assert.Equal(t, "lib1", result["_id"])
}

func TestInsert(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	})
	assert.NoError(t, c.Insert(context.Background(), "LibraryDB", "lib2", map[string]interface{}{"name": "TV"}))
}

func TestUpsert_Exists(t *testing.T) {
	callCount := 0
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// GetByID returns existing
			json.NewEncoder(w).Encode(map[string]interface{}{"_id": "lib1", "name": "Old"})
		} else {
			// Update
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		}
	})
	assert.NoError(t, c.Upsert(context.Background(), "LibraryDB", "lib1", map[string]interface{}{"name": "New"}))
	assert.Equal(t, 2, callCount)
}

func TestUpsert_NotExists(t *testing.T) {
	callCount := 0
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// GetByID returns empty
			json.NewEncoder(w).Encode(map[string]interface{}{})
		} else {
			// Insert
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		}
	})
	assert.NoError(t, c.Upsert(context.Background(), "LibraryDB", "lib1", map[string]interface{}{"name": "New"}))
	assert.Equal(t, 2, callCount)
}

func TestGetNodes(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v2/get-nodes", r.URL.Path)
		json.NewEncoder(w).Encode(map[string]interface{}{"node1": map[string]interface{}{}})
	})
	nodes, err := c.GetNodes(context.Background())
	require.NoError(t, err)
	assert.Contains(t, nodes, "node1")
}
