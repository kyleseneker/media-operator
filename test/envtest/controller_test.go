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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	if err != nil {
		t.Fatalf("starting control plane: %v", err)
	}
	t.Cleanup(func() { _ = env.Stop() })

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("client-go scheme: %v", err)
	}
	for _, add := range adders {
		if err := add(scheme); err != nil {
			t.Fatalf("registering scheme: %v", err)
		}
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("building client: %v", err)
	}
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
	if err != nil {
		t.Fatalf("starting control plane: %v", err)
	}
	t.Cleanup(func() { _ = env.Stop() })

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("client-go scheme: %v", err)
	}
	if err := transcodev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("transcode scheme: %v", err)
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("building client: %v", err)
	}
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
		ObjectMeta: metav1.ObjectMeta{Name: "tdarr", Namespace: "default"},
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

	if err := c.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tdarr-credentials", Namespace: "default"},
		Data:       map[string][]byte{"api-key": []byte("k")},
	}); err != nil {
		t.Fatalf("creating secret: %v", err)
	}
	if err := c.Create(ctx, tdarrCR(url)); err != nil {
		t.Fatalf("creating CR: %v", err)
	}

	r := &transcode.TdarrConfigReconciler{Client: c, Scheme: c.Scheme(), Recorder: events.NewFakeRecorder(50)}
	key := types.NamespacedName{Name: "tdarr", Namespace: "default"}
	if _, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var got transcodev1alpha1.TdarrConfig
	if err := c.Get(ctx, key, &got); err != nil {
		t.Fatalf("re-reading CR: %v", err)
	}
	if len(got.Status.Conditions) == 0 {
		t.Fatal("no conditions persisted; the status subresource write did not land")
	}
	if got.Status.LastSyncTime == nil {
		t.Error("lastSyncTime not persisted")
	}
}

// A finalizer genuinely blocks deletion here, so an orphan policy that fails to
// clear it would wedge `kubectl delete` rather than passing silently.
func TestDeleteWithOrphanPolicyReleasesTheObject(t *testing.T) {
	c := startAPIServer(t)
	ctx := context.Background()
	url := fakeTdarrServer(t)

	if err := c.Create(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tdarr-credentials", Namespace: "default"},
		Data:       map[string][]byte{"api-key": []byte("k")},
	}); err != nil {
		t.Fatalf("creating secret: %v", err)
	}
	if err := c.Create(ctx, tdarrCR(url)); err != nil {
		t.Fatalf("creating CR: %v", err)
	}

	r := &transcode.TdarrConfigReconciler{Client: c, Scheme: c.Scheme(), Recorder: events.NewFakeRecorder(50)}
	key := types.NamespacedName{Name: "tdarr", Namespace: "default"}
	req := ctrl.Request{NamespacedName: key}

	if _, err := r.Reconcile(ctx, req); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	var cr transcodev1alpha1.TdarrConfig
	if err := c.Get(ctx, key, &cr); err != nil {
		t.Fatalf("re-reading CR: %v", err)
	}
	if len(cr.Finalizers) == 0 {
		t.Fatal("no finalizer was set, so this test would pass without exercising deletion")
	}
	if err := c.Delete(ctx, &cr); err != nil {
		t.Fatalf("deleting CR: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := r.Reconcile(ctx, req); err != nil {
			t.Fatalf("reconcile during deletion: %v", err)
		}
		err := c.Get(ctx, key, &cr)
		if apierrors.IsNotFound(err) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("CR still present after deletion; finalizers: %v", cr.Finalizers)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
