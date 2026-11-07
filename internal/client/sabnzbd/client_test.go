package sabnzbd

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

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	hc, err := engine.NewHTTPClient(srv.URL, engine.AuthNone,
		engine.WithTransport(&http.Transport{}),
	)
	require.NoError(t, err)
	return NewClient(hc, "test-api-key")
}

func TestPing(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api", r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "mode=version")
		assert.Contains(t, string(body), "apikey=test-api-key")
		_, _ = w.Write([]byte(`"4.0.0"`))
	})
	assert.NoError(t, c.Ping(context.Background()))
}

func TestGetConfig(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "mode=get_config")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"config": map[string]any{"misc": map[string]any{"download_dir": "/downloads"}},
		})
	})
	cfg, err := c.GetConfig(context.Background())
	require.NoError(t, err)
	assert.Contains(t, cfg, "misc")
}

func TestSetConfig(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "mode=set_config")
		assert.Contains(t, string(body), "section=misc")
		assert.Contains(t, string(body), "keyword=download_dir")
		assert.Contains(t, string(body), "value=%2Fnew")
		w.WriteHeader(http.StatusOK)
	})
	assert.NoError(t, c.SetConfig(context.Background(), "misc", "download_dir", "/new"))
}

func TestSetConfigMulti(t *testing.T) {
	var callCount int
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	})
	err := c.SetConfigMulti(context.Background(), "misc", map[string]string{
		"download_dir": "/dl", "complete_dir": "/done",
	})
	assert.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

func TestApiParams(t *testing.T) {
	c := &Client{apiKey: "key123"}
	params := c.apiParams("get_config", "section=misc&keyword=dir")
	assert.Equal(t, "get_config", params.Get("mode"))
	assert.Equal(t, "key123", params.Get("apikey"))
	assert.Equal(t, "json", params.Get("output"))
	assert.Equal(t, "misc", params.Get("section"))
	assert.Equal(t, "dir", params.Get("keyword"))
}

// SABnzbd answers a rejected change with HTTP 200; treating that as success
// reported servers and categories as synced when none had been written.
func TestSetConfigSurfacesARejectedChange(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":false,"error":"Config item does not exist"}`))
	})
	err := c.SetConfig(context.Background(), "servers,0", "host", "news")
	require.ErrorContains(t, err, "Config item does not exist")
}

func TestSetNamedAddressesTheEntryByName(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.NoError(t, r.ParseForm())
		assert.Equal(t, "set_config", r.PostForm.Get("mode"))
		assert.Equal(t, "servers", r.PostForm.Get("section"))
		assert.Equal(t, "Primary", r.PostForm.Get("keyword"))
		assert.Equal(t, "news.example.com", r.PostForm.Get("host"))
		_, _ = w.Write([]byte(`{"config":{"servers":[{"name":"Primary"}]}}`))
	})
	require.NoError(t, c.SetNamed(context.Background(), "servers", "Primary", map[string]string{"host": "news.example.com"}))
}
