package jellyfin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kyleseneker/media-operator/internal/engine"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	hc, err := engine.NewHTTPClient(srv.URL, engine.AuthMediaBrowser,
		engine.WithTransport(&http.Transport{}),
	)
	require.NoError(t, err)
	return NewClient(hc)
}

func TestIsSetupComplete(t *testing.T) {
	tests := []struct {
		name string
		body map[string]any
		want bool
	}{
		{"completed", map[string]any{"StartupWizardCompleted": true}, true},
		{"not completed", map[string]any{"StartupWizardCompleted": false}, false},
		{"missing field", map[string]any{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/System/Info/Public", r.URL.Path)
				_ = json.NewEncoder(w).Encode(tt.body)
			})
			result, err := c.IsSetupComplete(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestRunSetupWizard(t *testing.T) {
	var calls []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	err := c.RunSetupWizard(context.Background(), "admin", "pass", "Jellyfin", "en", "US")
	require.NoError(t, err)
	assert.Equal(t, []string{
		"/Startup/User",
		"/Startup/Configuration",
		"/Startup/RemoteAccess",
		"/Startup/Complete",
	}, calls)
}

func TestAuthenticate(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/Users/AuthenticateByName", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"AccessToken": "test-token-123"})
	})
	err := c.Authenticate(context.Background(), "admin", "pass")
	assert.NoError(t, err)
}

func TestAuthenticate_NoToken(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{})
	})
	err := c.Authenticate(context.Background(), "admin", "pass")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no access token")
}

func TestGetConfig(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/System/Configuration/encoding", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"EnableHardwareEncoding": true})
	})
	cfg, err := c.GetConfig(context.Background(), "/System/Configuration/encoding")
	require.NoError(t, err)
	assert.Equal(t, true, cfg["EnableHardwareEncoding"])
}

func TestListLibraries(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/Library/VirtualFolders", r.URL.Path)
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"Name": "Movies"}, {"Name": "TV"},
		})
	})
	libs, err := c.ListLibraries(context.Background())
	require.NoError(t, err)
	assert.Len(t, libs, 2)
}

func TestCreateLibrary(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.RawQuery, "name=Movies")
		assert.Contains(t, r.URL.RawQuery, "collectionType=movies")
		w.WriteHeader(http.StatusOK)
	})
	err := c.CreateLibrary(context.Background(), "Movies", "movies", map[string]any{})
	assert.NoError(t, err)
}

func TestCreateLibraryNestsOptionsUnderLibraryOptions(t *testing.T) {
	var body map[string]any
	var query url.Values
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusNoContent)
	})

	opts := map[string]any{"PathInfos": []map[string]any{{"Path": "/data/media/movies"}}}
	require.NoError(t, c.CreateLibrary(context.Background(), "Movies", "movies", opts))

	assert.Equal(t, "Movies", query.Get("name"))
	assert.Equal(t, "movies", query.Get("collectionType"))

	lo, ok := body["LibraryOptions"].(map[string]any)
	require.True(t, ok, "options must be nested under LibraryOptions, got %v", body)
	infos, ok := lo["PathInfos"].([]any)
	require.True(t, ok, "PathInfos missing from LibraryOptions")
	require.Len(t, infos, 1)
	assert.Equal(t, "/data/media/movies", infos[0].(map[string]any)["Path"])
}
