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

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	hc, err := engine.NewHTTPClient(srv.URL, engine.AuthNone, engine.WithTransport(&http.Transport{}))
	require.NoError(t, err)
	return NewClient(hc)
}

// /api/status does not exist in Maintainerr 3; the readiness endpoint also
// checks the database.
func TestPingUsesReadinessEndpoint(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/health/ready", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	})
	assert.NoError(t, c.Ping(context.Background()))
}

// Settings endpoints answer 200 or 201 when they refuse a write; the refusal is
// only in the body.
func TestSettingsWriteFailsOnNOKBody(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "NOK", "code": 0, "message": "Authenticate with Plex before saving Plex server settings",
		})
	})
	err := c.UpdateSettings(context.Background(), map[string]any{"plex_hostname": "plex"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Authenticate with Plex")
}

func TestSettingsWriteSucceedsOnOKBody(t *testing.T) {
	var body map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/settings/seerr", r.URL.Path)
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "OK", "code": 1, "message": "Success"})
	})
	require.NoError(t, c.SaveService(context.Background(), Seerr, map[string]any{"url": "http://seerr:5055", "api_key": "k"}))
	assert.Equal(t, "http://seerr:5055", body["url"])
}

func TestAddArrPostsToTheServerList(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/settings/radarr", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "OK", "code": 1, "data": map[string]any{"id": 3}})
	})
	assert.NoError(t, c.AddArr(context.Background(), Radarr, map[string]any{"serverName": "main"}))
}

func TestUpdateArrPutsByID(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/api/settings/sonarr/4", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "OK", "code": 1})
	})
	assert.NoError(t, c.UpdateArr(context.Background(), Sonarr, 4, map[string]any{"serverName": "main"}))
}

func TestSetMediaServerTypeSendsNoRuleMigration(t *testing.T) {
	var body map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/settings/media-server/switch", r.URL.Path)
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "OK", "code": 1})
	})
	require.NoError(t, c.SetMediaServerType(context.Background(), "plex"))
	assert.Equal(t, map[string]any{"targetServerType": "plex", "migrateRules": false}, body)
}

// Rule group saves fail with real HTTP errors, which carry Maintainerr's reason.
func TestCreateRuleGroupReportsRejection(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/rules", r.URL.Path)
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "First value is not available"})
	})
	err := c.CreateRuleGroup(context.Background(), map[string]any{"name": "g"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "First value is not available")
}

func TestUpdateRuleGroupPutsWithoutID(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/api/rules", r.URL.Path, "the id travels in the body")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 1, "result": "Success"})
	})
	assert.NoError(t, c.UpdateRuleGroup(context.Background(), map[string]any{"id": 2}))
}

func TestQualityProfilesPath(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/servarr/radarr/3/profiles", r.URL.Path)
		_ = json.NewEncoder(w).Encode([]map[string]any{{"id": 1, "name": "HD-1080p"}})
	})
	profiles, err := c.QualityProfiles(context.Background(), Radarr, 3)
	require.NoError(t, err)
	assert.Equal(t, "HD-1080p", profiles[0]["name"])
}
