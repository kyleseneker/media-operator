package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kyleseneker/media-operator/internal/metrics"
)

func TestReconcileSetting_NoChange(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id": 1, "rename": true, "createEmpty": false,
			})
		case http.MethodPut:
			t.Error("PUT should not be called when nothing changed")
		}
	}

	_, hc := newTestServer(t, handler)
	err := reconcileSetting(context.Background(), hc, "/api/v3/config/naming", map[string]interface{}{
		"rename": true, "createEmpty": false,
	})
	assert.NoError(t, err)
}

func TestReconcileSetting_WithChange(t *testing.T) {
	var putCalled bool
	var putBody map[string]interface{}

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id": 1, "rename": false, "extra": "preserved",
			})
		case http.MethodPut:
			putCalled = true
			json.NewDecoder(r.Body).Decode(&putBody)
			assert.True(t, strings.HasSuffix(r.URL.Path, "/1"), "PUT path should include id")
			w.WriteHeader(http.StatusOK)
		}
	}

	_, hc := newTestServer(t, handler)
	err := reconcileSetting(context.Background(), hc, "/api/v3/config/naming", map[string]interface{}{
		"rename": true,
	})
	require.NoError(t, err)
	assert.True(t, putCalled)
	assert.Equal(t, true, putBody["rename"])
	assert.Equal(t, "preserved", putBody["extra"])
}

func TestReconcileSetting_NoID(t *testing.T) {
	_, hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"rename": true})
	})
	err := reconcileSetting(context.Background(), hc, "/api/v3/config/naming", map[string]interface{}{"rename": true})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no id")
}

func TestReconcileResource_CreateNew(t *testing.T) {
	var postCalled bool

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode([]map[string]interface{}{})
		case http.MethodPost:
			postCalled = true
			w.WriteHeader(http.StatusCreated)
		}
	}

	_, hc := newTestServer(t, handler)
	endpoint := ResourceEndpoint{Name: "tags", Path: "/api/v3/tag", MatchField: "label", Policy: CreateOrUpdate}
	err := reconcileResource(context.Background(), hc, endpoint, map[string]interface{}{"label": "test"})
	require.NoError(t, err)
	assert.True(t, postCalled)
}

func TestReconcileResource_UpdateExisting(t *testing.T) {
	var putCalled bool

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": float64(5), "name": "test-dc", "host": "old-host"},
			})
		case http.MethodPut:
			putCalled = true
			assert.True(t, strings.HasSuffix(r.URL.Path, "/5"))
			w.WriteHeader(http.StatusOK)
		}
	}

	_, hc := newTestServer(t, handler)
	endpoint := ResourceEndpoint{Name: "downloadClients", Path: "/api/v3/downloadclient", MatchField: "name", Policy: CreateOrUpdate}
	err := reconcileResource(context.Background(), hc, endpoint, map[string]interface{}{
		"name": "test-dc", "host": "new-host",
	})
	require.NoError(t, err)
	assert.True(t, putCalled)
}

func TestReconcileResource_CreateOnly(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": float64(1), "path": "/tv"},
			})
		case http.MethodPut:
			t.Error("PUT should not be called for CreateOnly policy")
		case http.MethodPost:
			t.Error("POST should not be called when resource exists with CreateOnly")
		}
	}

	_, hc := newTestServer(t, handler)
	endpoint := ResourceEndpoint{Name: "rootFolders", Path: "/api/v3/rootfolder", MatchField: "path", Policy: CreateOnly}
	err := reconcileResource(context.Background(), hc, endpoint, map[string]interface{}{"path": "/tv"})
	assert.NoError(t, err)
}

func TestPruneResources(t *testing.T) {
	var deletedPaths []string

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": float64(1), "name": "managed"},
				{"id": float64(2), "name": "unmanaged-1"},
				{"id": float64(3), "name": "unmanaged-2"},
			})
		case http.MethodDelete:
			deletedPaths = append(deletedPaths, r.URL.Path)
			w.WriteHeader(http.StatusOK)
		}
	}

	_, hc := newTestServer(t, handler)
	endpoint := ResourceEndpoint{Name: "downloadClients", Path: "/api/v3/downloadclient", MatchField: "name", Policy: CreateOrUpdate, Prunable: true}
	desired := []map[string]interface{}{{"name": "managed"}}

	before := testutil.ToFloat64(metrics.ResourcesPrunedTotal.WithLabelValues(testAppLabel, "downloadClients"))
	pruned, err := pruneResources(context.Background(), hc, endpoint, desired)
	require.NoError(t, err)
	assert.Len(t, pruned, 2)
	assert.Contains(t, deletedPaths, "/api/v3/downloadclient/2")
	assert.Contains(t, deletedPaths, "/api/v3/downloadclient/3")

	after := testutil.ToFloat64(metrics.ResourcesPrunedTotal.WithLabelValues(testAppLabel, "downloadClients"))
	assert.Equal(t, before+2, after, "expected prune counter to increment by 2")
}

func TestPruneResources_SafetyThreshold(t *testing.T) {
	// Create more than DefaultMaxPruneCount (25) unmanaged resources
	existing := make([]map[string]interface{}, 30)
	for i := range existing {
		existing[i] = map[string]interface{}{"id": float64(i + 1), "name": fmt.Sprintf("item-%d", i+1)}
	}

	handler := func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(existing)
		case http.MethodDelete:
			t.Error("DELETE should not be called when safety threshold exceeded")
		}
	}

	_, hc := newTestServer(t, handler)
	endpoint := ResourceEndpoint{Name: "indexers", Path: "/api/v3/indexer", MatchField: "name", Prunable: true}
	desired := []map[string]interface{}{} // No desired items, all 30 would be pruned

	before := testutil.ToFloat64(metrics.ResourcesPrunedTotal.WithLabelValues(testAppLabel, "indexers"))
	_, err := pruneResources(context.Background(), hc, endpoint, desired)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to prune")
	assert.Contains(t, err.Error(), "30")

	after := testutil.ToFloat64(metrics.ResourcesPrunedTotal.WithLabelValues(testAppLabel, "indexers"))
	assert.Equal(t, before, after, "prune counter must NOT increment when safety threshold blocks deletion")
}

func TestReconcileApp(t *testing.T) {
	callLog := make(map[string]int)

	handler := func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		callLog[key]++

		switch {
		// Settings: naming
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/config/naming":
			json.NewEncoder(w).Encode(map[string]interface{}{"id": float64(1), "rename": false})
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/v3/config/naming/"):
			w.WriteHeader(http.StatusOK)

		// Resources: tags
		case r.Method == http.MethodGet && r.URL.Path == "/api/v3/tag":
			json.NewEncoder(w).Encode([]map[string]interface{}{})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v3/tag":
			w.WriteHeader(http.StatusCreated)

		default:
			w.WriteHeader(http.StatusOK)
		}
	}

	_, hc := newTestServer(t, handler)

	def := AppDefinition{
		Settings: []SettingEndpoint{
			{Name: "naming", Path: "/api/v3/config/naming"},
		},
		Resources: []ResourceEndpoint{
			{Name: "tags", Path: "/api/v3/tag", MatchField: "label", Policy: CreateOrUpdate},
		},
	}
	sections := map[string]interface{}{
		"naming": map[string]interface{}{"rename": true},
	}
	resources := map[string][]map[string]interface{}{
		"tags": {{"label": "test"}},
	}

	result := ReconcileApp(context.Background(), hc, def, sections, resources, false)
	assert.True(t, result.Success())
	assert.Contains(t, result.Synced, "naming")
	assert.Contains(t, result.Synced, "tags(test)")
}

func TestReconcileApp_SkipsNilSections(t *testing.T) {
	_, hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no HTTP calls expected, got %s %s", r.Method, r.URL.Path)
	})

	def := AppDefinition{
		Settings: []SettingEndpoint{{Name: "naming", Path: "/api/v3/config/naming"}},
	}
	result := ReconcileApp(context.Background(), hc, def, map[string]interface{}{}, nil, false)
	assert.True(t, result.Success())
	assert.Empty(t, result.Synced)
}
