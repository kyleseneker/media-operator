//go:build compat

package compat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	automationv1alpha1 "github.com/kyleseneker/media-operator/api/automation/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/controller/automation"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
)

func TestAutobrr(t *testing.T) {
	t.Parallel()
	ab := startContainer(t, containerSpec{name: "autobrr", image: autobrrImage, port: 7474})
	key := autobrrAPIKey(t, ab.url())
	qbit, qbitPassword := startQBittorrent(t)
	proxy := newRecordingProxy(t, ab.url())

	checkCompat(t, compatCase{
		fixture: "autobrr.yaml",
		export:  "autobrr",
		vars: map[string]string{
			"URL": proxy.url(), "HOST": hostIP, "QBITTORRENT_PORT": strconv.Itoa(qbit.port),
		},
		secrets: map[string]string{
			"api-key": key, "qbittorrent-username": qbittorrentUser, "qbittorrent-password": qbitPassword,
			"sonarr-api-key": randomKey(), "ipt-passkey": randomKey(), "torznab-api-key": randomKey(),
		},
		newObj: func() ctrlcommon.ConfigResource { return &automationv1alpha1.AutobrrConfig{} },
		reconciler: func(c client.Client) reconcile.Reconciler {
			return &automation.AutobrrConfigReconciler{Client: c, Scheme: c.Scheme(), Recorder: events.NewFakeRecorder(100)}
		},
		proxy:   proxy,
		observe: true,
		verify: func(t *testing.T) {
			hc := autobrrClient(t, ab.url(), key)
			ctx := context.Background()
			downloaders, err := hc.GetJSONList(ctx, "/api/downloaders")
			require.NoError(t, err)
			require.Len(t, downloaders, 2)
			assert.Equal(t, "QBITTORRENT", named(downloaders, "qBittorrent")["type"])
			sonarr, _ := named(downloaders, "Sonarr")["settings"].(map[string]any)
			assert.Equal(t, "<redacted>", sonarr["apikey"], "the Sonarr client has no API key")
			indexers, err := hc.GetJSONList(ctx, "/api/indexer")
			require.NoError(t, err)
			require.Len(t, indexers, 2)
			ipt, err := hc.GetJSON(ctx, fmt.Sprintf("/api/indexer/%.0f", named(indexers, "IPTorrents")["id"]))
			require.NoError(t, err)
			assert.Equal(t, "ipt", ipt["identifier"])
			assert.Equal(t, "https://iptorrents.com/", ipt["base_url"])
			iptSettings, _ := ipt["settings"].(map[string]any)
			assert.Equal(t, "<redacted>", iptSettings["passkey"], "the passkey did not reach Autobrr")
			assert.Equal(t, "torznab", named(indexers, "Compat Torznab")["implementation"])
			feeds, err := hc.GetJSONList(ctx, "/api/feeds")
			require.NoError(t, err)
			require.Len(t, feeds, 1)
			feedIndexer, _ := feeds[0]["indexer"].(map[string]any)
			assert.Equal(t, "Compat Torznab", feedIndexer["name"])
			networks, err := hc.GetJSONList(ctx, "/api/irc")
			require.NoError(t, err)
			require.Len(t, networks, 1)
			assert.Equal(t, map[string]any{"mechanism": "NONE"}, networks[0]["auth"])
			filters, err := hc.GetJSONList(ctx, "/api/filters")
			require.NoError(t, err)
			require.Len(t, filters, 1)
			filter, err := hc.GetJSON(ctx, fmt.Sprintf("/api/filters/%.0f", filters[0]["id"]))
			require.NoError(t, err)
			assert.Equal(t, "tv,hd", filter["tags"])
			filterIndexers, _ := filter["indexers"].([]any)
			require.Len(t, filterIndexers, 1, "the filter is not linked to its indexer")
			actions, err := hc.GetJSONList(ctx, fmt.Sprintf("/api/actions?filter_id=%.0f", filters[0]["id"]))
			require.NoError(t, err)
			require.Len(t, actions, 1, "the filter's action was not created")
			assert.Equal(t, named(downloaders, "qBittorrent")["id"], actions[0]["client_id"])
			assert.Equal(t, []any{"DV"}, filter["match_hdr"])
			assert.Equal(t, "cam,ts", filter["except_tags"])
			assert.Equal(t, "DAY", filter["max_downloads_unit"])
			assert.InDelta(t, 1.5, actions[0]["limit_ratio"], 0)
			assert.Equal(t, "SUBFOLDER_CREATE", actions[0]["content_layout"])
		},
		cleanup: func(t *testing.T, _ map[string][]string) {
			hc := autobrrClient(t, ab.url(), key)
			for _, path := range []string{"/api/downloaders", "/api/indexer", "/api/feeds", "/api/irc", "/api/filters"} {
				items, err := hc.GetJSONList(context.Background(), path)
				require.NoError(t, err)
				assert.Empty(t, items, "%s still has what the operator created", path)
			}
		},
	})
}

func autobrrClient(t *testing.T, url, key string) *engine.HTTPClient {
	t.Helper()
	hc, err := engine.NewHTTPClient(url, engine.AuthAPIKey, engine.WithAPIKey(key),
		engine.WithAPIKeyHeader("X-API-Token"), engine.WithAppLabel("compat"))
	require.NoError(t, err)
	return hc
}

// autobrrAPIKey onboards a fresh Autobrr, signs in and creates an API key, the
// steps its web UI takes on first start.
func autobrrAPIKey(t *testing.T, base string) string {
	t.Helper()
	waitFor(t, "autobrr", 2*time.Minute, func() error {
		resp, err := http.Get(base + "/api/healthz/liveness")
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("liveness %d", resp.StatusCode)
		}
		return nil
	})
	creds := []byte(`{"username":"compat","password":"compat-password-123"}`)
	post := func(c *http.Client, path string, body []byte) *http.Response {
		resp, err := c.Post(base+path, "application/json", bytes.NewReader(body))
		require.NoError(t, err)
		require.Less(t, resp.StatusCode, 300, "%s answered %d", path, resp.StatusCode)
		return resp
	}
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	c := &http.Client{Jar: jar}
	_ = post(c, "/api/auth/onboard", creds).Body.Close()
	_ = post(c, "/api/auth/login", creds).Body.Close()
	resp := post(c, "/api/keys", []byte(`{"name":"compat","scopes":[]}`))
	defer func() { _ = resp.Body.Close() }()
	var created struct{ Key string }
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&created))
	require.NotEmpty(t, created.Key)
	return created.Key
}

func named(items []map[string]any, name string) map[string]any {
	for _, item := range items {
		if item["name"] == name {
			return item
		}
	}
	return nil
}
