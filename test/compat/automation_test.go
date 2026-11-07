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
			require.Len(t, downloaders, 1)
			assert.Equal(t, "QBITTORRENT", downloaders[0]["type"])
			networks, err := hc.GetJSONList(ctx, "/api/irc")
			require.NoError(t, err)
			require.Len(t, networks, 1)
			assert.Equal(t, map[string]any{"mechanism": "NONE"}, networks[0]["auth"])
			filters, err := hc.GetJSONList(ctx, "/api/filters")
			require.NoError(t, err)
			require.Len(t, filters, 1)
			actions, err := hc.GetJSONList(ctx, fmt.Sprintf("/api/actions?filter_id=%.0f", filters[0]["id"]))
			require.NoError(t, err)
			require.Len(t, actions, 1, "the filter's action was not created")
			assert.Equal(t, downloaders[0]["id"], actions[0]["client_id"])
		},
		cleanup: func(t *testing.T) {
			hc := autobrrClient(t, ab.url(), key)
			for _, path := range []string{"/api/downloaders", "/api/irc", "/api/filters"} {
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
