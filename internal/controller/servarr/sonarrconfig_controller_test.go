package servarr

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	servarrv1alpha1 "github.com/kyleseneker/media-operator/api/servarr/v1alpha1"
)

const conditionTrue = "True"

// fakeSonarr records every request it receives and answers the subset of the
// Sonarr v3 API the reconciler touches. Existing resources are seeded by name
// so prune behaviour can be exercised.
type fakeSonarr struct {
	mu       sync.Mutex
	requests []string
	existing map[string][]map[string]any
}

func newFakeSonarr(existing map[string][]map[string]any) *fakeSonarr {
	if existing == nil {
		existing = map[string][]map[string]any{}
	}
	return &fakeSonarr{existing: existing}
}

func (f *fakeSonarr) saw(method, path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.requests, method+" "+path)
}

func (f *fakeSonarr) sawPrefix(method, prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.requests {
		if strings.HasPrefix(r, method+" "+prefix) {
			return true
		}
	}
	return false
}

// routableAddr returns a non-loopback IPv4 address on this host. The operator's
// HTTP client deliberately refuses to dial loopback and link-local addresses as
// SSRF protection, so a fake app on 127.0.0.1 is unreachable by design. Binding
// to the host's real address exercises the reconciler through that guard rather
// than around it.
func routableAddr(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Skipf("cannot enumerate interfaces: %v", err)
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipnet.IP.To4()
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		return ip.String()
	}
	t.Skip("no routable non-loopback IPv4 address on this host")
	return ""
}

func (f *fakeSonarr) server(t *testing.T) *httptest.Server {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(routableAddr(t), "0"))
	if err != nil {
		t.Skipf("cannot bind a routable listener: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")

		// Settings endpoints are singletons: GET returns current, PUT accepts.
		if strings.HasPrefix(r.URL.Path, "/api/v3/config/") {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1})
			return
		}

		switch r.Method {
		case http.MethodGet:
			if r.URL.Path == "/api/v3/system/status" {
				_ = json.NewEncoder(w).Encode(map[string]any{"version": "4.0.0"})
				return
			}
			kind := strings.TrimPrefix(r.URL.Path, "/api/v3/")
			list := f.existing[kind]
			if list == nil {
				list = []map[string]any{}
			}
			_ = json.NewEncoder(w).Encode(list)
		case http.MethodPost, http.MethodPut:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body == nil {
				body = map[string]any{}
			}
			body["id"] = 99
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(body)
		case http.MethodDelete:
			w.WriteHeader(http.StatusOK)
		}
	}))
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func newTestReconciler(t *testing.T, objs ...runtime.Object) *SonarrConfigReconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("adding client-go scheme: %v", err)
	}
	if err := servarrv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding servarr scheme: %v", err)
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(objs...).
		WithStatusSubresource(&servarrv1alpha1.SonarrConfig{}).
		Build()
	return &SonarrConfigReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: events.NewFakeRecorder(50),
	}
}

func apiKeySecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "sonarr-credentials", Namespace: "media"},
		Data:       map[string][]byte{"api-key": []byte("test-key")},
	}
}

func sonarrConfig(url string, mutate func(*servarrv1alpha1.SonarrConfig)) *servarrv1alpha1.SonarrConfig {
	cfg := &servarrv1alpha1.SonarrConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "sonarr", Namespace: "media"},
		Spec: servarrv1alpha1.SonarrConfigSpec{
			Connection: commonv1alpha1.AppConnection{
				URL:             url,
				APIKeySecretRef: commonv1alpha1.SecretKeyRef{Name: "sonarr-credentials", Key: "api-key"},
			},
			RootFolders: []commonv1alpha1.RootFolder{{Path: "/tv"}},
		},
	}
	if mutate != nil {
		mutate(cfg)
	}
	return cfg
}

// reconcileOnce runs a single reconcile. HandleLifecycle installs the finalizer
// and falls through in the same pass, so one call performs the full sync.
func reconcileOnce(t *testing.T, r *SonarrConfigReconciler) ctrl.Result {
	t.Helper()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "sonarr", Namespace: "media"}}
	res, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return res
}

func getConfig(t *testing.T, r *SonarrConfigReconciler) *servarrv1alpha1.SonarrConfig {
	t.Helper()
	var cfg servarrv1alpha1.SonarrConfig
	if err := r.Get(context.Background(), types.NamespacedName{Name: "sonarr", Namespace: "media"}, &cfg); err != nil {
		t.Fatalf("fetching config: %v", err)
	}
	return &cfg
}

func conditionStatus(cfg *servarrv1alpha1.SonarrConfig, condType string) string {
	for _, c := range cfg.Status.Conditions {
		if c.Type == condType {
			return string(c.Status)
		}
	}
	return ""
}

func TestReconcileWritesDeclaredResources(t *testing.T) {
	app := newFakeSonarr(nil)
	srv := app.server(t)

	rename := true
	r := newTestReconciler(t, apiKeySecret(), sonarrConfig(srv.URL, func(c *servarrv1alpha1.SonarrConfig) {
		c.Spec.Naming = &servarrv1alpha1.SonarrNaming{
			RenameEpisodes:        &rename,
			StandardEpisodeFormat: "{Series Title} - S{season:00}E{episode:00}",
		}
		c.Spec.DownloadClients = []commonv1alpha1.DownloadClient{{
			Name: "qBittorrent", Protocol: "torrent", Implementation: "QBittorrent",
			Host: "qbittorrent", Port: 8080, Category: "tv",
		}}
	}))
	reconcileOnce(t, r)

	if !app.saw("POST", "/api/v3/rootfolder") {
		t.Error("root folder was never created")
	}
	if !app.saw("POST", "/api/v3/downloadclient") {
		t.Error("download client was never created")
	}
	// Settings are written to /config/<section>/<id>.
	if !app.sawPrefix("PUT", "/api/v3/config/naming/") {
		t.Error("naming settings were never pushed")
	}

	cfg := getConfig(t, r)
	if got := conditionStatus(cfg, "Synced"); got != conditionTrue {
		t.Errorf("Synced = %q, want True (conditions: %+v)", got, cfg.Status.Conditions)
	}
	if len(cfg.Status.ManagedResources["downloadClients"]) == 0 {
		t.Errorf("managedResources did not record the download client: %+v", cfg.Status.ManagedResources)
	}
}

func TestReconcileMarksUnreachableAppNotReady(t *testing.T) {
	// A URL that nothing is listening on.
	r := newTestReconciler(t, apiKeySecret(), sonarrConfig("http://127.0.0.1:1", nil))
	reconcileOnce(t, r)

	cfg := getConfig(t, r)
	if got := conditionStatus(cfg, "Ready"); got == conditionTrue {
		t.Errorf("Ready = True for an unreachable app; conditions: %+v", cfg.Status.Conditions)
	}
}

func TestReconcileMissingSecretDoesNotContactApp(t *testing.T) {
	app := newFakeSonarr(nil)
	srv := app.server(t)

	// No Secret in the cluster.
	r := newTestReconciler(t, sonarrConfig(srv.URL, nil))
	reconcileOnce(t, r)

	if app.sawPrefix("GET", "/api/v3/") {
		t.Error("reconciler contacted the app despite failing to resolve its API key")
	}
	cfg := getConfig(t, r)
	if got := conditionStatus(cfg, "Synced"); got == conditionTrue {
		t.Errorf("Synced = True despite a missing secret; conditions: %+v", cfg.Status.Conditions)
	}
}

// Prune must never remove resources the operator did not create. status
// .managedResources is the record of what it made, and an empty record means
// everything in the app predates the operator.
func TestPruneLeavesUnmanagedResourcesAlone(t *testing.T) {
	app := newFakeSonarr(map[string][]map[string]any{
		"downloadclient": {{"id": float64(7), "name": "hand-made-client"}},
	})
	srv := app.server(t)

	enabled := true
	r := newTestReconciler(t, apiKeySecret(), sonarrConfig(srv.URL, func(c *servarrv1alpha1.SonarrConfig) {
		c.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{Prune: &enabled}
		c.Spec.DownloadClients = []commonv1alpha1.DownloadClient{{
			Name: "qBittorrent", Protocol: "torrent", Implementation: "QBittorrent",
			Host: "qbittorrent", Port: 8080, Category: "tv",
		}}
	}))
	reconcileOnce(t, r)

	// Guard against a vacuous pass: if the reconcile never reached the app, the
	// absence of a DELETE would prove nothing.
	if !app.saw("POST", "/api/v3/downloadclient") {
		t.Fatal("reconcile never reached the app, so the DELETE check is meaningless")
	}
	if app.sawPrefix("DELETE", "/api/v3/downloadclient") {
		t.Error("prune deleted a download client the operator never created")
	}
}

// The mirror of the above: once a resource is recorded as operator-created,
// dropping it from the spec does prune it.
func TestPruneRemovesPreviouslyManagedResources(t *testing.T) {
	app := newFakeSonarr(map[string][]map[string]any{
		"downloadclient": {{"id": float64(7), "name": "retired-client"}},
	})
	srv := app.server(t)

	enabled := true
	cfg := sonarrConfig(srv.URL, func(c *servarrv1alpha1.SonarrConfig) {
		c.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{Prune: &enabled}
		c.Spec.DownloadClients = []commonv1alpha1.DownloadClient{{
			Name: "qBittorrent", Protocol: "torrent", Implementation: "QBittorrent",
			Host: "qbittorrent", Port: 8080, Category: "tv",
		}}
		c.Status.ManagedResources = map[string][]string{
			"downloadClients": {"qBittorrent", "retired-client"},
		}
	})
	r := newTestReconciler(t, apiKeySecret(), cfg)
	reconcileOnce(t, r)

	if !app.sawPrefix("DELETE", "/api/v3/downloadclient") {
		t.Error("prune did not remove a resource it had previously created")
	}
}
