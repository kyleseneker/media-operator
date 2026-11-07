//go:build compat

package compat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	curationv1alpha1 "github.com/kyleseneker/media-operator/api/curation/v1alpha1"
	jellyfinclient "github.com/kyleseneker/media-operator/internal/client/jellyfin"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/controller/curation"
	"github.com/kyleseneker/media-operator/internal/engine"
)

// TestMaintainerr sets up a fresh Maintainerr against Jellyfin and a Radarr, and
// creates a rule group that reads both.
func TestMaintainerr(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	jf := startJellyfin(t)
	jellyfinKey := jellyfinAPIKey(t, jf.url())
	radarr, radarrKey := startPVR(t, "radarr", radarrImage, 7878, "v3")

	maintainerr := startContainer(t, containerSpec{name: "maintainerr", image: maintainerrImage, port: 6246})
	waitFor(t, "maintainerr", 3*time.Minute, func() error {
		resp, err := http.Get(maintainerr.url() + "/api/health/ready")
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("status %d", resp.StatusCode)
		}
		return nil
	})
	fakes := startFakes(t)
	proxy := newRecordingProxy(t, maintainerr.url())

	checkCompat(t, compatCase{
		fixture: "maintainerr.yaml",
		export:  "maintainerr",
		vars: map[string]string{
			"URL": proxy.url(), "HOST": hostIP, "FAKES": fakes,
			"JELLYFIN_PORT": strconv.Itoa(jf.port), "RADARR_PORT": strconv.Itoa(radarr.port),
		},
		secrets: map[string]string{
			"jellyfin-api-key": jellyfinKey, "radarr-api-key": radarrKey, "seerr-api-key": randomKey(),
		},
		newObj: func() ctrlcommon.ConfigResource { return &curationv1alpha1.MaintainerrConfig{} },
		reconciler: func(c client.Client) reconcile.Reconciler {
			return &curation.MaintainerrConfigReconciler{Client: c, Scheme: c.Scheme(), Recorder: events.NewFakeRecorder(100)}
		},
		proxy:   proxy,
		observe: true,
		verify: func(t *testing.T) {
			hc := directClient(t, maintainerr.url(), "")
			settings, err := hc.GetJSON(ctx, "/api/settings")
			require.NoError(t, err)
			assert.Equal(t, "jellyfin", settings["media_server_type"])

			groups, err := hc.GetJSONList(ctx, "/api/rules")
			require.NoError(t, err)
			require.Len(t, groups, 1)
			group := groups[0]
			assert.Equal(t, "Stale unwatched movies", group["name"])
			assert.Equal(t, "movie", group["dataType"])
			collection, _ := group["collection"].(map[string]any)
			assert.EqualValues(t, 30, collection["deleteAfterDays"])
			assert.Equal(t, true, collection["cleanupLeftoverFolders"], "Maintainerr kept the leftover-folder cleanup")
			assert.NotNil(t, collection["radarrSettingsId"])
			rules, _ := group["rules"].([]any)
			require.Len(t, rules, 3)
			var last map[string]any
			require.NoError(t, json.Unmarshal([]byte(rules[2].(map[string]any)["ruleJson"].(string)), &last))
			assert.EqualValues(t, 1, last["operator"], "the second section joins with OR")
			assert.EqualValues(t, 1, last["section"])
		},
		cleanup: func(t *testing.T) {
			hc := directClient(t, maintainerr.url(), "")
			for _, path := range []string{"/api/rules", "/api/settings/radarr"} {
				items, err := hc.GetJSONList(ctx, path)
				require.NoError(t, err)
				assert.Empty(t, items, "%s still has what the operator created", path)
			}
		},
	})
}

// jellyfinAPIKey runs Jellyfin's setup and creates a Movies library and an API
// key for Maintainerr, which connects to Jellyfin by key only.
func jellyfinAPIKey(t *testing.T, base string) string {
	t.Helper()
	ctx := context.Background()
	password := randomKey()
	hc, err := engine.NewHTTPClient(base, engine.AuthMediaBrowser, engine.WithAppLabel("compat"))
	require.NoError(t, err)
	jc := jellyfinclient.NewClient(hc)
	require.NoError(t, jc.RunSetupWizard(ctx, jellyfinAdmin, password, "compat", "en", "US"))
	require.NoError(t, jc.Authenticate(ctx, jellyfinAdmin, password))
	require.NoError(t, jc.CreateLibrary(ctx, "Movies", "movies", map[string]any{
		"PathInfos": []map[string]any{{"Path": "/media/movies"}},
	}))
	_, err = hc.Do(ctx, http.MethodPost, "/Auth/Keys?app=maintainerr", nil)
	require.NoError(t, err)
	keys, err := hc.GetJSON(ctx, "/Auth/Keys")
	require.NoError(t, err)
	items, _ := keys["Items"].([]any)
	require.NotEmpty(t, items, "Jellyfin created no API key")
	token, _ := items[0].(map[string]any)["AccessToken"].(string)
	require.NotEmpty(t, token)
	return token
}
