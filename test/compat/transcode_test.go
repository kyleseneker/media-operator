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

	transcodev1alpha1 "github.com/kyleseneker/media-operator/api/transcode/v1alpha1"
	tdarrclient "github.com/kyleseneker/media-operator/internal/client/tdarr"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/controller/transcode"
	"github.com/kyleseneker/media-operator/internal/engine"
)

func TestTdarr(t *testing.T) {
	t.Parallel()
	tdarr := startContainer(t, containerSpec{
		name: "tdarr", image: tdarrImage, port: 8265,
		env: withEnv(linuxserverEnv, map[string]string{
			"serverIP": "0.0.0.0", "serverPort": "8266", "webUIPort": "8265",
			"internalNode": "true", "inContainer": "true", "nodeName": "InternalNode", "auth": "false",
		}),
	})
	tdarr.exec(t, "mkdir -p /media/movies /temp")
	hc, err := engine.NewHTTPClient(tdarr.url(), engine.AuthNone, engine.WithAppLabel("compat"))
	require.NoError(t, err)
	tc := tdarrclient.NewClient(hc)
	// Worker limits belong to a node, so wait for the internal node to register.
	waitFor(t, "tdarr internal node", 4*time.Minute, func() error {
		nodes, err := tc.GetNodes(context.Background())
		if err != nil {
			return err
		}
		if len(nodes) == 0 {
			return fmt.Errorf("no nodes registered yet")
		}
		return nil
	})
	proxy := newRecordingProxy(t, tdarr.url())

	checkCompat(t, compatCase{
		fixture: "tdarr.yaml",
		vars:    map[string]string{"URL": proxy.url()},
		// Tdarr 3 enables authentication on a fresh config; this runs it without, as
		// homelab does. The key is still required by the CRD.
		secrets: map[string]string{"api-key": "unused"},
		newObj:  func() ctrlcommon.ConfigResource { return &transcodev1alpha1.TdarrConfig{} },
		reconciler: func(c client.Client) reconcile.Reconciler {
			return &transcode.TdarrConfigReconciler{Client: c, Scheme: c.Scheme(), Recorder: events.NewFakeRecorder(100)}
		},
		proxy:   proxy,
		observe: true,
		// Tdarr reads and writes through POST /api/v2/cruddb; the mode decides.
		read: func(r request) bool {
			if getOnly(r) {
				return true
			}
			return r.Method == http.MethodPost && r.Path == "/api/v2/cruddb" &&
				(strings.Contains(r.Body, `"mode":"getAll"`) || strings.Contains(r.Body, `"mode":"getById"`))
		},
		verify: func(t *testing.T) {
			lib, err := tc.GetByID(context.Background(), "LibrarySettingsJSONDB", "compatMovies")
			require.NoError(t, err)
			assert.Equal(t, "/media/movies", lib["folder"])
			assert.Equal(t, "compatFlow1", lib["flowId"])
			flow, err := tc.GetByID(context.Background(), "FlowsJSONDB", "compatFlow1")
			require.NoError(t, err)
			assert.Equal(t, "Compat - remux check", flow["name"])
		},
	})
}
