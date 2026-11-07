//go:build compat

package compat

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	subtitlesv1alpha1 "github.com/kyleseneker/media-operator/api/subtitles/v1alpha1"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/controller/subtitles"
	"github.com/kyleseneker/media-operator/internal/engine"
)

func TestBazarr(t *testing.T) {
	t.Parallel()
	bazarr := startContainer(t, containerSpec{name: "bazarr", image: bazarrImage, port: 6767, env: linuxserverEnv})
	sonarr, sonarrKey := startPVR(t, "sonarr", sonarrImage, 8989, "v3")
	radarr, radarrKey := startPVR(t, "radarr", radarrImage, 7878, "v3")

	var key string
	waitFor(t, "bazarr api key", 3*time.Minute, func() error {
		out, err := runTool("exec", bazarr.id, "sh", "-c",
			`sed -n '/^auth:/,/^[^ ]/p' /config/config/config.yaml | sed -n 's/^  apikey: *//p'`)
		if err != nil || out == "" {
			return fmt.Errorf("apikey not written yet: %v", err)
		}
		key = out
		return nil
	})
	hc := bazarrClient(t, bazarr.url(), key)
	waitFor(t, "bazarr", 3*time.Minute, func() error {
		_, err := hc.GetJSON(context.Background(), "/api/system/settings")
		return err
	})
	proxy := newRecordingProxy(t, bazarr.url())

	checkCompat(t, compatCase{
		fixture: "bazarr.yaml",
		export:  "bazarr",
		vars: map[string]string{
			"URL": proxy.url(), "HOST": hostIP,
			"SONARR_PORT": strconv.Itoa(sonarr.port), "RADARR_PORT": strconv.Itoa(radarr.port),
		},
		secrets: map[string]string{"api-key": key, "sonarr-api-key": sonarrKey, "radarr-api-key": radarrKey},
		newObj:  func() ctrlcommon.ConfigResource { return &subtitlesv1alpha1.BazarrConfig{} },
		reconciler: func(c client.Client) reconcile.Reconciler {
			return &subtitles.BazarrConfigReconciler{Client: c, Scheme: c.Scheme(), Recorder: events.NewFakeRecorder(100)}
		},
		proxy:   proxy,
		observe: true,
		verify: func(t *testing.T) {
			settings, err := hc.GetJSON(context.Background(), "/api/system/settings")
			require.NoError(t, err)
			general, _ := settings["general"].(map[string]any)
			assert.Equal(t, true, general["use_sonarr"])
			assert.Equal(t, float64(90), general["minimum_score"])
			sonarrSettings, _ := settings["sonarr"].(map[string]any)
			assert.Equal(t, hostIP, sonarrSettings["ip"])
			assert.Contains(t, general["enabled_providers"], "podnapisi")
		},
	})
}

// bazarrClient talks to Bazarr directly, outside the operator.
func bazarrClient(t *testing.T, base, key string) *engine.HTTPClient {
	t.Helper()
	hc, err := engine.NewHTTPClient(base, engine.AuthAPIKey, engine.WithAPIKey(key),
		engine.WithAPIKeyHeader("X-API-KEY"), engine.WithAppLabel("compat"))
	require.NoError(t, err)
	return hc
}
