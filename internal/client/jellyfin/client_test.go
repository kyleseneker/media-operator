package jellyfin

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
	hc, err := engine.NewHTTPClient(srv.URL, engine.AuthMediaBrowser,
		engine.WithTransport(&http.Transport{}),
	)
	require.NoError(t, err)
	return srv, NewClient(hc)
}

func TestIsSetupComplete(t *testing.T) {
	tests := []struct {
		name     string
		body     map[string]interface{}
		want     bool
	}{
		{"completed", map[string]interface{}{"StartupWizardCompleted": true}, true},
		{"not completed", map[string]interface{}{"StartupWizardCompleted": false}, false},
		{"missing field", map[string]interface{}{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/System/Info/Public", r.URL.Path)
				json.NewEncoder(w).Encode(tt.body)
			})
			result, err := c.IsSetupComplete(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestRunSetupWizard(t *testing.T) {
	var calls []string
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
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
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/Users/AuthenticateByName", r.URL.Path)
		json.NewEncoder(w).Encode(map[string]interface{}{"AccessToken": "test-token-123"})
	})
	err := c.Authenticate(context.Background(), "admin", "pass")
	assert.NoError(t, err)
}

func TestAuthenticate_NoToken(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{})
	})
	err := c.Authenticate(context.Background(), "admin", "pass")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no access token")
}

func TestGetConfig(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/System/Configuration/encoding", r.URL.Path)
		json.NewEncoder(w).Encode(map[string]interface{}{"EnableHardwareEncoding": true})
	})
	cfg, err := c.GetConfig(context.Background(), "/System/Configuration/encoding")
	require.NoError(t, err)
	assert.Equal(t, true, cfg["EnableHardwareEncoding"])
}

func TestListLibraries(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/Library/VirtualFolders", r.URL.Path)
		json.NewEncoder(w).Encode([]map[string]interface{}{
			{"Name": "Movies"}, {"Name": "TV"},
		})
	})
	libs, err := c.ListLibraries(context.Background())
	require.NoError(t, err)
	assert.Len(t, libs, 2)
}

func TestCreateLibrary(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Contains(t, r.URL.RawQuery, "name=Movies")
		assert.Contains(t, r.URL.RawQuery, "collectionType=movies")
		w.WriteHeader(http.StatusOK)
	})
	err := c.CreateLibrary(context.Background(), "Movies", "movies", map[string]interface{}{})
	assert.NoError(t, err)
}
