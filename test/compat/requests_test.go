//go:build compat

package compat

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	requestsv1alpha1 "github.com/kyleseneker/media-operator/api/requests/v1alpha1"
	jellyfinclient "github.com/kyleseneker/media-operator/internal/client/jellyfin"
	seerrclient "github.com/kyleseneker/media-operator/internal/client/seerr"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/controller/requests"
	"github.com/kyleseneker/media-operator/internal/engine"
)

// TestSeerr runs Seerr's first-time setup through Jellyfin, as a rebuild does:
// a fresh Seerr, a Jellyfin with one library, and Sonarr and Radarr to connect.
func TestSeerr(t *testing.T) {
	t.Parallel()
	jf := startJellyfin(t)
	jellyfinPassword := randomKey()
	hc, err := engine.NewHTTPClient(jf.url(), engine.AuthMediaBrowser, engine.WithAppLabel("compat"))
	require.NoError(t, err)
	jc := jellyfinclient.NewClient(hc)
	ctx := context.Background()
	require.NoError(t, jc.RunSetupWizard(ctx, jellyfinAdmin, jellyfinPassword, "compat", "en", "US"))
	require.NoError(t, jc.Authenticate(ctx, jellyfinAdmin, jellyfinPassword))
	require.NoError(t, jc.CreateLibrary(ctx, "Movies", "movies", map[string]any{
		"PathInfos": []map[string]any{{"Path": "/media/movies"}},
	}))

	sonarr, sonarrKey := startPVR(t, "sonarr", sonarrImage, 8989, "v3")
	radarr, radarrKey := startPVR(t, "radarr", radarrImage, 7878, "v3")
	for _, app := range []struct {
		c    *container
		key  string
		path string
	}{{sonarr, sonarrKey, "/data/media/tv"}, {radarr, radarrKey, "/data/media/movies"}} {
		app.c.exec(t, "mkdir -p "+app.path+" && chown -R abc:abc /data")
		root := map[string]any{"path": app.path}
		require.NoError(t, directClient(t, app.c.url(), app.key).PostJSON(ctx, "/api/v3/rootfolder", root))
	}

	seerr := startContainer(t, containerSpec{
		name: "seerr", image: seerrImage, port: 5055, env: map[string]string{"TZ": "Etc/UTC"},
	})
	waitFor(t, "seerr", 3*time.Minute, func() error {
		resp, err := http.Get(seerr.url() + "/api/v1/settings/public")
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return assert.AnError
		}
		return nil
	})
	fakes := startFakes(t)
	proxy := newRecordingProxy(t, seerr.url())
	seerrAPIKey := func(t *testing.T) string {
		hc, err := engine.NewHTTPClient(seerr.url(), engine.AuthSession, engine.WithAppLabel("compat"))
		require.NoError(t, err)
		sc := seerrclient.NewClient(hc)
		require.NoError(t, sc.AuthenticateJellyfin(ctx, jellyfinAdmin, jellyfinPassword, hostIP, jf.port, "", false, true))
		key, err := sc.GetAPIKey(ctx)
		require.NoError(t, err)
		return key
	}

	checkCompat(t, compatCase{
		fixture:      "seerr.yaml",
		export:       "seerr",
		exportAPIKey: seerrAPIKey,
		cleanup: func(t *testing.T) {
			hc := directClient(t, seerr.url(), seerrAPIKey(t))
			for _, kind := range []string{"sonarr", "radarr"} {
				servers, err := hc.GetJSONList(ctx, "/api/v1/settings/"+kind)
				require.NoError(t, err)
				assert.Empty(t, servers, "Seerr still has the %s connection the operator created", kind)
			}
		},
		vars: map[string]string{
			"URL": proxy.url(), "HOST": hostIP, "FAKES": fakes, "JELLYFIN_PORT": strconv.Itoa(jf.port),
			"SONARR_PORT": strconv.Itoa(sonarr.port), "RADARR_PORT": strconv.Itoa(radarr.port),
		},
		secrets: map[string]string{
			"jellyfin-username": jellyfinAdmin, "jellyfin-password": jellyfinPassword,
			"sonarr-api-key": sonarrKey, "radarr-api-key": radarrKey,
		},
		newObj: func() ctrlcommon.ConfigResource { return &requestsv1alpha1.SeerrConfig{} },
		reconciler: func(c client.Client) reconcile.Reconciler {
			return &requests.SeerrConfigReconciler{Client: c, Scheme: c.Scheme(), Recorder: events.NewFakeRecorder(100)}
		},
		proxy:   proxy,
		observe: true,
		// Signing in through Jellyfin is a POST; after setup it changes nothing.
		read: func(r request) bool {
			return getOnly(r) || (r.Method == http.MethodPost && r.Path == "/api/v1/auth/jellyfin")
		},
		verify: func(t *testing.T) {
			resp, err := http.Get(seerr.url() + "/api/v1/settings/public")
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()
			var public struct {
				Initialized      bool   `json:"initialized"`
				ApplicationTitle string `json:"applicationTitle"`
			}
			require.NoError(t, jsonDecode(resp, &public))
			assert.True(t, public.Initialized, "first-time setup did not complete")
			assert.Equal(t, "Compat Requests", public.ApplicationTitle)

			server, err := directClient(t, seerr.url(), seerrAPIKey(t)).GetJSON(ctx, "/api/v1/settings/jellyfin")
			require.NoError(t, err)
			assert.Equal(t, hostIP, server["ip"])
			assert.Equal(t, float64(jf.port), server["port"])
			assert.NotContains(t, server, "hostname", "Seerr's settings migration rewrites the port from a stored hostname")
		},
	})
}
