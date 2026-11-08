package export

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/intstr"

	curationv1alpha1 "github.com/kyleseneker/media-operator/api/curation/v1alpha1"
)

var fakeMaintainerrAPI = map[string]string{
	"/api/settings":          `{"media_server_type":"jellyfin","rules_handler_job_cron":"0 3 * * *","collection_handler_job_cron":""}`,
	"/api/settings/jellyfin": `{"jellyfin_url":"http://jellyfin:8096","jellyfin_api_key":"jf-key","jellyfin_user_id":""}`,
	"/api/settings/sonarr":   `[]`,
	"/api/settings/radarr": `[{"id":1,"serverName":"Radarr","url":"http://radarr:7878","apiKey":"radarr-key",
		"tagExclusions":true,"exclusionTag":"keep"}]`,
	"/api/settings/seerr":            `{"url":"http://seerr:5055","api_key":"see...key"}`,
	"/api/settings/tautulli":         `{"url":"","api_key":""}`,
	"/api/servarr/radarr/1/profiles": `[{"id":4,"name":"HD-1080p"}]`,
	"/api/media-server/libraries":    `[{"id":"lib1","title":"Movies","type":"movie"}]`,
	"/api/notifications/configurations": `[{"id":7,"name":"Discord","agent":"discord","enabled":true,"types":[16,8,2048],
		"aboutScale":3,"options":{"webhookUrl":"https://discord.example/hook","botUsername":"Maintainerr","botAvatarUrl":""}}]`,
	"/api/rules/constants": `{"applications":[{"id":0,"name":"Jellyfin","props":[{"id":1,"name":"addDate"},{"id":2,"name":"viewCount"}]},
		{"id":1,"name":"Radarr","props":[{"id":5,"name":"monitored"}]}]}`,
	"/api/rules": `[{"id":3,"name":"Stale","description":"","libraryId":"lib1","dataType":"movie","isActive":true,
		"ruleHandlerCronSchedule":"","notifications":[{"id":7,"name":"Discord"}],"collection":{"arrAction":7,"deleteAfterDays":30,"radarrSettingsId":1,
		"radarrQualityProfileId":4,"listExclusions":true,"visibleOnHome":false},
		"rules":[
			{"id":11,"ruleJson":"{\"operator\":null,\"action\":5,\"firstVal\":[0,1],\"customVal\":{\"ruleTypeId\":0,\"value\":\"15552000\"},\"section\":0}"},
			{"id":12,"ruleJson":"{\"operator\":0,\"action\":2,\"firstVal\":[0,2],\"customVal\":{\"ruleTypeId\":0,\"value\":\"0\"},\"section\":0}"},
			{"id":13,"ruleJson":"{\"operator\":1,\"action\":2,\"firstVal\":[1,5],\"customVal\":{\"ruleTypeId\":3,\"value\":\"0\"},\"section\":1}"}]}]`,
}

func TestMaintainerrExportsConnectionsAndRuleGroups(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := fakeMaintainerrAPI[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	res, err := Run(context.Background(), Options{App: "maintainerr", URL: srv.URL})
	require.NoError(t, err)
	cfg, ok := res.Config.(*curationv1alpha1.MaintainerrConfig)
	require.True(t, ok)

	require.NotNil(t, cfg.Spec.MediaServer.Jellyfin)
	assert.Equal(t, "jf-key", res.Secret.StringData[cfg.Spec.MediaServer.Jellyfin.APIKeySecretRef.Key])
	require.Len(t, cfg.Spec.Radarr, 1)
	assert.Equal(t, "keep", cfg.Spec.Radarr[0].ExclusionTag)
	assert.Equal(t, "radarr-key", res.Secret.StringData[cfg.Spec.Radarr[0].APIKeySecretRef.Key])
	require.NotNil(t, cfg.Spec.Seerr)
	assert.Contains(t, res.Unfilled, cfg.Spec.Seerr.APIKeySecretRef.Key, "a masked key is left to fill in")
	assert.Nil(t, cfg.Spec.Tautulli)
	assert.Equal(t, "0 3 * * *", cfg.Spec.Jobs.Rules)

	require.Len(t, cfg.Spec.RuleGroups, 1)
	g := cfg.Spec.RuleGroups[0]
	assert.Equal(t, "Movies", g.Library)
	assert.Equal(t, "changeQualityProfile", g.ArrAction)
	assert.Equal(t, int32(30), *g.ActionAfterDays)
	assert.Equal(t, "Radarr", g.RadarrServer)
	assert.Equal(t, "HD-1080p", g.QualityProfile)
	require.Len(t, g.Sections, 2)
	assert.Empty(t, g.Sections[0].Operator)
	assert.Equal(t, "OR", g.Sections[1].Operator)
	first := g.Sections[0].Rules[0]
	assert.Equal(t, "Jellyfin.addDate", first.FirstValue)
	assert.Equal(t, "BEFORE", first.Action)
	assert.Equal(t, &curationv1alpha1.MaintainerrCustomValue{Type: "custom_days", Value: intstr.FromInt32(180)}, first.CustomValue)
	assert.Equal(t, "AND", g.Sections[0].Rules[1].Operator)
	assert.Equal(t, intstr.FromInt32(0), g.Sections[0].Rules[1].CustomValue.Value)
	last := g.Sections[1].Rules[0]
	assert.Empty(t, last.Operator, "the section carries the operator joining it")
	assert.Equal(t, "Radarr.monitored", last.FirstValue)
	assert.Equal(t, &curationv1alpha1.MaintainerrCustomValue{Type: "boolean", Value: intstr.FromString("false")}, last.CustomValue)
	assert.Equal(t, []string{"Discord"}, g.Notifications)

	require.Len(t, cfg.Spec.Notifications, 1)
	n := cfg.Spec.Notifications[0]
	assert.Equal(t, "discord", n.Agent)
	assert.Equal(t, []string{"mediaAboutToBeHandled", "mediaHandled"}, n.Types)
	assert.Contains(t, res.Warnings, `notification "Discord": MaintainerrConfig can't express notification type 2048, so it isn't exported`)
	assert.Equal(t, int32(3), *n.AboutScale)
	assert.Equal(t, "Maintainerr", n.Options["botUsername"].ToInterface())
	assert.NotContains(t, n.Options, "botAvatarUrl", "an empty option isn't exported")
	require.Contains(t, n.SecretOptions, "webhookUrl")
	assert.Equal(t, "https://discord.example/hook", res.Secret.StringData[n.SecretOptions["webhookUrl"].Key])
}
