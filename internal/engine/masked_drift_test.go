package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
	if err != nil {
		t.Fatalf("building client: %v", err)
	}
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
		if err := reconcileResource(context.Background(), hc, ep, desiredWithPassword("settles", "pw-v1"), false); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}

	if len(writes) != 1 {
		t.Errorf("wrote %d times, want 1 — the masked secret is still looping: %v", len(writes), writes)
	}
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
		if err := reconcileResource(ctx, hc, ep, desiredWithPassword("rotates", "pw-v1"), false); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
	}
	if len(writes) != 1 {
		t.Fatalf("before rotation: wrote %d times, want 1", len(writes))
	}

	for range 3 {
		if err := reconcileResource(ctx, hc, ep, desiredWithPassword("rotates", "pw-v2"), false); err != nil {
			t.Fatalf("reconcile after rotation: %v", err)
		}
	}
	if len(writes) != 2 {
		t.Errorf("after rotation: wrote %d times, want 2", len(writes))
	}
}

// Observe must not seed the secret, so a CR being measured stays read-only.
func TestObserveDoesNotRecordMaskedSecret(t *testing.T) {
	metrics.DriftCorrectedTotal.Reset()
	var writes []string
	hc := maskingServer(t, "observed", &writes)
	ep := ResourceEndpoint{Name: "downloadClients", Path: "/api/v3/downloadclient", MatchField: "name", Policy: CreateOrUpdate}

	for range 3 {
		if err := reconcileResource(context.Background(), hc, ep, desiredWithPassword("observed", "pw-v1"), true); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
	}
	if len(writes) != 0 {
		t.Errorf("observe wrote to the app: %v", writes)
	}
}
