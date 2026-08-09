package pvr

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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	pvrv1alpha1 "github.com/kyleseneker/media-operator/api/pvr/v1alpha1"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
)

const conditionTrue = "True"

// The four PVR controllers are the same implementation with a different API
// version and download-client category field, so every case runs against all
// four rather than trusting that Sonarr passing implies the rest do.

type buildOpts struct {
	downloadClient bool
	naming         bool
	prune          bool
	managed        map[string][]string
}

type pvrApp struct {
	name   string
	apiVer string
	build  func(url string, o buildOpts) ctrlcommon.ConfigResource
	empty  func() ctrlcommon.ConfigResource
	newRec func(c client.Client, s *runtime.Scheme) reconcile.Reconciler
}

func baseSpec(o buildOpts) (commonv1alpha1.AppConnection, []commonv1alpha1.RootFolder, []commonv1alpha1.DownloadClient, *commonv1alpha1.ReconcileConfig) {
	conn := commonv1alpha1.AppConnection{
		APIKeySecretRef: commonv1alpha1.SecretKeyRef{Name: "app-credentials", Key: "api-key"},
	}
	roots := []commonv1alpha1.RootFolder{{Path: "/media"}}
	var dcs []commonv1alpha1.DownloadClient
	if o.downloadClient {
		dcs = []commonv1alpha1.DownloadClient{{
			Name: "qBittorrent", Protocol: "torrent", Implementation: "QBittorrent",
			Host: "qbittorrent", Port: 8080, Category: "media",
		}}
	}
	var rec *commonv1alpha1.ReconcileConfig
	if o.prune {
		enabled := true
		rec = &commonv1alpha1.ReconcileConfig{Prune: &enabled}
	}
	return conn, roots, dcs, rec
}

func meta() metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: "app", Namespace: "media"}
}

var pvrApps = []pvrApp{
	{
		name: "sonarr", apiVer: "v3",
		empty: func() ctrlcommon.ConfigResource { return &pvrv1alpha1.SonarrConfig{} },
		newRec: func(c client.Client, s *runtime.Scheme) reconcile.Reconciler {
			return &SonarrConfigReconciler{Client: c, Scheme: s, Recorder: events.NewFakeRecorder(50)}
		},
		build: func(url string, o buildOpts) ctrlcommon.ConfigResource {
			conn, roots, dcs, rec := baseSpec(o)
			conn.URL = url
			c := &pvrv1alpha1.SonarrConfig{ObjectMeta: meta(), Spec: pvrv1alpha1.SonarrConfigSpec{
				Connection: conn, RootFolders: roots, DownloadClients: dcs, Reconcile: rec,
			}}
			if o.naming {
				yes := true
				c.Spec.Naming = &pvrv1alpha1.SonarrNaming{RenameEpisodes: &yes}
			}
			c.Status.ManagedResources = o.managed
			return c
		},
	},
	{
		name: "radarr", apiVer: "v3",
		empty: func() ctrlcommon.ConfigResource { return &pvrv1alpha1.RadarrConfig{} },
		newRec: func(c client.Client, s *runtime.Scheme) reconcile.Reconciler {
			return &RadarrConfigReconciler{Client: c, Scheme: s, Recorder: events.NewFakeRecorder(50)}
		},
		build: func(url string, o buildOpts) ctrlcommon.ConfigResource {
			conn, roots, dcs, rec := baseSpec(o)
			conn.URL = url
			c := &pvrv1alpha1.RadarrConfig{ObjectMeta: meta(), Spec: pvrv1alpha1.RadarrConfigSpec{
				Connection: conn, RootFolders: roots, DownloadClients: dcs, Reconcile: rec,
			}}
			if o.naming {
				yes := true
				c.Spec.Naming = &pvrv1alpha1.RadarrNaming{RenameMovies: &yes}
			}
			c.Status.ManagedResources = o.managed
			return c
		},
	},
	{
		name: "lidarr", apiVer: "v1",
		empty: func() ctrlcommon.ConfigResource { return &pvrv1alpha1.LidarrConfig{} },
		newRec: func(c client.Client, s *runtime.Scheme) reconcile.Reconciler {
			return &LidarrConfigReconciler{Client: c, Scheme: s, Recorder: events.NewFakeRecorder(50)}
		},
		build: func(url string, o buildOpts) ctrlcommon.ConfigResource {
			conn, roots, dcs, rec := baseSpec(o)
			conn.URL = url
			c := &pvrv1alpha1.LidarrConfig{ObjectMeta: meta(), Spec: pvrv1alpha1.LidarrConfigSpec{
				Connection: conn, RootFolders: roots, DownloadClients: dcs, Reconcile: rec,
			}}
			if o.naming {
				yes := true
				c.Spec.Naming = &pvrv1alpha1.LidarrNaming{RenameTracks: &yes}
			}
			c.Status.ManagedResources = o.managed
			return c
		},
	},
	{
		name: "readarr", apiVer: "v1",
		empty: func() ctrlcommon.ConfigResource { return &pvrv1alpha1.ReadarrConfig{} },
		newRec: func(c client.Client, s *runtime.Scheme) reconcile.Reconciler {
			return &ReadarrConfigReconciler{Client: c, Scheme: s, Recorder: events.NewFakeRecorder(50)}
		},
		build: func(url string, o buildOpts) ctrlcommon.ConfigResource {
			conn, roots, dcs, rec := baseSpec(o)
			conn.URL = url
			c := &pvrv1alpha1.ReadarrConfig{ObjectMeta: meta(), Spec: pvrv1alpha1.ReadarrConfigSpec{
				Connection: conn, RootFolders: roots, DownloadClients: dcs, Reconcile: rec,
			}}
			if o.naming {
				yes := true
				c.Spec.Naming = &pvrv1alpha1.ReadarrNaming{RenameBooks: &yes}
			}
			c.Status.ManagedResources = o.managed
			return c
		},
	},
}

// fakeArr answers the subset of the API the reconciler touches and records
// every request. Existing resources are seeded by endpoint so prune can be
// exercised.
type fakeArr struct {
	apiVer   string
	mu       sync.Mutex
	requests []string
	existing map[string][]map[string]any
}

func (f *fakeArr) saw(method, path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.requests, method+" "+path)
}

func (f *fakeArr) sawPrefix(method, prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.requests {
		if strings.HasPrefix(r, method+" "+prefix) {
			return true
		}
	}
	return false
}

func (f *fakeArr) path(suffix string) string { return "/api/" + f.apiVer + "/" + suffix }

// routableAddr returns a non-loopback IPv4 address on this host. The operator's
// HTTP client refuses to dial loopback and link-local addresses as SSRF
// protection, so a fake app on 127.0.0.1 is unreachable by design and every
// assertion against it would pass for the wrong reason.
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

// unhealthyURL returns an app that answers but fails its health check. This
// drives the same Ping-failure branch as an unroutable host, without depending
// on how the host OS treats connections to a closed port — macOS drops them and
// the dial hangs for the full timeout, Linux refuses immediately.
func unhealthyURL(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(routableAddr(t), "0"))
	if err != nil {
		t.Skipf("cannot bind: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL
}

func newFakeArr(t *testing.T, apiVer string, existing map[string][]map[string]any) (*fakeArr, string) {
	t.Helper()
	if existing == nil {
		existing = map[string][]map[string]any{}
	}
	f := &fakeArr{apiVer: apiVer, existing: existing}
	prefix := "/api/" + apiVer + "/"

	ln, err := net.Listen("tcp", net.JoinHostPort(routableAddr(t), "0"))
	if err != nil {
		t.Skipf("cannot bind a routable listener: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, prefix+"config/") {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1})
			return
		}
		switch r.Method {
		case http.MethodGet:
			if r.URL.Path == prefix+"system/status" {
				_ = json.NewEncoder(w).Encode(map[string]any{"version": "4.0.0"})
				return
			}
			list := f.existing[strings.TrimPrefix(r.URL.Path, prefix)]
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
	return f, srv.URL
}

func apiKeySecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "app-credentials", Namespace: "media"},
		Data:       map[string][]byte{"api-key": []byte("test-key")},
	}
}

// run wires a reconciler around the given objects and reconciles once.
func run(t *testing.T, app pvrApp, objs ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("adding client-go scheme: %v", err)
	}
	if err := pvrv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding pvr scheme: %v", err)
	}
	b := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(app.empty())
	for _, o := range objs {
		b = b.WithObjects(o)
	}
	c := b.Build()
	r := app.newRec(c, scheme)
	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "app", Namespace: "media"},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return c
}

func conditionStatus(t *testing.T, c client.Client, app pvrApp, condType string) string {
	t.Helper()
	obj := app.empty()
	if err := c.Get(context.Background(), types.NamespacedName{Name: "app", Namespace: "media"}, obj); err != nil {
		t.Fatalf("fetching config: %v", err)
	}
	for _, cond := range *obj.GetConditions() {
		if cond.Type == condType {
			return string(cond.Status)
		}
	}
	return ""
}

func TestReconcileWritesDeclaredResources(t *testing.T) {
	for _, app := range pvrApps {
		t.Run(app.name, func(t *testing.T) {
			fa, url := newFakeArr(t, app.apiVer, nil)
			cfg := app.build(url, buildOpts{downloadClient: true, naming: true})
			c := run(t, app, apiKeySecret(), cfg)

			if !fa.saw("POST", fa.path("rootfolder")) {
				t.Error("root folder was never created")
			}
			if !fa.saw("POST", fa.path("downloadclient")) {
				t.Error("download client was never created")
			}
			// Settings are written to /config/<section>/<id>.
			if !fa.sawPrefix("PUT", fa.path("config/naming/")) {
				t.Error("naming settings were never pushed")
			}
			if got := conditionStatus(t, c, app, "Synced"); got != conditionTrue {
				t.Errorf("Synced = %q, want True", got)
			}

			obj := app.empty()
			if err := c.Get(context.Background(), types.NamespacedName{Name: "app", Namespace: "media"}, obj); err != nil {
				t.Fatalf("fetching config: %v", err)
			}
			if len((*obj.GetManagedResources())["downloadClients"]) == 0 {
				t.Errorf("managedResources did not record the download client: %+v", *obj.GetManagedResources())
			}
		})
	}
}

func TestReconcileMarksUnhealthyAppNotReady(t *testing.T) {
	for _, app := range pvrApps {
		t.Run(app.name, func(t *testing.T) {
			cfg := app.build(unhealthyURL(t), buildOpts{})
			c := run(t, app, apiKeySecret(), cfg)
			if got := conditionStatus(t, c, app, "Ready"); got == conditionTrue {
				t.Error("Ready = True for an app failing its health check")
			}
		})
	}
}

func TestReconcileMissingSecretDoesNotContactApp(t *testing.T) {
	for _, app := range pvrApps {
		t.Run(app.name, func(t *testing.T) {
			fa, url := newFakeArr(t, app.apiVer, nil)
			cfg := app.build(url, buildOpts{})
			c := run(t, app, cfg) // no Secret
			if fa.sawPrefix("GET", "/api/") {
				t.Error("reconciler contacted the app despite failing to resolve its API key")
			}
			if got := conditionStatus(t, c, app, "Synced"); got == conditionTrue {
				t.Error("Synced = True despite a missing secret")
			}
		})
	}
}

// Prune must never remove resources the operator did not create.
// status.managedResources is the record of what it made, so an empty record
// means everything in the app predates the operator.
func TestPruneLeavesUnmanagedResourcesAlone(t *testing.T) {
	for _, app := range pvrApps {
		t.Run(app.name, func(t *testing.T) {
			fa, url := newFakeArr(t, app.apiVer, map[string][]map[string]any{
				"downloadclient": {{"id": float64(7), "name": "hand-made-client"}},
			})
			cfg := app.build(url, buildOpts{downloadClient: true, prune: true})
			run(t, app, apiKeySecret(), cfg)

			// Guard against a vacuous pass: if the reconcile never reached the
			// app, the absence of a DELETE would prove nothing.
			if !fa.saw("POST", fa.path("downloadclient")) {
				t.Fatal("reconcile never reached the app, so the DELETE check is meaningless")
			}
			if fa.sawPrefix("DELETE", fa.path("downloadclient")) {
				t.Error("prune deleted a download client the operator never created")
			}
		})
	}
}

// The mirror: once a resource is recorded as operator-created, dropping it from
// the spec does prune it.
func TestPruneRemovesPreviouslyManagedResources(t *testing.T) {
	for _, app := range pvrApps {
		t.Run(app.name, func(t *testing.T) {
			fa, url := newFakeArr(t, app.apiVer, map[string][]map[string]any{
				"downloadclient": {{"id": float64(7), "name": "retired-client"}},
			})
			cfg := app.build(url, buildOpts{
				downloadClient: true, prune: true,
				managed: map[string][]string{"downloadClients": {"qBittorrent", "retired-client"}},
			})
			run(t, app, apiKeySecret(), cfg)

			if !fa.sawPrefix("DELETE", fa.path("downloadclient")) {
				t.Error("prune did not remove a resource it had previously created")
			}
		})
	}
}
