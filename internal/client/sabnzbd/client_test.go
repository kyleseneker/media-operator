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

func newTestClient(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	hc, err := engine.NewHTTPClient(srv.URL, engine.AuthNone,
		engine.WithTransport(&http.Transport{}),
	)
	require.NoError(t, err)
	return srv, NewClient(hc, "test-api-key")
}

func TestPing(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api", r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "mode=version")
		assert.Contains(t, string(body), "apikey=test-api-key")
		w.Write([]byte(`"4.0.0"`))
	})
	assert.NoError(t, c.Ping(context.Background()))
}

func TestGetConfig(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "mode=get_config")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"config": map[string]interface{}{"misc": map[string]interface{}{"download_dir": "/downloads"}},
		})
	})
	cfg, err := c.GetConfig(context.Background())
	require.NoError(t, err)
	assert.Contains(t, cfg, "misc")
}

func TestSetConfig(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
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
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
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
