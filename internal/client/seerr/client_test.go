package seerr

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
	hc, err := engine.NewHTTPClient(srv.URL, engine.AuthSession,
		engine.WithTransport(&http.Transport{}),
	)
	require.NoError(t, err)
	return srv, NewClient(hc)
}

func TestIsInitialized(t *testing.T) {
	tests := []struct {
		name string
		body map[string]interface{}
		want bool
	}{
		{"initialized", map[string]interface{}{"initialized": true}, true},
		{"not initialized", map[string]interface{}{"initialized": false}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(tt.body)
			})
			result, err := c.IsInitialized(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestAuthenticatePlex(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/auth/plex", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		w.WriteHeader(http.StatusOK)
	})
	assert.NoError(t, c.AuthenticatePlex(context.Background(), "plex-token"))
}

func TestAuthenticateJellyfin(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/auth/jellyfin", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	assert.NoError(t, c.AuthenticateJellyfin(context.Background(), "admin", "pass", "jf.local", 8096))
}

func TestGetAPIKey(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/settings/main", r.URL.Path)
		json.NewEncoder(w).Encode(map[string]interface{}{"apiKey": "seerr-key-123"})
	})
	key, err := c.GetAPIKey(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "seerr-key-123", key)
}

func TestGetAPIKey_Missing(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{})
	})
	_, err := c.GetAPIKey(context.Background())
	assert.Error(t, err)
}

func TestGet(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"key": "val"})
	})
	result, err := c.Get(context.Background(), "/api/v1/settings/main")
	require.NoError(t, err)
	assert.Equal(t, "val", result["key"])
}

func TestPost(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		json.NewEncoder(w).Encode(map[string]interface{}{"id": float64(1)})
	})
	result, err := c.Post(context.Background(), "/api/v1/resource", map[string]interface{}{"name": "test"})
	require.NoError(t, err)
	assert.Equal(t, float64(1), result["id"])
}

func TestPing(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/settings/public", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	assert.NoError(t, c.Ping(context.Background()))
}
