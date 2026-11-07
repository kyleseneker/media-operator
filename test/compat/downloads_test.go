//go:build compat

package compat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	downloadsv1alpha1 "github.com/kyleseneker/media-operator/api/downloads/v1alpha1"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/controller/downloads"
	"github.com/kyleseneker/media-operator/internal/engine"
)

func TestSabnzbd(t *testing.T) {
	t.Parallel()
	sab := startContainer(t, containerSpec{name: "sabnzbd", image: sabnzbdImage, port: 8080, env: linuxserverEnv})
	var key string
	waitFor(t, "sabnzbd api key", 2*time.Minute, func() error {
		out, err := runTool("exec", sab.id, "sh", "-c", `sed -n 's/^api_key = //p' /config/sabnzbd.ini`)
		if err != nil || out == "" {
			return fmt.Errorf("api_key not written yet: %v", err)
		}
		key = out
		return nil
	})
	sab.exec(t, "mkdir -p /downloads/complete /downloads/incomplete /downloads/nzb-backup && chown -R abc:abc /downloads")
	waitFor(t, "sabnzbd", 2*time.Minute, func() error {
		_, err := sabnzbdGet(sab.url(), key, "version")
		return err
	})
	proxy := newRecordingProxy(t, sab.url())

	checkCompat(t, compatCase{
		fixture: "sabnzbd.yaml",
		export:  "sabnzbd",
		vars:    map[string]string{"URL": proxy.url()},
		secrets: map[string]string{"api-key": key, "usenet-username": "compat", "usenet-password": "usenet-secret"},
		newObj:  func() ctrlcommon.ConfigResource { return &downloadsv1alpha1.SabnzbdConfig{} },
		reconciler: func(c client.Client) reconcile.Reconciler {
			return &downloads.SabnzbdConfigReconciler{Client: c, Scheme: c.Scheme(), Recorder: events.NewFakeRecorder(100)}
		},
		proxy: proxy,
		// Every call is a form POST to /api; the mode says whether it writes.
		read: func(r request) bool {
			values, _ := url.ParseQuery(r.Body)
			mode := values.Get("mode")
			return getOnly(r) || mode == "version" || mode == "get_config"
		},
		observe: true,
		verify: func(t *testing.T) {
			cfg := sabnzbdConfigOf(t, sab.url(), key)
			assert.Equal(t, "/downloads/complete", cfg.Misc["complete_dir"])
			assert.Equal(t, "50M", cfg.Misc["bandwidth_max"])
			require.Len(t, cfg.Servers, 1)
			assert.Equal(t, "news.example.com", cfg.Servers[0]["host"])
			names := make([]any, 0, len(cfg.Categories))
			for _, c := range cfg.Categories {
				names = append(names, c["name"])
			}
			assert.Subset(t, names, []any{"tv", "movies"})
		},
		cleanup: func(t *testing.T) {
			cfg := sabnzbdConfigOf(t, sab.url(), key)
			assert.Empty(t, cfg.Servers, "servers the operator created are still there")
			for _, c := range cfg.Categories {
				assert.NotContains(t, []any{"tv", "movies"}, c["name"], "a category the operator created is still there")
			}
		},
	})
}

type sabnzbdSections struct {
	Misc       map[string]any   `json:"misc"`
	Servers    []map[string]any `json:"servers"`
	Categories []map[string]any `json:"categories"`
}

func sabnzbdConfigOf(t *testing.T, base, key string) sabnzbdSections {
	t.Helper()
	raw, err := sabnzbdGet(base, key, "get_config")
	require.NoError(t, err)
	var out struct {
		Config sabnzbdSections `json:"config"`
	}
	require.NoError(t, json.Unmarshal(raw, &out))
	return out.Config
}

// sabnzbdGet calls the API directly, outside the operator.
func sabnzbdGet(base, key, mode string) ([]byte, error) {
	resp, err := http.Get(base + "/api?output=json&apikey=" + key + "&mode=" + mode)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK || bytes.Contains(body, []byte(`"error"`)) {
		return nil, fmt.Errorf("%s: %d %s", mode, resp.StatusCode, body)
	}
	return body, nil
}

func TestQBittorrent(t *testing.T) {
	t.Parallel()
	qbit, password := startQBittorrent(t)
	proxy := newRecordingProxy(t, qbit.url())

	checkCompat(t, compatCase{
		fixture: "qbittorrent.yaml",
		export:  "qbittorrent",
		vars:    map[string]string{"URL": proxy.url()},
		secrets: map[string]string{"username": qbittorrentUser, "password": password},
		newObj:  func() ctrlcommon.ConfigResource { return &downloadsv1alpha1.QBittorrentConfig{} },
		reconciler: func(c client.Client) reconcile.Reconciler {
			return &downloads.QBittorrentConfigReconciler{Client: c, Scheme: c.Scheme(), Recorder: events.NewFakeRecorder(100)}
		},
		proxy: proxy,
		// The WebUI authenticates with a POST; it changes no settings.
		read: func(r request) bool {
			return getOnly(r) || (r.Method == http.MethodPost && r.Path == "/api/v2/auth/login")
		},
		observe: true,
		verify: func(t *testing.T) {
			hc := qbittorrentClient(t, qbit.url(), password)
			prefs, err := hc.GetJSON(context.Background(), "/api/v2/app/preferences")
			require.NoError(t, err)
			assert.Equal(t, 2.5, prefs["max_ratio"])
			assert.Equal(t, false, prefs["dht"])
			assert.Equal(t, "/downloads/complete", prefs["save_path"])
			categories, err := hc.GetJSON(context.Background(), "/api/v2/torrents/categories")
			require.NoError(t, err)
			assert.Contains(t, categories, "movies")
		},
	})
}

// qbittorrentClient logs in to the WebUI directly for verification.
func qbittorrentClient(t *testing.T, base, password string) *engine.HTTPClient {
	t.Helper()
	hc, err := engine.NewHTTPClient(base, engine.AuthNone, engine.WithAppLabel("compat"))
	require.NoError(t, err)
	_, err = hc.DoForm(context.Background(), "/api/v2/auth/login",
		url.Values{"username": {qbittorrentUser}, "password": {password}})
	require.NoError(t, err)
	name, value := hc.CookieValuePrefix("QBT_SID_")
	if value == "" {
		name, value = "SID", hc.CookieValue("SID")
	}
	hc.SetCookieSession(name, value)
	return hc
}
