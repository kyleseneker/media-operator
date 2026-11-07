//go:build compat

package compat

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mediaserversv1alpha1 "github.com/kyleseneker/media-operator/api/mediaservers/v1alpha1"
	jellyfinclient "github.com/kyleseneker/media-operator/internal/client/jellyfin"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/controller/mediaservers"
	"github.com/kyleseneker/media-operator/internal/engine"
)

const jellyfinAdmin = "compat-admin"

// startJellyfin runs a fresh Jellyfin with its library folders created. Its
// first-time setup is left to the operator, which is what a rebuild relies on.
func startJellyfin(t *testing.T) *container {
	t.Helper()
	jf := startContainer(t, containerSpec{name: "jellyfin", image: jellyfinImage, port: 8096})
	jf.exec(t, "mkdir -p /media/movies /media/tv")
	// Jellyfin 12 serves a startup placeholder, system info included, before the
	// real server takes the port over; its log says when that has happened.
	waitFor(t, "jellyfin", 3*time.Minute, func() error {
		logs, err := runTool("logs", jf.id)
		if err != nil {
			return err
		}
		if !strings.Contains(logs, "Startup complete") {
			return fmt.Errorf("still starting")
		}
		return nil
	})
	return jf
}

func TestJellyfin(t *testing.T) {
	t.Parallel()
	jf := startJellyfin(t)
	password := randomKey()
	proxy := newRecordingProxy(t, jf.url())

	checkCompat(t, compatCase{
		fixture: "jellyfin.yaml",
		vars:    map[string]string{"URL": proxy.url()},
		secrets: map[string]string{"username": jellyfinAdmin, "password": password},
		newObj:  func() ctrlcommon.ConfigResource { return &mediaserversv1alpha1.JellyfinConfig{} },
		reconciler: func(c client.Client) reconcile.Reconciler {
			return &mediaservers.JellyfinConfigReconciler{Client: c, Scheme: c.Scheme(), Recorder: events.NewFakeRecorder(100)}
		},
		proxy: proxy,
		// Signing in is a POST that changes nothing.
		read: func(r request) bool {
			return getOnly(r) || (r.Method == http.MethodPost && r.Path == "/Users/AuthenticateByName")
		},
		observe: true,
		verify: func(t *testing.T) {
			jc := jellyfinClient(t, jf.url(), password)
			cfg, err := jc.GetConfig(context.Background(), "/System/Configuration")
			require.NoError(t, err)
			assert.Equal(t, "compat-jellyfin", cfg["ServerName"])
			encoding, err := jc.GetConfig(context.Background(), "/System/Configuration/encoding")
			require.NoError(t, err)
			assert.Equal(t, float64(28), encoding["H265Crf"])
			libraries, err := jc.ListLibraries(context.Background())
			require.NoError(t, err)
			names := make([]any, 0, len(libraries))
			for _, l := range libraries {
				names = append(names, l["Name"])
			}
			assert.ElementsMatch(t, []any{"Movies", "TV Shows"}, names)
		},
	})
}

// jellyfinClient signs in as the admin the operator created.
func jellyfinClient(t *testing.T, base, password string) *jellyfinclient.Client {
	t.Helper()
	hc, err := engine.NewHTTPClient(base, engine.AuthMediaBrowser, engine.WithAppLabel("compat"))
	require.NoError(t, err)
	jc := jellyfinclient.NewClient(hc)
	require.NoError(t, jc.Authenticate(context.Background(), jellyfinAdmin, password))
	return jc
}

// plexAllowedNetworks lets an unclaimed Plex answer the suite without a
// plex.tv account: Plex accepts any token from these networks.
const plexAllowedNetworks = "10.0.0.0/255.0.0.0,172.16.0.0/255.240.0.0,192.168.0.0/255.255.0.0"

func TestPlex(t *testing.T) {
	t.Parallel()
	prefs := `<?xml version="1.0" encoding="utf-8"?>
<Preferences allowedNetworks="` + plexAllowedNetworks + `" FriendlyName="compat" AcceptedEULA="1" />
`
	plex := startContainer(t, containerSpec{
		name: "plex", image: plexImage, port: 32400,
		files: map[string]string{"/config/Library/Application Support/Plex Media Server/Preferences.xml": prefs},
	})
	plex.exec(t, "mkdir -p /media/movies /media/tv /media/ready /transcode")
	// Plex answers long before it will create a library ("the server is still
	// starting up"); a throwaway library shows when it is ready.
	waitFor(t, "plex", 3*time.Minute, func() error {
		resp, err := http.Post(plex.url()+"/library/sections?name=ready&type=movie&agent=tv.plex.agents.movie"+
			"&scanner=Plex%20Movie&language=en-US&location=%2Fmedia%2Fready", "", nil)
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			return fmt.Errorf("library create %d", resp.StatusCode)
		}
		return nil
	})
	deleteReady, err := http.NewRequest(http.MethodDelete, plex.url()+"/library/sections/1", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(deleteReady)
	require.NoError(t, err)
	_ = resp.Body.Close()
	proxy := newRecordingProxy(t, plex.url())

	checkCompat(t, compatCase{
		fixture: "plex.yaml",
		vars:    map[string]string{"URL": proxy.url(), "ALLOWED_NETWORKS": plexAllowedNetworks},
		secrets: map[string]string{"token": "unclaimed"},
		newObj:  func() ctrlcommon.ConfigResource { return &mediaserversv1alpha1.PlexConfig{} },
		reconciler: func(c client.Client) reconcile.Reconciler {
			return &mediaservers.PlexConfigReconciler{Client: c, Scheme: c.Scheme(), Recorder: events.NewFakeRecorder(100)}
		},
		proxy:   proxy,
		observe: true,
		verify: func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, plex.url()+"/library/sections", nil)
			require.NoError(t, err)
			req.Header.Set("Accept", "application/json")
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()
			var sections struct {
				MediaContainer struct {
					Directory []struct{ Title string } `json:"Directory"`
				} `json:"MediaContainer"`
			}
			require.NoError(t, jsonDecode(resp, &sections))
			titles := make([]string, 0, len(sections.MediaContainer.Directory))
			for _, d := range sections.MediaContainer.Directory {
				titles = append(titles, d.Title)
			}
			assert.ElementsMatch(t, []string{"Movies", "TV Shows"}, titles)
		},
	})
}
