package export

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	downloadsv1alpha1 "github.com/kyleseneker/media-operator/api/downloads/v1alpha1"
)

func TestQBittorrentExportsPreferencesCategoriesAndLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			require.NoError(t, r.ParseForm())
			if r.Form.Get("username") != "admin" || r.Form.Get("password") != "secret" {
				_, _ = w.Write([]byte("Fails."))
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "session", Path: "/"})
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/preferences":
			_, _ = w.Write([]byte(`{"save_path":"/downloads/","dht":false,"max_connec":400,"max_ratio":2.5,` +
				`"max_ratio_act":3,"web_ui_port":8080,"bypass_auth_subnet_whitelist":"","not_in_the_crd":1}`))
		case "/api/v2/torrents/categories":
			_, _ = w.Write([]byte(`{"tv":{"name":"tv","savePath":"/downloads/tv"},"movies":{"name":"movies","savePath":""}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	res, err := Run(context.Background(), Options{App: "qbittorrent", URL: srv.URL, Username: "admin", Password: "secret"})
	require.NoError(t, err)
	cfg, ok := res.Config.(*downloadsv1alpha1.QBittorrentConfig)
	require.True(t, ok)
	prefs := cfg.Spec.Preferences
	assert.Equal(t, "/downloads/", prefs.SavePath)
	assert.False(t, *prefs.DHT)
	assert.Equal(t, 400, *prefs.MaxConnec)
	assert.Equal(t, "2.5", *prefs.MaxRatio)
	assert.Equal(t, "removeWithContent", *prefs.MaxRatioAction)
	assert.Equal(t, []downloadsv1alpha1.QBittorrentCategory{{Name: "movies"}, {Name: "tv", SavePath: "/downloads/tv"}}, cfg.Spec.Categories)

	assert.Equal(t, "username", cfg.Spec.Connection.UsernameSecretRef.Key)
	assert.Equal(t, "password", cfg.Spec.Connection.PasswordSecretRef.Key)
	assert.Equal(t, map[string]string{"username": "admin", "password": "secret"}, res.Secret.StringData)
	assert.Empty(t, res.Unfilled)
	assert.True(t, NeedsLogin("qbittorrent"))
	assert.False(t, NeedsAPIKey("qbittorrent"))
}
