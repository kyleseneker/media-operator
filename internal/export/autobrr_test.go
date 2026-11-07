package export

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	automationv1alpha1 "github.com/kyleseneker/media-operator/api/automation/v1alpha1"
)

var fakeAutobrrAPI = map[string]string{
	"/api/downloaders": `[{"id":3,"name":"qBit","type":"QBITTORRENT","enabled":true,"host":"http://qbit","port":8080,
		"tls":false,"username":"admin","password":"<redacted>"}]`,
	"/api/indexer": `[{"id":1,"name":"Tracker","implementation":"irc","enabled":true,"base_url":"","api_key":""}]`,
	"/api/irc": `[{"id":1,"name":"Net","enabled":false,"server":"irc.example","port":6697,"tls":true,"nick":"bot",
		"auth":{"mechanism":"NICKSERV","account":"bot","password":"<redacted>"},
		"channels":[{"name":"#announce","password":""},{"name":"#vip","password":"<redacted>"}]}]`,
	"/api/feeds":   `[{"id":1,"name":"Feed","type":"TORZNAB","enabled":true,"url":"http://feed","interval":15,"api_key":"<redacted>"}]`,
	"/api/filters": `[{"id":7,"name":"TV"}]`,
	"/api/filters/7": `{"id":7,"name":"TV","enabled":true,"priority":1,"use_regex":false,"match_releases":"",
		"resolutions":["1080p"],"sources":["WEB-DL"],"codecs":[],"indexers":[{"id":1,"name":"Tracker","identifier":"tracker"}]}`,
	"/api/actions": `[{"id":2,"name":"Send","type":"QBITTORRENT","enabled":true,"client_id":3,"category":"tv","save_path":""}]`,
}

func TestAutobrrExportsEachResourceWithSecretsInTheSecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Token") != "k" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		body, ok := fakeAutobrrAPI[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	res, err := Run(context.Background(), Options{App: "autobrr", URL: srv.URL, APIKey: "k"})
	require.NoError(t, err)
	cfg, ok := res.Config.(*automationv1alpha1.AutobrrConfig)
	require.True(t, ok)

	require.Len(t, cfg.Spec.DownloadClients, 1)
	dc := cfg.Spec.DownloadClients[0]
	assert.Equal(t, "qBittorrent", dc.Type)
	assert.Equal(t, 8080, *dc.Port)
	assert.Equal(t, "admin", res.Secret.StringData[dc.UsernameSecretRef.Key])
	assert.Contains(t, res.Unfilled, dc.PasswordSecretRef.Key)
	assert.Nil(t, dc.APIKeySecretRef)

	require.Len(t, cfg.Spec.IRCNetworks, 1)
	irc := cfg.Spec.IRCNetworks[0]
	assert.Equal(t, "NickServ", irc.AuthMechanism)
	assert.Equal(t, "bot", res.Secret.StringData[irc.AuthAccountSecretRef.Key])
	assert.Nil(t, irc.Channels[0].PasswordSecretRef)
	assert.Equal(t, "irc-net-vip-password", irc.Channels[1].PasswordSecretRef.Key)

	require.Len(t, cfg.Spec.Feeds, 1)
	assert.Equal(t, 15, *cfg.Spec.Feeds[0].Interval)
	require.Len(t, cfg.Spec.Indexers, 1)
	assert.Nil(t, cfg.Spec.Indexers[0].APIKeySecretRef)

	require.Len(t, cfg.Spec.Filters, 1)
	filter := cfg.Spec.Filters[0]
	assert.Equal(t, []string{"1080p"}, filter.Resolutions)
	assert.Empty(t, filter.Indexers)
	assert.Contains(t, res.Warnings, `filter "TV": AutobrrConfig lists indexers by name, so they stay as set in Autobrr`)
	require.Len(t, filter.Actions, 1)
	assert.Equal(t, "qBit", filter.Actions[0].ClientRef, "an action names its download client")
	assert.Equal(t, "qBittorrent", filter.Actions[0].Type)
	assert.Equal(t, "tv", filter.Actions[0].Category)
}
