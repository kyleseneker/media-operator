//go:build compat

package compat

import (
	"context"
	"net/http"
	"net/url"
	"testing"

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
			if err != nil {
				t.Fatal(err)
			}
			if prefs["max_ratio"] != 2.5 || prefs["dht"] != false || prefs["save_path"] != "/downloads/complete" {
				t.Errorf("preferences did not reach qBittorrent: max_ratio=%v dht=%v save_path=%v",
					prefs["max_ratio"], prefs["dht"], prefs["save_path"])
			}
			categories, err := hc.GetJSON(context.Background(), "/api/v2/torrents/categories")
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := categories["movies"]; !ok {
				t.Errorf("categories = %v", categories)
			}
		},
	})
}

// qbittorrentClient logs in to the WebUI directly for verification.
func qbittorrentClient(t *testing.T, base, password string) *engine.HTTPClient {
	t.Helper()
	hc, err := engine.NewHTTPClient(base, engine.AuthNone, engine.WithAppLabel("compat"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hc.DoForm(context.Background(), "/api/v2/auth/login",
		url.Values{"username": {qbittorrentUser}, "password": {password}}); err != nil {
		t.Fatal(err)
	}
	name, value := hc.CookieValuePrefix("QBT_SID_")
	if value == "" {
		name, value = "SID", hc.CookieValue("SID")
	}
	hc.SetCookieSession(name, value)
	return hc
}
