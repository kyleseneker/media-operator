package engine

import (
	"errors"
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			name:     "APIKey",
			authType: AuthAPIKey,
			setup:    func(c *HTTPClient) { c.apiKey = "my-key" },
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
