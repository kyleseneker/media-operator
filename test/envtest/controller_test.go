// Reconciling against a real kube-apiserver exercises behaviour a fake client
// cannot: the status subresource is a separate write, CRD validation rejects
// values the Go types accept, and a finalizer genuinely blocks deletion until
// the controller clears it.
package envtest

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	transcodev1alpha1 "github.com/kyleseneker/media-operator/api/transcode/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/controller/transcode"
)

// startAPIServerWith boots one control plane for a set of API groups, so a table
// of kinds does not pay for a control plane each.
func startAPIServerWith(t *testing.T, adders ...func(*runtime.Scheme) error) client.Client {
	t.Helper()
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS unset; run via `make test-envtest`")
	}

	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := env.Start()
	require.NoError(t, err, "starting control plane")
	t.Cleanup(func() { _ = env.Stop() })

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme), "client-go scheme")
	for _, add := range adders {
		require.NoError(t, add(scheme), "registering scheme")
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err, "building client")
	return c
}

func startAPIServer(t *testing.T) client.Client {
	t.Helper()
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS unset; run via `make test-envtest`")
	}

	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := env.Start()
	require.NoError(t, err, "starting control plane")
	t.Cleanup(func() { _ = env.Stop() })

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme), "client-go scheme")
	require.NoError(t, transcodev1alpha1.AddToScheme(scheme), "transcode scheme")
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err, "building client")
	return c
}

// fakeTdarrServer answers the endpoints the controller touches, on a routable
// address because the HTTP client refuses to dial loopback.
func fakeTdarrServer(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Skipf("cannot enumerate interfaces: %v", err)
	}
	host := ""
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if ip := ipnet.IP.To4(); ip != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
			host = ip.String()
			break
		}
	}
	if host == "" {
		t.Skip("no routable non-loopback IPv4 address on this host")
	}

	ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Skipf("cannot bind a routable listener: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL
}

func tdarrCR(url string) *transcodev1alpha1.TdarrConfig {
	orphan := "orphan"
	return &transcodev1alpha1.TdarrConfig{
		Name: "tdarr", Namespace: "default",
		Spec: transcodev1alpha1.TdarrConfigSpec{
			Connection: commonv1alpha1.AppConnection{
				URL:             url,
				APIKeySecretRef: commonv1alpha1.SecretKeyRef{Name: "tdarr-credentials", Key: "api-key"},
			},
			Reconcile: &commonv1alpha1.ReconcileConfig{DeletionPolicy: &orphan},
		},
	}
}

// The status subresource is a separate write against a real API server, so a
// controller that only mutates the in-memory object reports nothing.
func TestReconcilePersistsStatusThroughSubresource(t *testing.T) {
	c := startAPIServer(t)
	ctx := context.Background()
	url := fakeTdarrServer(t)

	require.NoError(t, c.Create(ctx, &corev1.Secret{
		Name: "tdarr-credentials", Namespace: "default",
		Data: map[string][]byte{"api-key": []byte("k")},
	}), "creating secret")
	require.NoError(t, c.Create(ctx, tdarrCR(url)), "creating CR")

	r := &transcode.TdarrConfigReconciler{Client: c, Scheme: c.Scheme(), Recorder: events.NewFakeRecorder(50)}
	key := types.NamespacedName{Name: "tdarr", Namespace: "default"}
	_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
	require.NoError(t, err, "reconcile")

	var got transcodev1alpha1.TdarrConfig
	require.NoError(t, c.Get(ctx, key, &got), "re-reading CR")
	require.NotEmpty(t, got.Status.Conditions, "no conditions persisted; the status subresource write did not land")
	assert.NotNil(t, got.Status.LastSyncTime, "lastSyncTime not persisted")
}

// A finalizer genuinely blocks deletion here, so an orphan policy that fails to
// clear it would wedge `kubectl delete` rather than passing silently.
func TestDeleteWithOrphanPolicyReleasesTheObject(t *testing.T) {
	c := startAPIServer(t)
	ctx := context.Background()
	url := fakeTdarrServer(t)

	require.NoError(t, c.Create(ctx, &corev1.Secret{
		Name: "tdarr-credentials", Namespace: "default",
		Data: map[string][]byte{"api-key": []byte("k")},
	}), "creating secret")
	require.NoError(t, c.Create(ctx, tdarrCR(url)), "creating CR")

	r := &transcode.TdarrConfigReconciler{Client: c, Scheme: c.Scheme(), Recorder: events.NewFakeRecorder(50)}
	key := types.NamespacedName{Name: "tdarr", Namespace: "default"}
	req := ctrl.Request{NamespacedName: key}

	_, err := r.Reconcile(ctx, req)
	require.NoError(t, err, "first reconcile")

	var cr transcodev1alpha1.TdarrConfig
	require.NoError(t, c.Get(ctx, key, &cr), "re-reading CR")
	require.NotEmpty(t, cr.Finalizers, "no finalizer was set, so this test would pass without exercising deletion")
	require.NoError(t, c.Delete(ctx, &cr), "deleting CR")

	deadline := time.Now().Add(30 * time.Second)
	for {
		_, err := r.Reconcile(ctx, req)
		require.NoError(t, err, "reconcile during deletion")
		err = c.Get(ctx, key, &cr)
		if apierrors.IsNotFound(err) {
			return
		}
		require.False(t, time.Now().After(deadline), "CR still present after deletion; finalizers: %v", cr.Finalizers)
		time.Sleep(200 * time.Millisecond)
	}
}
