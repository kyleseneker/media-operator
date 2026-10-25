//go:build integration

// Package integration checks real APIs on explicitly supplied disposable apps.
package integration

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	common "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	indexers "github.com/kyleseneker/media-operator/api/indexers/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/client/prowlarr"
	"github.com/kyleseneker/media-operator/internal/client/pvr"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/stretchr/testify/require"
)

func requiredEnv(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	require.NotEmpty(t, value, "set %s for a disposable application", name)
	return value
}

func appClient(t *testing.T, app string) *engine.HTTPClient {
	t.Helper()
	require.Equal(t, "1", os.Getenv("MEDIA_OPERATOR_TEST_DISPOSABLE"),
		"these tests mutate application configuration; explicitly opt in using disposable apps")
	prefix := "MEDIA_TEST_" + strings.ToUpper(app)
	client, err := engine.NewHTTPClient(requiredEnv(t, prefix+"_URL"), engine.AuthAPIKey,
		engine.WithAPIKey(requiredEnv(t, prefix+"_API_KEY")), engine.WithAppLabel(app),
		// Disposable local applications may be reached through loopback port forwards.
		engine.WithTransport(&http.Transport{}))
	require.NoError(t, err)
	status, err := client.GetJSON(context.Background(), "/api/"+apiVersion(app)+"/system/status")
	require.NoError(t, err)
	t.Logf("%s version: %v", app, status["version"])
	return client
}

func apiVersion(app string) string {
	if app == "prowlarr" {
		return "v1"
	}
	return "v3"
}

func TestRemoteMappingsOnDisposableApps(t *testing.T) {
	for _, app := range []string{"sonarr", "radarr"} {
		t.Run(app, func(t *testing.T) {
			hc := appClient(t, app)
			ctx := context.Background()
			suffix := fmt.Sprintf("operator-test-%d", time.Now().UnixNano())
			local := requiredEnv(t, "MEDIA_TEST_"+strings.ToUpper(app)+"_LOCAL_PATH")
			opts := pvr.Options{RemotePathMappings: []common.RemotePathMapping{
				{Host: suffix, RemotePath: "/downloads", LocalPath: local},
			}}
			// Cleanup is installed first so even a successful write followed by a failed
			// read does not leave a test mapping behind. The unique hostname is test-owned.
			t.Cleanup(func() {
				items, err := hc.GetJSONList(ctx, "/api/v3/remotepathmapping")
				require.NoError(t, err)
				for _, item := range items {
					if item["host"] == suffix {
						require.NoError(t, hc.DeleteJSON(ctx, fmt.Sprintf("/api/v3/remotepathmapping/%.0f", item["id"])))
					}
				}
			})
			result, err := pvr.Reconcile(ctx, hc, "v3", struct{}{}, opts, engine.SyncPolicy{}, nil)
			require.NoError(t, err)
			require.Empty(t, result.Errors)
			require.Len(t, result.Managed["remotePathMappings"], 1)
			items, err := hc.GetJSONList(ctx, "/api/v3/remotepathmapping")
			require.NoError(t, err)
			var created map[string]any
			for _, item := range items {
				if item["host"] == suffix {
					created = item
				}
			}
			require.NotNil(t, created)
			require.Equal(t, "/downloads/", created["remotePath"])
			require.Equal(t, strings.TrimRight(local, "/")+"/", created["localPath"])
			second, err := pvr.Reconcile(ctx, hc, "v3", struct{}{}, opts, engine.SyncPolicy{}, result.Managed)
			require.NoError(t, err)
			require.Empty(t, second.Errors)
			require.Equal(t, result.Managed, second.Managed)
			items, err = hc.GetJSONList(ctx, "/api/v3/remotepathmapping")
			require.NoError(t, err)
			count := 0
			for _, item := range items {
				if item["host"] == suffix {
					count++
					require.Equal(t, created["id"], item["id"])
				}
			}
			require.Equal(t, 1, count, "second pass must reuse the original mapping")
		})
	}
}

func TestProwlarrProfileRecreation(t *testing.T) {
	hc := appClient(t, "prowlarr")
	ctx := context.Background()
	name := fmt.Sprintf("operator-test-%d", time.Now().UnixNano())
	enabled := true
	seeders := 2
	spec := indexers.ProwlarrConfigSpec{
		AppProfiles: []indexers.ProwlarrAppProfile{{
			Name: name, EnableRss: &enabled, EnableAutomaticSearch: &enabled,
			EnableInteractiveSearch: &enabled, MinimumSeeders: &seeders,
		}},
		Indexers: []indexers.ProwlarrIndexer{{Name: "reference-check", AppProfileName: name}},
	}
	t.Cleanup(func() {
		ids, err := engine.FetchNamedIDs(ctx, hc, "/api/v1/appprofile", "name")
		require.NoError(t, err)
		for _, id := range ids[name] {
			require.NoError(t, hc.DeleteJSON(ctx, fmt.Sprintf("/api/v1/appprofile/%d", id)))
		}
	})
	previous := 0
	for range 2 {
		resources := prowlarr.ProwlarrResources(prowlarr.ProwlarrOptions{AppProfiles: spec.AppProfiles}, nil)
		result := engine.ReconcileApp(ctx, hc, prowlarr.ProwlarrDefinition(), nil, resources, engine.SyncPolicy{}, nil)
		require.Empty(t, result.Errors)
		resolved, _, err := prowlarr.ResolveReferences(ctx, hc, spec)
		require.NoError(t, err)
		require.NotNil(t, resolved.Indexers[0].AppProfileId)
		id := *resolved.Indexers[0].AppProfileId
		require.Positive(t, id)
		if previous != 0 {
			require.NotEqual(t, previous, id, "recreated profile should receive a new ID")
		}
		profile, err := hc.GetJSON(ctx, fmt.Sprintf("/api/v1/appprofile/%d", id))
		require.NoError(t, err)
		require.Equal(t, true, profile["enableRss"])
		require.Equal(t, float64(2), profile["minimumSeeders"])
		require.NoError(t, hc.DeleteJSON(ctx, fmt.Sprintf("/api/v1/appprofile/%d", id)))
		previous = id
	}
}
