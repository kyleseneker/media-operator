//go:build compat

package compat

import (
	"context"
	"net/http"
	"net/url"
	"testing"

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

func TestQBittorrent(t *testing.T) {
	t.Parallel()
	qbit, password := startQBittorrent(t)
	proxy := newRecordingProxy(t, qbit.url())

	checkCompat(t, compatCase{
		fixture: "qbittorrent.yaml",
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
