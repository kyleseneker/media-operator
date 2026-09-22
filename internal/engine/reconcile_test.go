package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReconcileResult_Success(t *testing.T) {
	tests := []struct {
		name   string
		result ReconcileResult
		want   bool
	}{
		{"no errors", ReconcileResult{Synced: []string{"a"}}, true},
		{"empty", ReconcileResult{}, true},
		{"with errors", ReconcileResult{Errors: []string{"fail"}}, false},
		{"synced and errors", ReconcileResult{Synced: []string{"a"}, Errors: []string{"b"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.result.Success())
		})
	}
}

func TestReconcileResult_Message(t *testing.T) {
	tests := []struct {
		name   string
		result ReconcileResult
		want   string
	}{
		{
			name:   "all synced",
			result: ReconcileResult{Synced: []string{"mediaManagement", "naming"}},
			want:   "synced: [mediaManagement naming]",
		},
		{
			name:   "nothing synced no errors",
			result: ReconcileResult{},
			want:   "all configuration sections synced",
		},
		{
			name:   "errors only",
			result: ReconcileResult{Errors: []string{"naming: failed"}},
			want:   "errors: [naming: failed]",
		},
		{
			name:   "synced and errors",
			result: ReconcileResult{Synced: []string{"ui"}, Errors: []string{"naming: fail"}},
			want:   "synced: [ui]; errors: [naming: fail]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.result.Message())
		})
	}
}

func TestIsNilInterface(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want bool
	}{
		{"nil", nil, true},
		{"typed nil pointer", (*string)(nil), true},
		{"non-nil pointer", ptrTo("hello"), false},
		{"non-pointer value", "hello", false},
		{"integer", 42, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isNilInterface(tt.val))
		})
	}
}

func ptrTo[T any](v T) *T { return &v }

func TestFailedUpdateDoesNotPruneOrForgetOwnership(t *testing.T) {
	var deletes int
	hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 1, "name": "desired", "enabled": false}, {"id": 2, "name": "removed"}})
		case http.MethodPut:
			w.WriteHeader(http.StatusInternalServerError)
		case http.MethodDelete:
			deletes++
		}
	})
	previous := map[string][]string{"indexers": {"desired", "removed"}, "tags": {"retained"}}
	result := ReconcileApp(context.Background(), hc, ownershipDefinition(), nil,
		map[string][]map[string]any{"indexers": {{"name": "desired", "enabled": true}}}, SyncPolicy{Prune: true}, previous)
	require.False(t, result.Success())
	assert.Zero(t, deletes, "a failed apply must block pruning for the resource type")
	assert.Equal(t, previous, result.Managed, "failed writes and omitted sections must retain ownership")
}

func TestExistingResourcesAreNotAdoptedForPruning(t *testing.T) {
	for _, observe := range []bool{false, true} {
		for _, existing := range []bool{false, true} {
			t.Run(strconv.FormatBool(observe)+"/"+strconv.FormatBool(existing), func(t *testing.T) {
				var posts int
				hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodPost {
						posts++
						return
					}
					items := []map[string]any{}
					if existing {
						items = append(items, map[string]any{"id": 1, "name": "manual"})
					}
					_ = json.NewEncoder(w).Encode(items)
				})
				result := ReconcileApp(context.Background(), hc, ownershipDefinition(), nil,
					map[string][]map[string]any{"indexers": {{"name": "manual"}}}, SyncPolicy{Observe: observe}, nil)
				require.True(t, result.Success())
				if !observe && !existing {
					assert.Equal(t, 1, posts)
					assert.Equal(t, []string{"manual"}, result.Managed["indexers"])
				} else {
					assert.Zero(t, posts)
					assert.Empty(t, result.Managed["indexers"])
				}
			})
		}
	}
}

func TestPruneRetainsFailedDeletesForRetry(t *testing.T) {
	hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			if r.URL.Path == "/indexers/3" {
				w.WriteHeader(http.StatusInternalServerError)
			}
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 1, "name": "desired"}, {"id": 2, "name": "deleted"}, {"id": 3, "name": "retry"}})
	})
	previous := map[string][]string{"indexers": {"desired", "deleted", "retry"}}
	result := ReconcileApp(context.Background(), hc, ownershipDefinition(), nil,
		map[string][]map[string]any{"indexers": {{"name": "desired"}}}, SyncPolicy{Prune: true}, previous)
	require.False(t, result.Success())
	assert.Equal(t, []string{"desired", "retry"}, result.Managed["indexers"])
	assert.Equal(t, []string{"desired", "deleted", "retry"}, previous["indexers"], "input status must not be mutated")
	require.Len(t, result.Pruned, 1)
	assert.Equal(t, "deleted", result.Pruned[0].Name)
}

func ownershipDefinition() AppDefinition {
	return AppDefinition{Resources: []ResourceEndpoint{{Name: "indexers", Path: "/indexers", MatchField: "name", Policy: CreateOrUpdate, Prunable: true}}}
}

func TestMaskedSecretsAreIsolatedBetweenInstances(t *testing.T) {
	for _, sameSecret := range []bool{true, false} {
		t.Run(fmt.Sprint(sameSecret), func(t *testing.T) {
			var writesA, writesB []string
			a := maskingServer(t, "shared-name", &writesA)
			b := maskingServer(t, "shared-name", &writesB)
			ep := ResourceEndpoint{Name: "downloadClients", Path: "/api/v3/downloadclient", MatchField: "name", Policy: CreateOrUpdate}
			passwordB := "password-a"
			if !sameSecret {
				passwordB = "password-b"
			}
			for range 3 {
				require.NoError(t, reconcileResource(context.Background(), a, ep, desiredWithPassword("shared-name", "password-a"), false))
				require.NoError(t, reconcileResource(context.Background(), b, ep, desiredWithPassword("shared-name", passwordB), false))
			}
			assert.Len(t, writesA, 1)
			assert.Len(t, writesB, 1, "each instance must receive exactly one initial write")
		})
	}
}

func TestMaskedSecretCacheIncludesOwner(t *testing.T) {
	var writes []string
	hc := maskingServer(t, "recreated", &writes)
	ep := ResourceEndpoint{Name: "downloadClients", Path: "/api/v3/downloadclient", MatchField: "name", Policy: CreateOrUpdate}
	for _, owner := range []string{"ns/config/uid-1", "ns/config/uid-1", "ns/config/uid-2"} {
		WithOwner(owner)(hc)
		require.NoError(t, reconcileResource(context.Background(), hc, ep, desiredWithPassword("recreated", "password"), false))
	}
	assert.Len(t, writes, 2, "a recreated CR must seed its secret independently")
}

func TestObserveDifference(t *testing.T) {
	for _, tt := range []struct {
		name    string
		current map[string]any
		desired any
		drift   bool
		fails   bool
	}{
		{"missing", nil, map[string]any{}, true, false},
		{"matching", map[string]any{"enabled": false, "other": "preserved"}, map[string]any{"enabled": false}, false, false},
		{"changed", map[string]any{"enabled": false}, map[string]any{"enabled": true}, true, false},
		{"masked", map[string]any{"password": "********"}, map[string]any{"password": "secret"}, false, false},
		{"invalid", map[string]any{}, make(chan int), false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			drift, err := ObserveDifference("test-observe", "settings", tt.name, tt.current, tt.desired)
			assert.Equal(t, tt.drift, drift)
			assert.Equal(t, tt.fails, err != nil)
		})
	}
}
