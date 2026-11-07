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
