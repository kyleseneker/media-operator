package engine

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// testAppLabel is the app label used by newTestServer. Tests that assert on
// metrics should use this label when reading values via testutil.ToFloat64.
const testAppLabel = "test-app"

// newTestServer creates an httptest server and an HTTPClient pointing at it.
// The client's transport is overridden to bypass SSRF protection (which blocks localhost).
// The client is tagged with app label "test-app" for metrics assertions.
func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *HTTPClient) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	hc, err := NewHTTPClient(srv.URL, AuthAPIKey,
		WithAPIKey("test-key"),
		WithTransport(&http.Transport{}),
		WithAppLabel(testAppLabel),
	)
	require.NoError(t, err)
	return srv, hc
}

// newTestServerWithAuth creates an httptest server and an HTTPClient with a custom auth type.
func newTestServerWithAuth(t *testing.T, handler http.HandlerFunc, authType AuthType, opts ...HTTPClientOption) (*httptest.Server, *HTTPClient) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	allOpts := append([]HTTPClientOption{WithTransport(&http.Transport{})}, opts...)
	hc, err := NewHTTPClient(srv.URL, authType, allOpts...)
	require.NoError(t, err)
	return srv, hc
}
