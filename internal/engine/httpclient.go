package engine

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/kyleseneker/media-operator/internal/metrics"
)

const outcomeError = "error"

// APIError represents a non-2xx HTTP response from a target app.
// Error() returns only the status code, which is safe for CR status conditions.
// Body contains the full response for debug logging.
type APIError struct {
	StatusCode int
	Body       []byte
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API request failed with status %d", e.StatusCode)
}

// DetailedMessage returns the status code and full response body for debug logging.
func (e *APIError) DetailedMessage() string {
	return fmt.Sprintf("API returned %d: %s", e.StatusCode, string(e.Body))
}

// HTTPClient is a generic HTTP client that supports all auth strategies.
type HTTPClient struct {
	baseURL    string
	authType   AuthType
	httpClient *http.Client

	// appLabel identifies the target app ("sonarr", "radarr", etc.) for metrics.
	// Defaults to "unknown" if not set via WithAppLabel.
	appLabel string

	// API key auth
	apiKey       string
	apiKeyHeader string // "X-Api-Key", "X-API-KEY", "x-api-key", etc.

	// Plex auth
	plexToken string

	// Cookie auth
	cookieSessionID   string
	cookieSessionName string

	// MediaBrowser auth
	mediaToken string
}

// HTTPClientOption configures an HTTPClient.
type HTTPClientOption func(*HTTPClient)

// NewHTTPClient creates a new HTTPClient with the given auth type.
// Returns an error if the URL scheme is not http/https.
func NewHTTPClient(baseURL string, authType AuthType, opts ...HTTPClientOption) (*HTTPClient, error) {
	if err := validateBaseURL(baseURL); err != nil {
		return nil, err
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("creating cookie jar: %w", err)
	}
	c := &HTTPClient{
		baseURL:      strings.TrimRight(baseURL, "/"),
		authType:     authType,
		apiKeyHeader: "X-Api-Key",
		appLabel:     "unknown",
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Jar:       jar,
			Transport: NewHTTPTransport(nil),
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// validateBaseURL checks that the URL is well-formed and uses an allowed scheme.
func validateBaseURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", rawURL, err)
	}
	switch u.Scheme {
	case "http", "https":
		// allowed
	default:
		return fmt.Errorf("URL scheme %q is not allowed (must be http or https)", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("URL %q has no host", rawURL)
	}
	return nil
}

// ssrfSafeDialContext resolves the host and blocks connections to link-local
// and loopback addresses, preventing SSRF to cloud metadata services and localhost.
func ssrfSafeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var allowed int
	var lastErr error
	for _, ip := range ips {
		if isBlockedIP(ip.IP) {
			continue
		}
		allowed++
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}

	// Distinguish "every address was blocked" from "the dial failed", which are
	// very different problems to chase.
	if allowed == 0 {
		return nil, fmt.Errorf("no allowed IP addresses for host %q (blocked: loopback, link-local)", host)
	}
	return nil, fmt.Errorf("dialing %s: %w", addr, lastErr)
}

// blockedCIDRs contains network ranges that the operator must never connect to.
var blockedCIDRs = func() []*net.IPNet {
	cidrs := []string{
		"127.0.0.0/8",    // loopback
		"169.254.0.0/16", // link-local / cloud metadata
		"::1/128",        // IPv6 loopback
		"fe80::/10",      // IPv6 link-local
	}
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, _ := net.ParseCIDR(c)
		nets = append(nets, n)
	}
	return nets
}()

func isBlockedIP(ip net.IP) bool {
	for _, n := range blockedCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// WithTLSConfig sets custom TLS configuration on the HTTP transport.
func WithTLSConfig(tlsCfg *tls.Config) HTTPClientOption {
	return func(c *HTTPClient) {
		if tlsCfg != nil {
			c.httpClient.Transport = NewHTTPTransport(tlsCfg)
		}
	}
}

// WithDisableRedirect prevents the HTTP client from following redirects.
// Used by qBittorrent to capture the SID cookie before redirect.
func WithDisableRedirect() HTTPClientOption {
	return func(c *HTTPClient) {
		c.httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
}

func WithAPIKey(key string) HTTPClientOption {
	return func(c *HTTPClient) { c.apiKey = key }
}

func WithAPIKeyHeader(header string) HTTPClientOption {
	return func(c *HTTPClient) { c.apiKeyHeader = header }
}

func WithPlexToken(token string) HTTPClientOption {
	return func(c *HTTPClient) { c.plexToken = token }
}

func WithMediaToken(token string) HTTPClientOption {
	return func(c *HTTPClient) { c.mediaToken = token }
}

// WithTimeout overrides the default request timeout. Apps that accept large
// payloads need longer than the default.
func WithTimeout(d time.Duration) HTTPClientOption {
	return func(c *HTTPClient) { c.httpClient.Timeout = d }
}

// WithTransport overrides the default HTTP transport.
// Useful for testing with httptest servers that listen on localhost.
func WithTransport(rt http.RoundTripper) HTTPClientOption {
	return func(c *HTTPClient) { c.httpClient.Transport = rt }
}

// WithAppLabel sets the app identifier ("sonarr", "radarr", etc.) used as
// a label on custom Prometheus metrics.
func WithAppLabel(app string) HTTPClientOption {
	return func(c *HTTPClient) { c.appLabel = app }
}

// AppLabel returns the app identifier used for metrics labeling.
func (c *HTTPClient) AppLabel() string {
	return c.appLabel
}

// SetMediaToken updates the MediaBrowser token after authentication.
func (c *HTTPClient) SetMediaToken(token string) {
	c.mediaToken = token
}

// SetAPIKey updates the API key.
func (c *HTTPClient) SetAPIKey(key string) {
	c.apiKey = key
}

// SetCookieSessionID sets the session cookie for cookie-based auth.
func (c *HTTPClient) SetCookieSessionID(sid string) {
	c.cookieSessionID = sid
}

// SetCookieSession sets the session cookie under a specific name.
func (c *HTTPClient) SetCookieSession(name, value string) {
	c.cookieSessionName = name
	c.cookieSessionID = value
}

// CookieValuePrefix returns the name and value of the first cookie whose name starts with prefix.
func (c *HTTPClient) CookieValuePrefix(prefix string) (string, string) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return "", ""
	}
	for _, cookie := range c.httpClient.Jar.Cookies(u) {
		if strings.HasPrefix(cookie.Name, prefix) {
			return cookie.Name, cookie.Value
		}
	}
	return "", ""
}

// CookieValue returns the value of the named cookie from the cookie jar for the client's base URL.
// Returns an empty string if the cookie is not found.
func (c *HTTPClient) CookieValue(name string) string {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return ""
	}
	for _, cookie := range c.httpClient.Jar.Cookies(u) {
		if cookie.Name == name {
			return cookie.Value
		}
	}
	return ""
}

// DoRaw executes an HTTP request with a raw body and explicit content type.
// This is the low-level method that all other request methods build on.
// It records request latency and error metrics labeled with the client's appLabel.
func (c *HTTPClient) DoRaw(ctx context.Context, method, path string, body io.Reader, contentType string) ([]byte, error) {
	start := time.Now()
	outcome := "success"
	defer func() {
		metrics.AppAPIRequestDuration.WithLabelValues(c.appLabel, method, outcome).Observe(time.Since(start).Seconds())
	}()

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		outcome = outcomeError
		return nil, fmt.Errorf("creating request: %w", err)
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	c.applyAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		outcome = outcomeError
		metrics.AppAPIErrorsTotal.WithLabelValues(c.appLabel, "network").Inc()
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		outcome = outcomeError
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		outcome = outcomeError
		metrics.AppAPIErrorsTotal.WithLabelValues(c.appLabel, fmt.Sprintf("%dxx", resp.StatusCode/100)).Inc()
		return nil, &APIError{StatusCode: resp.StatusCode, Body: respBody}
	}

	return respBody, nil
}

// Do executes an HTTP request with a JSON body.
func (c *HTTPClient) Do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshaling body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}
	return c.DoRaw(ctx, method, path, reqBody, "application/json")
}

// DoForm executes a form-encoded POST request.
func (c *HTTPClient) DoForm(ctx context.Context, path string, form url.Values) ([]byte, error) {
	return c.DoRaw(ctx, http.MethodPost, path, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
}

func (c *HTTPClient) applyAuth(req *http.Request) {
	switch c.authType {
	case AuthAPIKey, AuthFormEncoded:
		if c.apiKey != "" {
			req.Header.Set(c.apiKeyHeader, c.apiKey)
		}
	case AuthPlexToken:
		if c.plexToken != "" {
			req.Header.Set("X-Plex-Token", c.plexToken)
			req.Header.Set("Accept", "application/json")
			req.Header.Set("X-Plex-Client-Identifier", "media-operator")
			req.Header.Set("X-Plex-Product", "media-operator")
			req.Header.Set("X-Plex-Version", "1.0.0")
		}
	case AuthMediaBrowser:
		auth := `MediaBrowser Client="media-operator", Device="operator", DeviceId="media-operator-operator", Version="1.0.0"`
		if c.mediaToken != "" {
			auth += fmt.Sprintf(`, Token="%s"`, c.mediaToken)
		}
		req.Header.Set("Authorization", auth)
	case AuthCookie:
		if c.cookieSessionID != "" {
			name := c.cookieSessionName
			if name == "" {
				name = "SID"
			}
			req.AddCookie(&http.Cookie{Name: name, Value: c.cookieSessionID})
		}
	case AuthSession:
		if c.apiKey != "" {
			req.Header.Set("X-Api-Key", c.apiKey)
		}
		// Cookie jar handles session cookies automatically
	}
}

// Ping checks if the app is reachable at the given path.
func (c *HTTPClient) Ping(ctx context.Context, path string) error {
	_, err := c.Do(ctx, http.MethodGet, path, nil)
	return err
}

// GetJSON fetches a JSON object from the given path.
func (c *HTTPClient) GetJSON(ctx context.Context, path string) (map[string]any, error) {
	data, err := c.Do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling: %w", err)
	}
	return result, nil
}

// GetJSONList fetches a JSON array from the given path.
func (c *HTTPClient) GetJSONList(ctx context.Context, path string) ([]map[string]any, error) {
	data, err := c.Do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var result []map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling: %w", err)
	}
	return result, nil
}

// PutJSON sends a PUT request with a JSON body.
func (c *HTTPClient) PutJSON(ctx context.Context, path string, body map[string]any) error {
	_, err := c.Do(ctx, http.MethodPut, path, body)
	return err
}

// PostJSON sends a POST request with a JSON body.
func (c *HTTPClient) PostJSON(ctx context.Context, path string, body map[string]any) error {
	_, err := c.Do(ctx, http.MethodPost, path, body)
	return err
}

// DeleteJSON sends a DELETE request.
func (c *HTTPClient) DeleteJSON(ctx context.Context, path string) error {
	_, err := c.Do(ctx, http.MethodDelete, path, nil)
	return err
}
