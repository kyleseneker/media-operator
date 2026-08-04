package qbittorrent

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
	hc, err := engine.NewHTTPClient(srv.URL, engine.AuthCookie,
		engine.WithTransport(&http.Transport{}),
		engine.WithDisableRedirect(),
	)
	require.NoError(t, err)
	return NewClient(hc, "admin", "adminadmin")
}

func TestLogin_Success(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v2/auth/login", r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "username=admin")
		// Set cookie with Path=/ so the jar captures it for the httptest host
		http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test-sid", Path: "/"})
		_, _ = w.Write([]byte("Ok."))
	})
	err := c.Login(context.Background())
	require.NoError(t, err)
}

func TestLogin_Failure(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("Fails."))
	})
	err := c.Login(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "login failed")
}

func TestPing(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v2/app/version", r.URL.Path)
		_, _ = w.Write([]byte("v4.6.0"))
	})
	assert.NoError(t, c.Ping(context.Background()))
}

func TestGetPreferences(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v2/app/preferences", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"save_path": "/downloads"})
	})
	prefs, err := c.GetPreferences(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "/downloads", prefs["save_path"])
}

func TestSetPreferences(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v2/app/setPreferences", r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "json=")
		w.WriteHeader(http.StatusOK)
	})
	err := c.SetPreferences(context.Background(), map[string]any{"save_path": "/new"})
	assert.NoError(t, err)
}

func TestListCategories(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v2/torrents/categories", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"tv": map[string]any{"savePath": "/tv"}})
	})
	cats, err := c.ListCategories(context.Background())
	require.NoError(t, err)
	assert.Contains(t, cats, "tv")
}

func TestCreateCategory(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v2/torrents/createCategory", r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "category=movies")
		w.WriteHeader(http.StatusOK)
	})
	assert.NoError(t, c.CreateCategory(context.Background(), "movies", "/movies"))
}

func TestEditCategory(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v2/torrents/editCategory", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	})
	assert.NoError(t, c.EditCategory(context.Background(), "movies", "/new-movies"))
}
