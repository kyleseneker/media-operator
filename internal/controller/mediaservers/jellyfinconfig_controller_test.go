package mediaservers

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
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

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	mediaserversv1alpha1 "github.com/kyleseneker/media-operator/api/mediaservers/v1alpha1"
)

const conditionTrue = "True"

// fakeJellyfin implements the handful of endpoints the reconciler drives:
// the public info probe, the four startup-wizard steps, authentication, and
// the virtual-folder (library) list.
type fakeJellyfin struct {
	mu            sync.Mutex
	requests      []string
	setupComplete bool
	existingLibs  []map[string]any
}

func (f *fakeJellyfin) saw(method, path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.requests, method+" "+path)
}

// routableAddr returns a non-loopback IPv4 address. The operator's HTTP client
// blocks loopback and link-local as SSRF protection, so a fake on 127.0.0.1 is
// unreachable by design and assertions against it pass for the wrong reason.
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

func (f *fakeJellyfin) serve(t *testing.T) string {
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
		switch r.URL.Path {
		case "/System/Info/Public":
			f.mu.Lock()
			done := f.setupComplete
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"StartupWizardCompleted": done})
		case "/Users/AuthenticateByName":
			_ = json.NewEncoder(w).Encode(map[string]any{"AccessToken": "test-token"})
		case "/Library/VirtualFolders":
			if r.Method == http.MethodGet {
				f.mu.Lock()
				libs := f.existingLibs
				f.mu.Unlock()
				if libs == nil {
					libs = []map[string]any{}
				}
				_ = json.NewEncoder(w).Encode(libs)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			// Startup wizard steps and config posts.
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL
}

func adminSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "jellyfin-admin", Namespace: "media"},
		Data:       map[string][]byte{"username": []byte("admin"), "password": []byte("hunter2")},
	}
}

func jellyfinConfig(url string, libs ...mediaserversv1alpha1.JellyfinLibrary) *mediaserversv1alpha1.JellyfinConfig {
	return &mediaserversv1alpha1.JellyfinConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "jellyfin", Namespace: "media"},
		Spec: mediaserversv1alpha1.JellyfinConfigSpec{
			Connection: mediaserversv1alpha1.JellyfinConnection{URL: url},
			AdminUser: mediaserversv1alpha1.JellyfinAdminUser{
				UsernameSecretRef: commonv1alpha1.SecretKeyRef{Name: "jellyfin-admin", Key: "username"},
				PasswordSecretRef: commonv1alpha1.SecretKeyRef{Name: "jellyfin-admin", Key: "password"},
			},
			Libraries: libs,
		},
	}
}

func runJellyfin(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("adding client-go scheme: %v", err)
	}
	if err := mediaserversv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding mediaservers scheme: %v", err)
	}
	b := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&mediaserversv1alpha1.JellyfinConfig{})
	for _, o := range objs {
		b = b.WithObjects(o)
	}
	c := b.Build()
	r := &JellyfinConfigReconciler{Client: c, Scheme: scheme, Recorder: events.NewFakeRecorder(50)}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "jellyfin", Namespace: "media"},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return c
}

func jellyfinCondition(t *testing.T, c client.Client, condType string) string {
	t.Helper()
	var cfg mediaserversv1alpha1.JellyfinConfig
	if err := c.Get(context.Background(), types.NamespacedName{Name: "jellyfin", Namespace: "media"}, &cfg); err != nil {
		t.Fatalf("fetching config: %v", err)
	}
	for _, cond := range cfg.Status.Conditions {
		if cond.Type == condType {
			return string(cond.Status)
		}
	}
	return ""
}

// A fresh Jellyfin reports the wizard incomplete; the operator must drive it
// rather than leaving the server unconfigured.
func TestReconcileRunsSetupWizardOnFreshInstall(t *testing.T) {
	app := &fakeJellyfin{setupComplete: false}
	url := app.serve(t)

	c := runJellyfin(t, adminSecret(), jellyfinConfig(url))

	for _, step := range []string{"/Startup/User", "/Startup/Configuration", "/Startup/RemoteAccess", "/Startup/Complete"} {
		if !app.saw("POST", step) {
			t.Errorf("wizard step %s was never run", step)
		}
	}
	if got := jellyfinCondition(t, c, "Synced"); got != conditionTrue {
		t.Errorf("Synced = %q, want True", got)
	}
}

// An already-configured server must not be put back through the wizard, which
// would try to recreate the admin user.
func TestReconcileSkipsWizardWhenSetupComplete(t *testing.T) {
	app := &fakeJellyfin{setupComplete: true}
	url := app.serve(t)

	runJellyfin(t, adminSecret(), jellyfinConfig(url))

	if app.saw("POST", "/Startup/User") {
		t.Error("re-ran the setup wizard against an already-configured server")
	}
	if !app.saw("POST", "/Users/AuthenticateByName") {
		t.Error("never authenticated against the configured server")
	}
}

func TestReconcileCreatesMissingLibrary(t *testing.T) {
	app := &fakeJellyfin{setupComplete: true}
	url := app.serve(t)

	runJellyfin(t, adminSecret(), jellyfinConfig(url,
		mediaserversv1alpha1.JellyfinLibrary{Name: "Movies", CollectionType: "movies", Paths: []string{"/media/movies"}}))

	if !app.saw("POST", "/Library/VirtualFolders") {
		t.Error("declared library was never created")
	}
}

// Libraries are create-only: an existing one by the same name is left alone
// rather than recreated, which would duplicate or clobber it.
func TestReconcileLeavesExistingLibraryAlone(t *testing.T) {
	app := &fakeJellyfin{
		setupComplete: true,
		existingLibs:  []map[string]any{{"Name": "Movies"}},
	}
	url := app.serve(t)

	runJellyfin(t, adminSecret(), jellyfinConfig(url,
		mediaserversv1alpha1.JellyfinLibrary{Name: "Movies", CollectionType: "movies", Paths: []string{"/media/movies"}}))

	if app.saw("POST", "/Library/VirtualFolders") {
		t.Error("recreated a library that already existed")
	}
}

func TestReconcileMissingSecretDoesNotContactApp(t *testing.T) {
	app := &fakeJellyfin{setupComplete: true}
	url := app.serve(t)

	c := runJellyfin(t, jellyfinConfig(url)) // no Secret

	if app.saw("GET", "/System/Info/Public") {
		t.Error("contacted the app despite failing to resolve admin credentials")
	}
	if got := jellyfinCondition(t, c, "Synced"); got == conditionTrue {
		t.Error("Synced = True despite missing credentials")
	}
}
