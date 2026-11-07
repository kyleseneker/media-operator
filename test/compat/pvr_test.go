//go:build compat

package compat

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pvrv1alpha1 "github.com/kyleseneker/media-operator/api/pvr/v1alpha1"
	pvrclient "github.com/kyleseneker/media-operator/internal/client/pvr"
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
		export:  "sonarr",
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
			require.NoError(t, err)
			assert.Equal(t, "Season {season:00}", naming["seasonFolderFormat"])
			requireNamed(t, hc, "/api/v3/downloadclient", "qBittorrent")
			requireNamed(t, hc, "/api/v3/indexer", "Compat Torznab")
			requireNamed(t, hc, "/api/v3/qualityprofile", "HD-1080p")
			requireNamed(t, hc, "/api/v3/notification", "Compat Webhook")
			lists, err := hc.GetJSONList(context.Background(), "/api/v3/importlist")
			require.NoError(t, err)
			require.Len(t, lists, 1)
			assert.Equal(t, "all", lists[0]["shouldMonitor"], "import list monitor did not reach Sonarr")
		},
		cleanup: ownedGone(directClient(t, sonarr.url(), key), pvrclient.Definition("v3")),
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
		export:  "radarr",
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
			require.NoError(t, err)
			assert.Equal(t, "smart", naming["colonReplacementFormat"])
			requireNamed(t, hc, "/api/v3/downloadclient", "qBittorrent")
			requireNamed(t, hc, "/api/v3/indexer", "Compat Torznab")
			requireNamed(t, hc, "/api/v3/qualityprofile", "HD-1080p")
			requireNamed(t, hc, "/api/v3/notification", "Compat Webhook")
			lists, err := hc.GetJSONList(context.Background(), "/api/v3/importlist")
			require.NoError(t, err)
			require.Len(t, lists, 1)
			assert.Equal(t, "movieOnly", lists[0]["monitor"], "import list monitor did not reach Radarr")
			assert.Equal(t, true, lists[0]["enabled"], "import list enabled did not reach Radarr")
		},
		cleanup: ownedGone(directClient(t, radarr.url(), key), pvrclient.Definition("v3")),
	})
}

func TestLidarr(t *testing.T) {
	t.Parallel()
	lidarr, key := startPVR(t, "lidarr", lidarrImage, 8686, "v1")
	lidarr.exec(t, "mkdir -p /data/media/music && chown -R abc:abc /data")
	qbit, qbitPassword := startQBittorrent(t)
	fakes := startFakes(t)
	proxy := newRecordingProxy(t, lidarr.url())

	checkCompat(t, compatCase{
		fixture: "lidarr.yaml",
		export:  "lidarr",
		vars: map[string]string{
			"URL": proxy.url(), "HOST": hostIP, "FAKES": fakes, "QBITTORRENT_PORT": strconv.Itoa(qbit.port),
		},
		secrets: map[string]string{
			"api-key": key, "qbittorrent-username": qbittorrentUser, "qbittorrent-password": qbitPassword,
		},
		newObj: func() ctrlcommon.ConfigResource { return &pvrv1alpha1.LidarrConfig{} },
		reconciler: func(c client.Client) reconcile.Reconciler {
			return &pvr.LidarrConfigReconciler{Client: c, Scheme: c.Scheme(), Recorder: events.NewFakeRecorder(100)}
		},
		proxy:   proxy,
		observe: true,
		verify: func(t *testing.T) {
			hc := directClient(t, lidarr.url(), key)
			requireNamed(t, hc, "/api/v1/rootfolder", "music")
			requireNamed(t, hc, "/api/v1/downloadclient", "qBittorrent")
			requireNamed(t, hc, "/api/v1/indexer", "Compat Torznab")
			requireNamed(t, hc, "/api/v1/qualityprofile", "Music")
			requireNamed(t, hc, "/api/v1/notification", "Compat Webhook")
			lists, err := hc.GetJSONList(context.Background(), "/api/v1/importlist")
			require.NoError(t, err)
			require.Len(t, lists, 1)
			assert.Equal(t, "entireArtist", lists[0]["shouldMonitor"], "import list monitor did not reach Lidarr")
		},
		// The root folder defaults to this profile and is never deleted.
		cleanup: ownedGone(directClient(t, lidarr.url(), key), pvrclient.Definition("v1"), "qualityProfiles/Music"),
	})
}

func TestReadarr(t *testing.T) {
	t.Parallel()
	readarr, key := startPVR(t, "readarr", readarrImage, 8787, "v1")
	readarr.exec(t, "mkdir -p /data/media/books && chown -R abc:abc /data")
	fakes := startFakes(t)
	proxy := newRecordingProxy(t, readarr.url())

	checkCompat(t, compatCase{
		fixture: "readarr.yaml",
		export:  "readarr",
		vars:    map[string]string{"URL": proxy.url(), "HOST": hostIP, "FAKES": fakes},
		secrets: map[string]string{"api-key": key},
		newObj:  func() ctrlcommon.ConfigResource { return &pvrv1alpha1.ReadarrConfig{} },
		reconciler: func(c client.Client) reconcile.Reconciler {
			return &pvr.ReadarrConfigReconciler{Client: c, Scheme: c.Scheme(), Recorder: events.NewFakeRecorder(100)}
		},
		proxy:   proxy,
		observe: true,
		verify: func(t *testing.T) {
			hc := directClient(t, readarr.url(), key)
			requireNamed(t, hc, "/api/v1/rootfolder", "books")
			requireNamed(t, hc, "/api/v1/indexer", "Compat Torznab")
			requireNamed(t, hc, "/api/v1/qualityprofile", "Books")
			requireNamed(t, hc, "/api/v1/notification", "Compat Webhook")
		},
		// The root folder defaults to this profile and is never deleted.
		cleanup: ownedGone(directClient(t, readarr.url(), key), pvrclient.Definition("v1"), "qualityProfiles/Books"),
	})
}

func requireNamed(t *testing.T, hc interface {
	GetJSONList(context.Context, string) ([]map[string]any, error)
}, path, name string) {
	t.Helper()
	items, err := hc.GetJSONList(context.Background(), path)
	require.NoError(t, err)
	names := make([]any, 0, len(items))
	for _, item := range items {
		names = append(names, item["name"])
	}
	assert.Contains(t, names, name, "%s has no %q", path, name)
}
