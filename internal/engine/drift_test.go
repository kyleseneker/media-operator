package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/kyleseneker/media-operator/internal/metrics"
)

func driftServer(t *testing.T, existing map[string]any, writes *[]string) *HTTPClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode([]map[string]any{existing})
			return
		}
		if writes != nil {
			*writes = append(*writes, r.Method+" "+r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(existing)
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

func endpointFor() ResourceEndpoint {
	return ResourceEndpoint{Name: "downloadClients", Path: "/api/v3/downloadclient", MatchField: "name", Policy: CreateOrUpdate}
}

// A resource whose live state already matches must not be rewritten, so the
// counter stays flat when nothing has drifted.
func TestNoDriftLeavesCounterUnchanged(t *testing.T) {
	metrics.DriftCorrectedTotal.Reset()
	existing := map[string]any{"id": float64(1), "name": "qbit", "enable": true}
	hc := driftServer(t, existing, nil)

	if err := reconcileResource(context.Background(), hc, endpointFor(), map[string]any{"name": "qbit", "enable": true}, false); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got := testutil.CollectAndCount(metrics.DriftCorrectedTotal); got != 0 {
		t.Errorf("counter moved despite no drift: %d series", got)
	}
}

// A resource the operator has to rewrite increments the counter, so a correction
// that never sticks shows up as a steady rate rather than passing unnoticed.
func TestDriftCorrectionIncrementsCounter(t *testing.T) {
	metrics.DriftCorrectedTotal.Reset()
	existing := map[string]any{"id": float64(1), "name": "qbit", "enable": false}
	hc := driftServer(t, existing, nil)

	if err := reconcileResource(context.Background(), hc, endpointFor(), map[string]any{"name": "qbit", "enable": true}, false); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if got := testutil.ToFloat64(metrics.DriftCorrectedTotal.WithLabelValues("sonarr", "downloadClients", "qbit")); got != 1 {
		t.Errorf("drift_corrected_total = %v, want 1", got)
	}
}

// Observe must record the difference and leave the app untouched, so drift can be
// measured before the operator is given write authority.
func TestObserveRecordsDriftWithoutWriting(t *testing.T) {
	metrics.DriftCorrectedTotal.Reset()
	var writes []string
	existing := map[string]any{"id": float64(1), "name": "qbit", "enable": false}
	hc := driftServer(t, existing, &writes)

	if err := reconcileResource(context.Background(), hc, endpointFor(), map[string]any{"name": "qbit", "enable": true}, true); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if got := testutil.ToFloat64(metrics.DriftCorrectedTotal.WithLabelValues("sonarr", "downloadClients", "qbit")); got != 1 {
		t.Errorf("drift_corrected_total = %v, want 1", got)
	}
	if len(writes) != 0 {
		t.Errorf("observe wrote to the app: %v", writes)
	}
}

// Enforce is the default, so an unset policy still corrects drift.
func TestEnforceWritesTheCorrection(t *testing.T) {
	metrics.DriftCorrectedTotal.Reset()
	var writes []string
	existing := map[string]any{"id": float64(1), "name": "qbit", "enable": false}
	hc := driftServer(t, existing, &writes)

	if err := reconcileResource(context.Background(), hc, endpointFor(), map[string]any{"name": "qbit", "enable": true}, false); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if len(writes) != 1 {
		t.Errorf("expected one write, got %v", writes)
	}
}
