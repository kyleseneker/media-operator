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

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	hc, err := engine.NewHTTPClient(srv.URL, engine.AuthAPIKey,
		engine.WithAPIKey("test-key"),
		engine.WithAPIKeyHeader("x-api-key"),
		engine.WithTransport(&http.Transport{}),
	)
	require.NoError(t, err)
	return NewClient(hc)
}

func TestPing(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v2/status", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	assert.NoError(t, c.Ping(context.Background()))
}

func TestCrudDB(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v2/cruddb", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"_id": "lib1", "name": "Movies"})
	})
	result, err := c.CrudDB(context.Background(), "LibraryDB", "getById", "lib1", nil)
	require.NoError(t, err)
	assert.Equal(t, "lib1", result["_id"])
}

func TestGetByID(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"_id": "lib1"})
	})
	result, err := c.GetByID(context.Background(), "LibraryDB", "lib1")
	require.NoError(t, err)
	assert.Equal(t, "lib1", result["_id"])
}

func TestInsert(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})
	assert.NoError(t, c.Insert(context.Background(), "LibraryDB", "lib2", map[string]any{"name": "TV"}))
}

func TestUpsert_Exists(t *testing.T) {
	callCount := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// GetByID returns existing
			_ = json.NewEncoder(w).Encode(map[string]any{"_id": "lib1", "name": "Old"})
		} else {
			// Update
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	})
	assert.NoError(t, c.Upsert(context.Background(), "LibraryDB", "lib1", map[string]any{"name": "New"}))
	assert.Equal(t, 2, callCount)
}

func TestUpsert_NotExists(t *testing.T) {
	callCount := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// GetByID returns empty
			_ = json.NewEncoder(w).Encode(map[string]any{})
		} else {
			// Insert
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	})
	assert.NoError(t, c.Upsert(context.Background(), "LibraryDB", "lib1", map[string]any{"name": "New"}))
	assert.Equal(t, 2, callCount)
}

func TestGetNodes(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v2/get-nodes", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"node1": map[string]any{}})
	})
	nodes, err := c.GetNodes(context.Background())
	require.NoError(t, err)
	assert.Contains(t, nodes, "node1")
}

// insert, update and removeOne answer 200 with a zero-length body. Unmarshalling
// that unconditionally fails the reconcile for a write that already succeeded.
func TestCrudDBAcceptsEmptyResponseBody(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	res, err := c.CrudDB(context.Background(), "LibrarySettingsJSONDB", "insert", "movies", map[string]any{"name": "Movies"})
	require.NoError(t, err, "an empty 200 body must not fail the call")
	assert.Empty(t, res)
}

func TestStepWorkerLimitPayloadShape(t *testing.T) {
	var body map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	require.NoError(t, c.StepWorkerLimit(context.Background(), "N4DY4iKvA", "transcodegpu", "increase"))

	data, ok := body["data"].(map[string]any)
	require.True(t, ok, "Tdarr destructures body.data; a flat payload is rejected with 400, got %v", body)
	assert.Equal(t, "N4DY4iKvA", data["nodeID"])
	assert.Equal(t, "transcodegpu", data["workerType"])
	assert.Equal(t, "increase", data["process"])
	if _, bad := data["limit"]; bad {
		t.Error("Tdarr takes a direction, not an absolute limit")
	}
}
