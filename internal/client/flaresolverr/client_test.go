package flaresolverr

import (
	"context"
	"encoding/json"
	"io"
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
	hc, err := engine.NewHTTPClient(srv.URL, engine.AuthNone,
		engine.WithTransport(&http.Transport{}),
	)
	require.NoError(t, err)
	return srv, NewClient(hc)
}

func TestPing(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(response{Status: "ok", Sessions: []string{}})
	})
	assert.NoError(t, c.Ping(context.Background()))
}

func TestListSessions(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "sessions.list")
		json.NewEncoder(w).Encode(response{Status: "ok", Sessions: []string{"sess1", "sess2"}})
	})
	sessions, err := c.ListSessions(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"sess1", "sess2"}, sessions)
}

func TestCreateSession(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "sessions.create")
		assert.Contains(t, string(body), "my-session")
		json.NewEncoder(w).Encode(response{Status: "ok"})
	})
	assert.NoError(t, c.CreateSession(context.Background(), "my-session"))
}

func TestDestroySession(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "sessions.destroy")
		json.NewEncoder(w).Encode(response{Status: "ok"})
	})
	assert.NoError(t, c.DestroySession(context.Background(), "my-session"))
}

func TestDo_ErrorStatus(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(response{Status: "error", Message: "something broke"})
	})
	err := c.Ping(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "something broke")
}
