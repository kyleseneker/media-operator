//go:build compat

package compat

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
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
	"github.com/kyleseneker/media-operator/internal/engine"
)

func TestSonarr(t *testing.T) {
	t.Parallel()
	sonarr, key := startPVR(t, "sonarr", sonarrImage, 8989, "v3")
	sonarr.exec(t, "mkdir -p /data/media/tv && chown -R abc:abc /data")
	seedDelayProfile(t, directClient(t, sonarr.url(), key), "/api/v3", "hdr",
		map[string]any{"enableUsenet": true, "enableTorrent": true, "preferredProtocol": "usenet"})
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

			bluray := qualityDefinition(t, hc, "/api/v3", "Bluray-1080p")
			assert.Equal(t, "Blu-ray 1080p", bluray["title"])
			assert.InDelta(t, 50.4, bluray["minSize"], 0)
			assert.Nil(t, bluray["maxSize"], "0 should be stored as unlimited")
			assert.Nil(t, bluray["preferredSize"], "0 should be stored as unlimited")
			assert.InDelta(t, 67.5, qualityDefinition(t, hc, "/api/v3", "HDTV-720p")["maxSize"], 0)

			profiles, order := delayProfiles(t, hc, "/api/v3")
			assert.Equal(t, []string{"anime", "hdr"}, order, "the adopted hdr profile should have moved after anime")
			assert.Equal(t, "torrent", profiles["anime"]["preferredProtocol"])
			assert.InDelta(t, 60, profiles["anime"]["usenetDelay"], 0)
			assert.InDelta(t, 100, profiles["anime"]["minimumCustomFormatScore"], 0)
			assert.Equal(t, false, profiles["hdr"]["enableUsenet"])
			assert.InDelta(t, 30, profiles["default"]["usenetDelay"], 0)
			assert.InDelta(t, 10, profiles["default"]["torrentDelay"], 0)
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

			web := qualityDefinition(t, hc, "/api/v3", "WEBDL-1080p")
			assert.Equal(t, "WEB 1080p", web["title"])
			assert.InDelta(t, 12.5, web["minSize"], 0)
			assert.InDelta(t, 350, web["preferredSize"], 0)
			assert.Nil(t, qualityDefinition(t, hc, "/api/v3", "Bluray-1080p")["maxSize"])

			profiles, _ := delayProfiles(t, hc, "/api/v3")
			assert.Equal(t, "torrent", profiles["kids,uhd"]["preferredProtocol"])
			assert.InDelta(t, 120, profiles["kids,uhd"]["usenetDelay"], 0)
			assert.Equal(t, false, profiles["default"]["bypassIfHighestQuality"])
		},
		cleanup: ownedGone(directClient(t, radarr.url(), key), pvrclient.Definition("v3")),
	})
}

func TestLidarr(t *testing.T) {
	t.Parallel()
	lidarr, key := startPVR(t, "lidarr", lidarrImage, 8686, "v1")
	lidarr.exec(t, "mkdir -p /data/media/music && chown -R abc:abc /data")
	seedDelayProfile(t, directClient(t, lidarr.url(), key), "/api/v1", "live", map[string]any{"items": []any{
		map[string]any{"name": "Usenet", "protocol": "UsenetDownloadProtocol", "allowed": true, "delay": 0},
		map[string]any{"name": "Torrent", "protocol": "TorrentDownloadProtocol", "allowed": true, "delay": 0},
	}})
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

			flac := qualityDefinition(t, hc, "/api/v1", "FLAC")
			assert.Nil(t, flac["maxSize"])
			assert.InDelta(t, 900, flac["preferredSize"], 0)
			assert.Equal(t, "MP3 320", qualityDefinition(t, hc, "/api/v1", "MP3-320")["title"])

			profiles, order := delayProfiles(t, hc, "/api/v1")
			assert.Equal(t, []string{"favorites", "live"}, order, "the adopted live profile should have moved after favorites")
			items := list(profiles["favorites"]["items"])
			require.Len(t, items, 2)
			assert.Equal(t, "TorrentDownloadProtocol", items[0]["protocol"], "the preferred protocol goes first")
			assert.InDelta(t, 15, items[0]["delay"], 0)
			assert.Equal(t, false, items[1]["allowed"])
			assert.InDelta(t, 5, list(profiles["live"]["items"])[0]["delay"], 0)
			assert.InDelta(t, 30, list(profiles["default"]["items"])[0]["delay"], 0)
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

			epub := qualityDefinition(t, hc, "/api/v1", "EPUB")
			assert.Equal(t, "ePub", epub["title"])
			assert.InDelta(t, 100, epub["maxSize"], 0)
			assert.Nil(t, qualityDefinition(t, hc, "/api/v1", "MP3")["maxSize"])

			profiles, _ := delayProfiles(t, hc, "/api/v1")
			assert.Equal(t, "torrent", profiles["favorites"]["preferredProtocol"])
			assert.InDelta(t, 30, profiles["favorites"]["torrentDelay"], 0)
			assert.InDelta(t, 15, profiles["default"]["usenetDelay"], 0)
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

// seedDelayProfile adds a tag and a delay profile holding it, as if made in the
// app, so the first reconcile has a profile to adopt and move into place.
func seedDelayProfile(t *testing.T, hc *engine.HTTPClient, prefix, tag string, profile map[string]any) {
	t.Helper()
	ctx := context.Background()
	body, err := hc.Do(ctx, http.MethodPost, prefix+"/tag", map[string]any{"label": tag})
	require.NoError(t, err)
	var created map[string]any
	require.NoError(t, json.Unmarshal(body, &created))
	profile["tags"] = []any{created["id"]}
	_, err = hc.Do(ctx, http.MethodPost, prefix+"/delayprofile", profile)
	require.NoError(t, err)
}

func qualityDefinition(t *testing.T, hc *engine.HTTPClient, prefix, quality string) map[string]any {
	t.Helper()
	definitions, err := hc.GetJSONList(context.Background(), prefix+"/qualitydefinition")
	require.NoError(t, err)
	for _, d := range definitions {
		if q, _ := d["quality"].(map[string]any); q["name"] == quality {
			return d
		}
	}
	require.FailNow(t, "no quality definition", quality)
	return nil
}

// delayProfiles keys the app's delay profiles by their sorted tag labels, or
// "default", and lists the tagged ones in the order the app checks them.
func delayProfiles(t *testing.T, hc *engine.HTTPClient, prefix string) (map[string]map[string]any, []string) {
	t.Helper()
	ctx := context.Background()
	tags, err := hc.GetJSONList(ctx, prefix+"/tag")
	require.NoError(t, err)
	labels := map[float64]string{}
	for _, tag := range tags {
		labels[tag["id"].(float64)], _ = tag["label"].(string)
	}
	profiles, err := hc.GetJSONList(ctx, prefix+"/delayprofile")
	require.NoError(t, err)
	slices.SortFunc(profiles, func(a, b map[string]any) int {
		return cmp.Compare(a["order"].(float64), b["order"].(float64))
	})
	byTags := map[string]map[string]any{}
	var order []string
	for _, p := range profiles {
		var names []string
		for _, id := range p["tags"].([]any) {
			names = append(names, labels[id.(float64)])
		}
		slices.Sort(names)
		key := strings.Join(names, ",")
		if key == "" {
			key = "default"
		} else {
			order = append(order, key)
		}
		byTags[key] = p
	}
	return byTags, order
}

func list(v any) []map[string]any {
	raw, _ := v.([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}
