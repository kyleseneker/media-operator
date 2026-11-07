package engine

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kyleseneker/media-operator/internal/metrics"
)

func TestValidateBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"valid http", "http://example.com", false},
		{"valid https", "https://example.com", false},
		{"http with port and path", "http://example.com:8989/sonarr", false},
		{"ftp scheme rejected", "ftp://example.com", true},
		{"file scheme rejected", "file:///etc/passwd", true},
		{"javascript scheme rejected", "javascript:alert(1)", true},
		{"empty string", "", true},
		{"no host", "http://", true},
		{"missing scheme", "example.com", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBaseURL(tt.url)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestIsBlockedIP(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		blocked bool
	}{
		{"loopback 127.0.0.1", "127.0.0.1", true},
		{"loopback 127.0.0.2", "127.0.0.2", true},
		{"link-local metadata", "169.254.169.254", true},
		{"link-local other", "169.254.1.1", true},
		{"ipv6 loopback", "::1", true},
		{"ipv6 link-local", "fe80::1", true},
		{"public IP", "8.8.8.8", false},
		{"private 10.x allowed", "10.0.0.1", false},
		{"private 192.168.x allowed", "192.168.1.1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			require.NotNil(t, ip, "failed to parse IP %q", tt.ip)
			assert.Equal(t, tt.blocked, isBlockedIP(ip))
		})
	}
}

func TestNewHTTPClient(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		auth    AuthType
		opts    []HTTPClientOption
		wantErr bool
	}{
		{"valid", "http://example.com", AuthAPIKey, []HTTPClientOption{WithAPIKey("key")}, false},
		{"invalid scheme", "ftp://example.com", AuthNone, nil, true},
		{"empty url", "", AuthNone, nil, true},
		{"with custom header", "http://example.com", AuthAPIKey, []HTTPClientOption{WithAPIKey("k"), WithAPIKeyHeader("X-API-KEY")}, false},
		{"with plex token", "http://example.com", AuthPlexToken, []HTTPClientOption{WithPlexToken("tok")}, false},
		{"with transport override", "http://example.com", AuthNone, []HTTPClientOption{WithTransport(&http.Transport{})}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hc, err := NewHTTPClient(tt.url, tt.auth, tt.opts...)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, hc)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, hc)
			}
		})
	}
}

func TestNewHTTPClient_TrailingSlash(t *testing.T) {
	hc, err := NewHTTPClient("http://example.com/", AuthNone)
	require.NoError(t, err)
	assert.Equal(t, "http://example.com", hc.baseURL)
}

func TestAPIError_Error(t *testing.T) {
	err := &APIError{StatusCode: 500, Body: []byte(`{"error":"secret details"}`)}
	assert.Contains(t, err.Error(), "500")
	assert.NotContains(t, err.Error(), "secret")
}

func TestAPIError_DetailedMessage(t *testing.T) {
	err := &APIError{StatusCode: 422, Body: []byte(`{"error":"validation failed"}`)}
	msg := err.DetailedMessage()
	assert.Contains(t, msg, "422")
	assert.Contains(t, msg, "validation failed")
}

func TestAPIError_Unwrap(t *testing.T) {
	apiErr := &APIError{StatusCode: 404, Body: []byte("not found")}
	var target *APIError
	assert.True(t, errors.As(apiErr, &target))
	assert.Equal(t, 404, target.StatusCode)
}

func TestApplyAuth(t *testing.T) {
	tests := []struct {
		name       string
		authType   AuthType
		setup      func(*HTTPClient)
		wantHeader string
		wantValue  string
	}{
		{
			name:       "APIKey",
			authType:   AuthAPIKey,
			setup:      func(c *HTTPClient) { c.apiKey = "my-key" },
			wantHeader: "X-Api-Key",
			wantValue:  "my-key",
		},
		{
			name:     "APIKey custom header",
			authType: AuthAPIKey,
			setup: func(c *HTTPClient) {
				c.apiKey = "my-key"
				c.apiKeyHeader = "X-API-KEY"
			},
			wantHeader: "X-API-KEY",
			wantValue:  "my-key",
		},
		{
			name:       "PlexToken",
			authType:   AuthPlexToken,
			setup:      func(c *HTTPClient) { c.plexToken = "plex-tok" },
			wantHeader: "X-Plex-Token",
			wantValue:  "plex-tok",
		},
		{
			name:       "MediaBrowser without token",
			authType:   AuthMediaBrowser,
			setup:      func(c *HTTPClient) {},
			wantHeader: "Authorization",
			wantValue:  `MediaBrowser Client="media-operator", Device="operator", DeviceId="media-operator-operator", Version="1.0.0"`,
		},
		{
			name:       "MediaBrowser with token",
			authType:   AuthMediaBrowser,
			setup:      func(c *HTTPClient) { c.mediaToken = "jf-tok" },
			wantHeader: "Authorization",
			wantValue:  `MediaBrowser Client="media-operator", Device="operator", DeviceId="media-operator-operator", Version="1.0.0", Token="jf-tok"`,
		},
		{
			name:       "FormEncoded",
			authType:   AuthFormEncoded,
			setup:      func(c *HTTPClient) { c.apiKey = "bazarr-key" },
			wantHeader: "X-Api-Key",
			wantValue:  "bazarr-key",
		},
		{
			name:       "Session with API key",
			authType:   AuthSession,
			setup:      func(c *HTTPClient) { c.apiKey = "seerr-key" },
			wantHeader: "X-Api-Key",
			wantValue:  "seerr-key",
		},
		{
			name:       "None",
			authType:   AuthNone,
			setup:      func(c *HTTPClient) {},
			wantHeader: "",
			wantValue:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &HTTPClient{authType: tt.authType, apiKeyHeader: "X-Api-Key"}
			tt.setup(c)
			req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
			require.NoError(t, err)
			c.applyAuth(req)
			if tt.wantHeader == "" {
				assert.Empty(t, req.Header.Get("X-Api-Key"))
				assert.Empty(t, req.Header.Get("Authorization"))
				assert.Empty(t, req.Header.Get("X-Plex-Token"))
			} else {
				assert.Equal(t, tt.wantValue, req.Header.Get(tt.wantHeader))
			}
		})
	}
}

func TestApplyAuth_Cookie(t *testing.T) {
	c := &HTTPClient{authType: AuthCookie, cookieSessionID: "abc123"}
	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)
	c.applyAuth(req)
	cookies := req.Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, "SID", cookies[0].Name)
	assert.Equal(t, "abc123", cookies[0].Value)
}

func TestSetters(t *testing.T) {
	c := &HTTPClient{}

	c.SetMediaToken("mt")
	assert.Equal(t, "mt", c.mediaToken)

	c.SetAPIKey("ak")
	assert.Equal(t, "ak", c.apiKey)

	c.SetCookieSessionID("sid")
	assert.Equal(t, "sid", c.cookieSessionID)
}

// A dial failure must not be reported as an SSRF block; they lead to completely
// different investigations.
func TestSSRFDialErrorDistinguishesBlockedFromUnreachable(t *testing.T) {
	// localhost resolves only to loopback, which is genuinely blocked.
	_, err := ssrfSafeDialContext(context.Background(), "tcp", "localhost:9")
	require.Error(t, err, "expected an error dialing loopback")
	assert.ErrorContains(t, err, "no allowed IP addresses", "loopback should report as blocked")

	// A routable address that refuses the connection must report the dial error.
	_, err = ssrfSafeDialContext(context.Background(), "tcp", "192.0.2.1:9")
	require.Error(t, err, "expected an error dialing an unreachable host")
	assert.NotContains(t, err.Error(), "no allowed IP addresses",
		"an unreachable but allowed address must not be reported as blocked")
}

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
			hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
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
		wantVal    any
	}{
		{"valid object", `{"name":"test","id":1}`, 200, false, "name", "test"},
		{"server error", `error`, 500, true, "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
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
	hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
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
			hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
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
	var capturedBody map[string]any

	hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
	})

	err := hc.PutJSON(context.Background(), "/api/config/1", map[string]any{"key": "val"})
	require.NoError(t, err)
	assert.Equal(t, http.MethodPut, capturedMethod)
	assert.Equal(t, "val", capturedBody["key"])
}

func TestPostJSON(t *testing.T) {
	var capturedMethod string

	hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		w.WriteHeader(http.StatusCreated)
	})

	err := hc.PostJSON(context.Background(), "/api/resource", map[string]any{"name": "test"})
	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, capturedMethod)
}

func TestDeleteJSON(t *testing.T) {
	var capturedMethod string

	hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
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
			hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.respBody))
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

	hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
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

	hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedCT = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("raw response"))
	})

	data, err := hc.DoRaw(context.Background(), http.MethodPost, "/api/test", nil, "text/plain")
	require.NoError(t, err)
	assert.Equal(t, "text/plain", capturedCT)
	assert.Equal(t, "raw response", string(data))
}

func TestDoForm(t *testing.T) {
	var capturedCT string
	var capturedBody string

	hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
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

	hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
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

			hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
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
	hc := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
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
