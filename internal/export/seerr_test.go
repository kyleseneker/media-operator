package export

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	requestsv1alpha1 "github.com/kyleseneker/media-operator/api/requests/v1alpha1"
)

var fakeSeerrAPI = map[string]string{
	"/api/v1/settings/main": `{"applicationTitle":"Requests","hideAvailable":true,"defaultPermissions":32,
		"mediaServerType":2,"apiKey":"seerr-key"}`,
	"/api/v1/settings/jellyfin": `{"ip":"jellyfin","port":8096,"useSsl":false,"urlBase":"","name":"home"}`,
	"/api/v1/settings/sonarr": `[
		{"id":0,"name":"Sonarr 4K","hostname":"sonarr4k","port":8989,"apiKey":"k4k","is4k":true,"isDefault":true},
		{"id":1,"name":"Sonarr","hostname":"sonarr","port":8989,"apiKey":"sonarr-key","is4k":false,"isDefault":true,
			"activeProfileId":4,"activeProfileName":"HD-1080p","activeDirectory":"/tv","enableSeasonFolders":true}]`,
	"/api/v1/settings/radarr":                 `[]`,
	"/api/v1/settings/notifications/discord":  `{"enabled":true,"types":4094,"options":{"webhookUrl":"https://hook","enableMentions":true,"botUsername":""}}`,
	"/api/v1/settings/notifications/webhook":  `{"enabled":false,"types":0,"options":{}}`,
	"/api/v1/settings/notifications/telegram": `{"enabled":false}`,
}

func TestSeerrExportsSettingsServicesAndNotifications(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := fakeSeerrAPI[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	res, err := Run(context.Background(), Options{App: "seerr", URL: srv.URL, APIKey: "seerr-key"})
	require.NoError(t, err)
	cfg, ok := res.Config.(*requestsv1alpha1.SeerrConfig)
	require.True(t, ok)
	assert.Equal(t, "Requests", cfg.Spec.Main.ApplicationTitle)
	assert.Equal(t, 32, *cfg.Spec.Main.DefaultPermissions)

	require.NotNil(t, cfg.Spec.JellyfinAuth)
	assert.Equal(t, "jellyfin", cfg.Spec.JellyfinAuth.Hostname)
	assert.Equal(t, 8096, *cfg.Spec.JellyfinAuth.Port)
	assert.ElementsMatch(t, []string{"jellyfin-username", "jellyfin-password"}, res.Unfilled)

	require.NotNil(t, cfg.Spec.Sonarr)
	assert.Equal(t, "Sonarr", cfg.Spec.Sonarr.Name, "the default server that isn't 4K is exported")
	assert.Equal(t, "HD-1080p", cfg.Spec.Sonarr.ActiveProfileName)
	assert.Nil(t, cfg.Spec.Sonarr.ActiveProfileId, "the name is portable; the id is not")
	assert.Equal(t, "sonarr-key", res.Secret.StringData[cfg.Spec.Sonarr.APIKeySecretRef.Key])
	assert.Contains(t, res.Warnings, `Seerr has another sonarr server, "Sonarr 4K"; SeerrConfig holds one, so only "Sonarr" is exported`)
	assert.Nil(t, cfg.Spec.Radarr)

	require.Len(t, cfg.Spec.Notifications, 1)
	discord := cfg.Spec.Notifications[0]
	assert.Equal(t, "discord", discord.Agent)
	assert.Equal(t, 4094, *discord.Types)
	assert.Equal(t, map[string]string{"webhookUrl": "https://hook", "enableMentions": "true"}, discord.Options)
	assert.Contains(t, res.Warnings, `notification "discord": webhookUrl looks like a credential, and SeerrConfig holds options as plain text`)
}
