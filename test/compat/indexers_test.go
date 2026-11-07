//go:build compat

package compat

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	indexersv1alpha1 "github.com/kyleseneker/media-operator/api/indexers/v1alpha1"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/controller/indexers"
)

func TestProwlarr(t *testing.T) {
	t.Parallel()
	prowlarr, key := startPVR(t, "prowlarr", prowlarrImage, 9696, "v1")
	sonarr, sonarrKey := startPVR(t, "sonarr", sonarrImage, 8989, "v3")
	qbit, qbitPassword := startQBittorrent(t)
	fakes := startFakes(t)
	proxy := newRecordingProxy(t, prowlarr.url())

	checkCompat(t, compatCase{
		fixture: "prowlarr.yaml",
		vars: map[string]string{
			"URL": proxy.url(), "HOST": hostIP, "FAKES": fakes, "QBITTORRENT_PORT": strconv.Itoa(qbit.port),
			"PROWLARR_URL": prowlarr.url(), "SONARR_URL": sonarr.url(),
		},
		secrets: map[string]string{
			"api-key": key, "sonarr-api-key": sonarrKey,
			"qbittorrent-username": qbittorrentUser, "qbittorrent-password": qbitPassword,
		},
		newObj: func() ctrlcommon.ConfigResource { return &indexersv1alpha1.ProwlarrConfig{} },
		reconciler: func(c client.Client) reconcile.Reconciler {
			return &indexers.ProwlarrConfigReconciler{Client: c, Scheme: c.Scheme(), Recorder: events.NewFakeRecorder(100)}
		},
		proxy:   proxy,
		observe: true,
		verify: func(t *testing.T) {
			hc := directClient(t, prowlarr.url(), key)
			requireNamed(t, hc, "/api/v1/appprofile", "Compat Profile")
			requireNamed(t, hc, "/api/v1/applications", "Sonarr")
			requireNamed(t, hc, "/api/v1/indexer", "Compat Torznab")
			requireNamed(t, hc, "/api/v1/downloadclient", "qBittorrent")
			requireNamed(t, hc, "/api/v1/notification", "Compat Webhook")
			// The application is only useful if Prowlarr can push the indexer into Sonarr.
			sonarrHC := directClient(t, sonarr.url(), sonarrKey)
			waitFor(t, "indexer sync to Sonarr", 2*time.Minute, func() error {
				items, err := sonarrHC.GetJSONList(context.Background(), "/api/v3/indexer")
				if err != nil {
					return err
				}
				for _, item := range items {
					if item["name"] == "Compat Torznab (Prowlarr)" {
						return nil
					}
				}
				return fmt.Errorf("sonarr has %d indexers, none synced from Prowlarr", len(items))
			})
		},
	})
}
