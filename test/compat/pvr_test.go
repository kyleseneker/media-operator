//go:build compat

package compat

import (
	"context"
	"strconv"
	"testing"

	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pvrv1alpha1 "github.com/kyleseneker/media-operator/api/pvr/v1alpha1"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/controller/pvr"
)

func TestSonarr(t *testing.T) {
	t.Parallel()
	sonarr, key := startPVR(t, "sonarr", sonarrImage, 8989, "v3")
	sonarr.exec(t, "mkdir -p /data/media/tv && chown -R abc:abc /data")
	qbit, qbitPassword := startQBittorrent(t)
	fakes := startFakes(t)
	proxy := newRecordingProxy(t, sonarr.url())

	checkCompat(t, compatCase{
		fixture: "sonarr.yaml",
		vars: map[string]string{
			"URL": proxy.url(), "HOST": hostIP, "FAKES": fakes,
			"QBITTORRENT_PORT": strconv.Itoa(qbit.port),
		},
		secrets: map[string]string{
			"api-key": key, "qbittorrent-username": qbittorrentUser, "qbittorrent-password": qbitPassword,
		},
		newObj: func() ctrlcommon.ConfigResource { return &pvrv1alpha1.SonarrConfig{} },
		reconciler: func(c client.Client) reconcile.Reconciler {
			return &pvr.SonarrConfigReconciler{Client: c, Scheme: c.Scheme(), Recorder: events.NewFakeRecorder(100)}
		},
		proxy:   proxy,
		observe: true,
		verify: func(t *testing.T) {
			hc := directClient(t, sonarr.url(), key)
			naming, err := hc.GetJSON(context.Background(), "/api/v3/config/naming")
			if err != nil {
				t.Fatal(err)
			}
			if naming["seasonFolderFormat"] != "Season {season:00}" {
				t.Errorf("seasonFolderFormat = %v", naming["seasonFolderFormat"])
			}
			requireNamed(t, hc, "/api/v3/downloadclient", "qBittorrent")
			requireNamed(t, hc, "/api/v3/indexer", "Compat Torznab")
			requireNamed(t, hc, "/api/v3/qualityprofile", "HD-1080p")
			requireNamed(t, hc, "/api/v3/notification", "Compat Webhook")
			lists, err := hc.GetJSONList(context.Background(), "/api/v3/importlist")
			if err != nil {
				t.Fatal(err)
			}
			if len(lists) != 1 || lists[0]["shouldMonitor"] != "all" {
				t.Errorf("import list monitor did not reach Sonarr: %v", lists)
			}
		},
	})
}

func TestRadarr(t *testing.T) {
	t.Parallel()
	radarr, key := startPVR(t, "radarr", radarrImage, 7878, "v3")
	radarr.exec(t, "mkdir -p /data/media/movies && chown -R abc:abc /data")
	qbit, qbitPassword := startQBittorrent(t)
	fakes := startFakes(t)
	proxy := newRecordingProxy(t, radarr.url())

	checkCompat(t, compatCase{
		fixture: "radarr.yaml",
		vars: map[string]string{
			"URL": proxy.url(), "HOST": hostIP, "FAKES": fakes,
			"QBITTORRENT_PORT": strconv.Itoa(qbit.port), "RADARR_LIST": "RadarrListImport",
		},
		secrets: map[string]string{
			"api-key": key, "qbittorrent-username": qbittorrentUser, "qbittorrent-password": qbitPassword,
		},
		newObj: func() ctrlcommon.ConfigResource { return &pvrv1alpha1.RadarrConfig{} },
		reconciler: func(c client.Client) reconcile.Reconciler {
			return &pvr.RadarrConfigReconciler{Client: c, Scheme: c.Scheme(), Recorder: events.NewFakeRecorder(100)}
		},
		proxy:   proxy,
		observe: true,
		verify: func(t *testing.T) {
			hc := directClient(t, radarr.url(), key)
			naming, err := hc.GetJSON(context.Background(), "/api/v3/config/naming")
			if err != nil {
				t.Fatal(err)
			}
			if naming["colonReplacementFormat"] != "smart" {
				t.Errorf("colonReplacementFormat = %v", naming["colonReplacementFormat"])
			}
			requireNamed(t, hc, "/api/v3/downloadclient", "qBittorrent")
			requireNamed(t, hc, "/api/v3/indexer", "Compat Torznab")
			requireNamed(t, hc, "/api/v3/qualityprofile", "HD-1080p")
			requireNamed(t, hc, "/api/v3/notification", "Compat Webhook")
			lists, err := hc.GetJSONList(context.Background(), "/api/v3/importlist")
			if err != nil {
				t.Fatal(err)
			}
			if len(lists) != 1 || lists[0]["monitor"] != "movieOnly" || lists[0]["enabled"] != true {
				t.Errorf("import list monitor/enabled did not reach Radarr: %v", lists)
			}
		},
	})
}

func requireNamed(t *testing.T, hc interface {
	GetJSONList(context.Context, string) ([]map[string]any, error)
}, path, name string) {
	t.Helper()
	items, err := hc.GetJSONList(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item["name"] == name {
			return
		}
	}
	t.Errorf("%s has no %q", path, name)
}
