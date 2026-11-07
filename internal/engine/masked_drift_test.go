package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kyleseneker/media-operator/internal/metrics"
)

// maskingServer mimics the apps: it stores whatever is written but always
// reports the secret field back as "********".
func maskingServer(t *testing.T, name string, writes *[]string) *HTTPClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := map[string]any{
			"id": float64(1), "name": name, "enable": true,
			"fields": []any{
				map[string]any{"name": "host", "value": "qbit.svc"},
				map[string]any{"name": "password", "value": "********"},
			},
		}
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode([]map[string]any{body})
			return
		}
		*writes = append(*writes, r.Method+" "+r.URL.Path)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)

	hc, err := NewHTTPClient(srv.URL, AuthAPIKey, WithAPIKey("k"),
		WithAPIKeyHeader("X-Api-Key"), WithAppLabel("sonarr"),
		WithTransport(&http.Transport{}))
	require.NoError(t, err, "building client")
	return hc
}

func desiredWithPassword(name, pw string) map[string]any {
	return map[string]any{
		"name": name, "enable": true,
		"fields": []any{
			map[string]any{"name": "host", "value": "qbit.svc"},
			map[string]any{"name": "password", "value": pw},
		},
	}
}

// The regression behind MediaOperatorDriftNeverSettles: the app masks the
// password on read, so the operator rewrote it on every pass and the write
// never settled. It must seed the secret once and then go quiet.
func TestMaskedSecretSettlesAfterOneWrite(t *testing.T) {
	metrics.DriftCorrectedTotal.Reset()
	var writes []string
	hc := maskingServer(t, "settles", &writes)
	ep := ResourceEndpoint{Name: "downloadClients", Path: "/api/v3/downloadclient", MatchField: "name", Policy: CreateOrUpdate}

	for i := range 5 {
		require.NoError(t, reconcileResource(context.Background(), hc, ep, desiredWithPassword("settles", "pw-v1"), false), "reconcile %d", i)
	}

	assert.Len(t, writes, 1, "the masked secret is still looping")
}

// A rotated secret is invisible in the fetched state, so it has to be pushed
// on the strength of the remembered digest alone.
func TestRotatedSecretIsWritten(t *testing.T) {
	metrics.DriftCorrectedTotal.Reset()
	var writes []string
	hc := maskingServer(t, "rotates", &writes)
	ep := ResourceEndpoint{Name: "downloadClients", Path: "/api/v3/downloadclient", MatchField: "name", Policy: CreateOrUpdate}

	ctx := context.Background()
	for range 3 {
		require.NoError(t, reconcileResource(ctx, hc, ep, desiredWithPassword("rotates", "pw-v1"), false), "reconcile")
	}
	require.Len(t, writes, 1, "before rotation")

	for range 3 {
		require.NoError(t, reconcileResource(ctx, hc, ep, desiredWithPassword("rotates", "pw-v2"), false), "reconcile after rotation")
	}
	assert.Len(t, writes, 2, "after rotation")
}

// Observe must not seed the secret, so a CR being measured stays read-only.
func TestObserveDoesNotRecordMaskedSecret(t *testing.T) {
	metrics.DriftCorrectedTotal.Reset()
	var writes []string
	hc := maskingServer(t, "observed", &writes)
	ep := ResourceEndpoint{Name: "downloadClients", Path: "/api/v3/downloadclient", MatchField: "name", Policy: CreateOrUpdate}

	for range 3 {
		require.NoError(t, reconcileResource(context.Background(), hc, ep, desiredWithPassword("observed", "pw-v1"), true), "reconcile")
	}
	assert.Empty(t, writes, "observe wrote to the app")
}

// Prowlarr was the worst offender at ten writes a minute. Its applications
// carry the key inside fields[], and the live resource holds fields the CR
// never declares, so the merge has to line up before the mask is even reached.
func TestProwlarrApplicationSettles(t *testing.T) {
	metrics.DriftCorrectedTotal.Reset()
	var writes []string
	live := map[string]any{
		"id": float64(1), "name": "Sonarr", "syncLevel": "fullSync",
		"implementation": "Sonarr", "configContract": "SonarrSettings",
		"fields": []any{
			map[string]any{"name": "prowlarrUrl", "value": "http://prowlarr:9696"},
			map[string]any{"name": "baseUrl", "value": "http://sonarr:8989"},
			map[string]any{"name": "apiKey", "value": "********"},
			map[string]any{"name": "authUsername", "value": nil},
			map[string]any{"name": "authPassword", "value": nil},
			map[string]any{"name": "syncCategories", "value": []any{5000, 5010}},
			map[string]any{"name": "animeSyncCategories", "value": []any{5070}},
			map[string]any{"name": "syncRejectBlocklistedTorrentHashesWhileGrabbing", "value": false},
		},
		"tags": []any{},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode([]map[string]any{live})
			return
		}
		writes = append(writes, r.Method+" "+r.URL.Path)
		_ = json.NewEncoder(w).Encode(live)
	}))
	t.Cleanup(srv.Close)

	hc, err := NewHTTPClient(srv.URL, AuthAPIKey, WithAPIKey("k"),
		WithAPIKeyHeader("X-Api-Key"), WithAppLabel("prowlarr"),
		WithTransport(&http.Transport{}))
	require.NoError(t, err, "building client")

	ep := ResourceEndpoint{Name: "applications", Path: "/api/v1/applications", MatchField: "name", Policy: CreateOrUpdate}
	desired := map[string]any{
		"name": "Sonarr", "syncLevel": "fullSync",
		"implementation": "Sonarr", "configContract": "SonarrSettings",
		"fields": []any{
			map[string]any{"name": "prowlarrUrl", "value": "http://prowlarr:9696"},
			map[string]any{"name": "baseUrl", "value": "http://sonarr:8989"},
			map[string]any{"name": "apiKey", "value": "real-key"},
			map[string]any{"name": "syncCategories", "value": []any{5000, 5010}},
		},
		"tags": []any{},
	}

	for i := range 5 {
		require.NoError(t, reconcileResource(context.Background(), hc, ep, desired, false), "reconcile %d", i)
	}
	assert.Len(t, writes, 1)
}

// A resource created with a secret comes back masked. Without remembering what
// the create wrote, the next pass rewrote every new download client and indexer.
func TestCreatedSecretIsNotRewrittenOnTheNextPass(t *testing.T) {
	var writes []string
	created := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := map[string]any{
			"id": float64(1), "name": "created", "enable": true,
			"fields": []any{
				map[string]any{"name": "host", "value": "qbit.svc"},
				map[string]any{"name": "password", "value": "********"},
			},
		}
		if r.Method == http.MethodGet {
			if !created {
				_ = json.NewEncoder(w).Encode([]map[string]any{})
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{body})
			return
		}
		writes = append(writes, r.Method)
		created = true
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	hc, err := NewHTTPClient(srv.URL, AuthAPIKey, WithAPIKey("k"), WithAppLabel("sonarr"),
		WithOwner("default/created/uid"), WithTransport(&http.Transport{}))
	require.NoError(t, err)
	ep := ResourceEndpoint{Name: "downloadClients", Path: "/api/v3/downloadclient", MatchField: "name", Policy: CreateOrUpdate}

	for i := range 3 {
		require.NoError(t, reconcileResource(context.Background(), hc, ep, desiredWithPassword("created", "pw"), false), "reconcile %d", i)
	}
	assert.Equal(t, []string{http.MethodPost}, writes, "want only the create")
}
