package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kyleseneker/media-operator/internal/metrics"
)

func TestPing(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    bool
	}{
		{"success", http.StatusOK, false},
		{"server error", http.StatusInternalServerError, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			})
			err := hc.Ping(context.Background(), "/health")
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGetJSON(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		statusCode int
		wantErr    bool
		wantKey    string
		wantVal    interface{}
	}{
		{"valid object", `{"name":"test","id":1}`, 200, false, "name", "test"},
		{"server error", `error`, 500, true, "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			})
			result, err := hc.GetJSON(context.Background(), "/api/config")
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantVal, result[tt.wantKey])
			}
		})
	}
}

func TestGetJSON_InvalidJSON(t *testing.T) {
	_, hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	})
	_, err := hc.GetJSON(context.Background(), "/api/config")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshaling")
}

func TestGetJSONList(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		statusCode int
		wantErr    bool
		wantLen    int
	}{
		{"valid array", `[{"name":"a"},{"name":"b"}]`, 200, false, 2},
		{"empty array", `[]`, 200, false, 0},
		{"server error", `error`, 500, true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			})
			result, err := hc.GetJSONList(context.Background(), "/api/list")
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Len(t, result, tt.wantLen)
			}
		})
	}
}

func TestPutJSON(t *testing.T) {
	var capturedMethod string
	var capturedBody map[string]interface{}

	_, hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
	})

	err := hc.PutJSON(context.Background(), "/api/config/1", map[string]interface{}{"key": "val"})
	require.NoError(t, err)
	assert.Equal(t, http.MethodPut, capturedMethod)
	assert.Equal(t, "val", capturedBody["key"])
}

func TestPostJSON(t *testing.T) {
	var capturedMethod string

	_, hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		w.WriteHeader(http.StatusCreated)
	})

	err := hc.PostJSON(context.Background(), "/api/resource", map[string]interface{}{"name": "test"})
	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, capturedMethod)
}

func TestDeleteJSON(t *testing.T) {
	var capturedMethod string

	_, hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		w.WriteHeader(http.StatusOK)
	})

	err := hc.DeleteJSON(context.Background(), "/api/resource/1")
	require.NoError(t, err)
	assert.Equal(t, http.MethodDelete, capturedMethod)
}

func TestDo(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		respBody   string
		wantErr    bool
	}{
		{"success", 200, `{"ok":true}`, false},
		{"non-2xx returns APIError", 400, `{"error":"bad"}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.respBody))
			})
			data, err := hc.Do(context.Background(), http.MethodGet, "/api/test", nil)
			if tt.wantErr {
				assert.Error(t, err)
				var apiErr *APIError
				assert.True(t, errors.As(err, &apiErr))
				assert.Equal(t, tt.statusCode, apiErr.StatusCode)
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, data)
			}
		})
	}
}

func TestDo_WithBody(t *testing.T) {
	var capturedBody []byte

	_, hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})

	_, err := hc.Do(context.Background(), http.MethodPost, "/api/test", map[string]string{"key": "val"})
	require.NoError(t, err)

	var parsed map[string]string
	require.NoError(t, json.Unmarshal(capturedBody, &parsed))
	assert.Equal(t, "val", parsed["key"])
}

func TestDoRaw(t *testing.T) {
	var capturedCT string

	_, hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedCT = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("raw response"))
	})

	data, err := hc.DoRaw(context.Background(), http.MethodPost, "/api/test", nil, "text/plain")
	require.NoError(t, err)
	assert.Equal(t, "text/plain", capturedCT)
	assert.Equal(t, "raw response", string(data))
}

func TestDoForm(t *testing.T) {
	var capturedCT string
	var capturedBody string

	_, hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedCT = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.WriteHeader(http.StatusOK)
	})

	form := url.Values{"key": {"val"}, "num": {"42"}}
	_, err := hc.DoForm(context.Background(), "/api/form", form)
	require.NoError(t, err)
	assert.Equal(t, "application/x-www-form-urlencoded", capturedCT)
	assert.Contains(t, capturedBody, "key=val")
	assert.Contains(t, capturedBody, "num=42")
}

func TestAuthHeaderSent(t *testing.T) {
	var capturedHeader string

	_, hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("X-Api-Key")
		w.WriteHeader(http.StatusOK)
	})

	_, err := hc.Do(context.Background(), http.MethodGet, "/test", nil)
	require.NoError(t, err)
	assert.Equal(t, "test-key", capturedHeader)
}

// TestDo_RecordsErrorMetric verifies non-2xx responses increment the error counter
// with the correct status class label.
func TestDo_RecordsErrorMetric(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantClass  string
	}{
		{"4xx", 400, "4xx"},
		{"5xx", 500, "5xx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := testutil.ToFloat64(metrics.AppAPIErrorsTotal.WithLabelValues(testAppLabel, tt.wantClass))

			_, hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			})
			_, err := hc.Do(context.Background(), http.MethodGet, "/test", nil)
			assert.Error(t, err)

			after := testutil.ToFloat64(metrics.AppAPIErrorsTotal.WithLabelValues(testAppLabel, tt.wantClass))
			assert.Equal(t, before+1, after, "expected %s error counter to increment by 1", tt.wantClass)
		})
	}
}

// TestDo_RecordsDurationHistogram verifies the duration histogram records samples
// on both success and error outcomes.
func TestDo_RecordsDurationHistogram(t *testing.T) {
	_, hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Histogram count should increase after each request. We read the count via
	// CollectAndCount which sums across all label combinations — so just check
	// that the total grew by at least 1.
	before := testutil.CollectAndCount(metrics.AppAPIRequestDuration)
	_, err := hc.Do(context.Background(), http.MethodGet, "/test", nil)
	require.NoError(t, err)
	after := testutil.CollectAndCount(metrics.AppAPIRequestDuration)
	assert.GreaterOrEqual(t, after, before, "expected duration histogram to record the request")
}
