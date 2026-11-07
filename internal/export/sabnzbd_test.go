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

func TestSabnzbdExportsMiscServersAndCategories(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		if r.Form.Get("apikey") != "k" || r.Form.Get("mode") != "get_config" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{"config":{
			"misc":{"complete_dir":"/done","download_dir":"/incomplete","tmp_dir":"","script_dir":"/scripts",
				"bandwidth_max":"50M","pre_check":1,"pause_on_post_processing":false,"auto_sort":"avg_age asc"},
			"servers":[{"name":"Primary","host":"news","port":563,"ssl":1,"connections":20,"priority":0,"retention":3000,
				"enable":1,"username":"me","password":"**********"}],
			"categories":[{"name":"*","dir":"","script":"None","priority":0},{"name":"tv","dir":"tv","script":"","priority":-100}]}}`))
	}))
	t.Cleanup(srv.Close)

	res, err := Run(context.Background(), Options{App: "sabnzbd", URL: srv.URL, APIKey: "k"})
	require.NoError(t, err)
	cfg, ok := res.Config.(*downloadsv1alpha1.SabnzbdConfig)
	require.True(t, ok)
	assert.Equal(t, &downloadsv1alpha1.SabnzbdFolders{CompleteDir: "/done", IncompleteDir: "/incomplete", ScriptDir: "/scripts"}, cfg.Spec.Folders)
	assert.Equal(t, "50M", cfg.Spec.General.DownloadSpeedLimit)
	assert.True(t, *cfg.Spec.General.PreCheck)
	assert.False(t, *cfg.Spec.General.PauseOnPostProcessing)
	assert.True(t, *cfg.Spec.General.AutoSort)
	assert.Empty(t, cfg.Spec.General.ScriptDir, "script_dir is exported once, as a folder")

	require.Len(t, cfg.Spec.Servers, 1)
	server := cfg.Spec.Servers[0]
	assert.Equal(t, 563, server.Port)
	assert.True(t, *server.SSL)
	assert.Equal(t, 3000, *server.Retention)
	assert.Equal(t, "me", res.Secret.StringData[server.UsernameSecretRef.Key])
	assert.Equal(t, []string{"server-primary-password"}, res.Unfilled)

	assert.Equal(t, []downloadsv1alpha1.SabnzbdCategory{
		{Name: "*", Script: "None", Priority: new(0)},
		{Name: "tv", Dir: "tv", Priority: new(-100)},
	}, cfg.Spec.Categories)
}
