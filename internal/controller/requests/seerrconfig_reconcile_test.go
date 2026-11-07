package requests

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
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	requestsv1alpha1 "github.com/kyleseneker/media-operator/api/requests/v1alpha1"
)

// fakeSeerr records requests and the bodies posted to the auth endpoints, so
// the login payload can be asserted rather than just the fact of a call. Like
// Seerr's request validator, it rejects query parameters the API does not
// declare; the operator sends none.
type fakeSeerr struct {
	mu          sync.Mutex
	requests    []string
	rejected    []string
	authBodies  map[string]map[string]any
	initialized bool
	// libraries is what a Jellyfin library sync finds, keyed by "id" with an
	// "enabled" flag the library PUT updates.
	libraries []map[string]any
}

func (f *fakeSeerr) saw(method, path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.requests, method+" "+path)
}

// order returns the position of the first matching request, or -1.
func (f *fakeSeerr) order(method, path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Index(f.requests, method+" "+path)
}

func (f *fakeSeerr) libraryEnabled(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, lib := range f.libraries {
		if lib["id"] == id {
			return lib["enabled"] == true
		}
	}
	return false
}

func (f *fakeSeerr) authBody(path string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.authBodies[path]
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

func (f *fakeSeerr) serve(t *testing.T) string {
	t.Helper()
	if f.authBodies == nil {
		f.authBodies = map[string]map[string]any{}
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(routableAddr(t), "0"))
	if err != nil {
		t.Skipf("cannot bind a routable listener: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		if body != nil {
			f.authBodies[r.URL.Path] = body
		}

		w.Header().Set("Content-Type", "application/json")
		if r.URL.RawQuery != "" {
			f.rejected = append(f.rejected, r.Method+" "+r.URL.RequestURI())
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "Unknown query parameter"})
			return
		}
		const libraryPrefix = "/api/v1/settings/jellyfin/library/"
		switch {
		case r.URL.Path == "/api/v1/settings/public":
			_ = json.NewEncoder(w).Encode(map[string]any{"initialized": f.initialized})
		case r.URL.Path == "/api/v1/settings/main":
			_ = json.NewEncoder(w).Encode(map[string]any{"apiKey": "seerr-generated-key"})
		case r.URL.Path == "/api/v1/settings/initialize" && r.Method == http.MethodPost:
			f.initialized = true
			_ = json.NewEncoder(w).Encode(map[string]any{"initialized": true})
		case r.URL.Path == libraryPrefix+"sync" && r.Method == http.MethodPost:
			if len(f.libraries) == 0 {
				w.WriteHeader(http.StatusNotFound)
				_ = json.NewEncoder(w).Encode(map[string]any{"message": "SYNC_ERROR_NO_LIBRARIES"})
				return
			}
			_ = json.NewEncoder(w).Encode(f.libraries)
		case strings.HasPrefix(r.URL.Path, libraryPrefix) && r.Method == http.MethodPut:
			id := strings.TrimPrefix(r.URL.Path, libraryPrefix)
			for _, lib := range f.libraries {
				if lib["id"] == id {
					lib["enabled"] = body["enabled"]
					_ = json.NewEncoder(w).Encode(lib)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "Library does not exist."})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL
}

func seerrSecret() *corev1.Secret {
	return &corev1.Secret{
		Name: "seerr-secrets", Namespace: "media",
		Data: map[string][]byte{
			"api-key":  []byte("supplied-key"),
			"username": []byte("admin"),
			"password": []byte("hunter2"),
			"token":    []byte("plex-token"),
		},
	}
}

func seerrConfig(addr string) *requestsv1alpha1.SeerrConfig {
	return &requestsv1alpha1.SeerrConfig{
		Name: "seerr", Namespace: "media",
		Spec: requestsv1alpha1.SeerrConfigSpec{
			Connection: requestsv1alpha1.SeerrConnection{URL: addr},
		},
	}
}

func runSeerr(t *testing.T, cfg *requestsv1alpha1.SeerrConfig) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("adding client-go scheme: %v", err)
	}
	if err := requestsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding requests scheme: %v", err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&requestsv1alpha1.SeerrConfig{}).
		WithObjects(cfg, seerrSecret()).Build()
	r := &SeerrConfigReconciler{Client: c, Scheme: scheme, Recorder: events.NewFakeRecorder(50)}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		Name: "seerr", Namespace: "media",
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
}

// A supplied API key is used directly. Logging in anyway would be pointless and,
// on a Jellyfin-backed Seerr, would need admin credentials the user never gave.
func TestSeerrSuppliedAPIKeySkipsLogin(t *testing.T) {
	app := &fakeSeerr{initialized: true}
	addr := app.serve(t)

	cfg := seerrConfig(addr)
	cfg.Spec.Connection.APIKeySecretRef = &commonv1alpha1.SecretKeyRef{Name: "seerr-secrets", Key: "api-key"}
	runSeerr(t, cfg)

	if app.saw("POST", "/api/v1/auth/jellyfin") || app.saw("POST", "/api/v1/auth/plex") {
		t.Error("logged in despite being given an API key")
	}
	if !app.saw("GET", "/api/v1/settings/public") {
		t.Fatal("reconcile never reached the app, so the login check is meaningless")
	}
}

// Without an API key the operator logs in and then reads the generated key back
// out of the main settings.
func TestSeerrWithoutAPIKeyLogsInAndFetchesKey(t *testing.T) {
	app := &fakeSeerr{initialized: true}
	addr := app.serve(t)

	port := 8096
	cfg := seerrConfig(addr)
	cfg.Spec.JellyfinAuth = &requestsv1alpha1.SeerrJellyfinAuth{
		Hostname:          "jellyfin",
		Port:              &port,
		UrlBase:           "/jf",
		UsernameSecretRef: commonv1alpha1.SecretKeyRef{Name: "seerr-secrets", Key: "username"},
		PasswordSecretRef: commonv1alpha1.SecretKeyRef{Name: "seerr-secrets", Key: "password"},
	}
	runSeerr(t, cfg)

	if !app.saw("POST", "/api/v1/auth/jellyfin") {
		t.Fatal("never logged in")
	}
	if !app.saw("GET", "/api/v1/settings/main") {
		t.Error("never read the API key back after logging in")
	}
}

// Seerr routes the login by serverType and builds the target URL from urlBase.
// Omitting either sends the request to the wrong place, which fails as an
// opaque auth error rather than anything diagnosable.
func TestSeerrJellyfinLoginCarriesServerTypeAndURLBase(t *testing.T) {
	app := &fakeSeerr{initialized: true}
	addr := app.serve(t)

	port := 8096
	cfg := seerrConfig(addr)
	cfg.Spec.JellyfinAuth = &requestsv1alpha1.SeerrJellyfinAuth{
		Hostname:          "jellyfin",
		Port:              &port,
		UrlBase:           "/jf",
		UsernameSecretRef: commonv1alpha1.SecretKeyRef{Name: "seerr-secrets", Key: "username"},
		PasswordSecretRef: commonv1alpha1.SecretKeyRef{Name: "seerr-secrets", Key: "password"},
	}
	runSeerr(t, cfg)

	body := app.authBody("/api/v1/auth/jellyfin")
	if body == nil {
		t.Fatal("no jellyfin login body captured")
	}
	if body["serverType"] == nil {
		t.Error("login payload omits serverType")
	}
	if body["urlBase"] != "/jf" {
		t.Errorf("urlBase = %v, want /jf", body["urlBase"])
	}
	if body["username"] != "admin" {
		t.Errorf("username = %v, want the resolved secret value", body["username"])
	}
}

func TestSeerrPlexLoginSendsToken(t *testing.T) {
	app := &fakeSeerr{initialized: true}
	addr := app.serve(t)

	cfg := seerrConfig(addr)
	cfg.Spec.PlexAuth = &requestsv1alpha1.SeerrPlexAuth{
		Hostname:       "plex",
		TokenSecretRef: commonv1alpha1.SecretKeyRef{Name: "seerr-secrets", Key: "token"},
	}
	runSeerr(t, cfg)

	body := app.authBody("/api/v1/auth/plex")
	if body == nil {
		t.Fatal("no plex login body captured")
	}
	if body["authToken"] != "plex-token" {
		t.Errorf("authToken = %v, want the resolved secret value", body["authToken"])
	}
}

func firstRunJellyfinConfig(addr string) *requestsv1alpha1.SeerrConfig {
	port := 8096
	cfg := seerrConfig(addr)
	cfg.Spec.Connection.APIKeySecretRef = &commonv1alpha1.SecretKeyRef{Name: "seerr-secrets", Key: "api-key"}
	cfg.Spec.JellyfinAuth = &requestsv1alpha1.SeerrJellyfinAuth{
		Hostname:          "jellyfin",
		Port:              &port,
		UsernameSecretRef: commonv1alpha1.SecretKeyRef{Name: "seerr-secrets", Key: "username"},
		PasswordSecretRef: commonv1alpha1.SecretKeyRef{Name: "seerr-secrets", Key: "password"},
	}
	return cfg
}

// First-time setup refreshes the library list from Jellyfin, enables every
// library, and only then completes initialization. Seerr 3.5.0 removed the
// GET .../library?sync&enable form; a request using it is rejected, setup
// never completes, and a rebuilt Seerr is left uninitialized.
func TestSeerrFirstRunEnablesJellyfinLibraries(t *testing.T) {
	app := &fakeSeerr{libraries: []map[string]any{
		{"id": "movies", "name": "Movies", "enabled": false},
		{"id": "shows", "name": "Shows", "enabled": false},
	}}
	addr := app.serve(t)

	runSeerr(t, firstRunJellyfinConfig(addr))

	if len(app.rejected) > 0 {
		t.Fatalf("sent query parameters Seerr does not declare: %v", app.rejected)
	}
	for _, id := range []string{"movies", "shows"} {
		if !app.libraryEnabled(id) {
			t.Errorf("library %s was not enabled", id)
		}
	}
	initialize := app.order("POST", "/api/v1/settings/initialize")
	if initialize < 0 {
		t.Fatal("setup never completed initialization")
	}
	if last := app.order("PUT", "/api/v1/settings/jellyfin/library/shows"); last > initialize {
		t.Error("initialized before every library was enabled")
	}
}

// Seerr's library sync answers 404 while Jellyfin has no libraries yet, as on a
// rebuild where the Jellyfin operator has not created them. Finishing setup then
// would leave Seerr with nothing enabled, and setup never runs again.
func TestSeerrFirstRunWaitsForJellyfinLibraries(t *testing.T) {
	app := &fakeSeerr{}
	addr := app.serve(t)

	runSeerr(t, firstRunJellyfinConfig(addr))

	if !app.saw("POST", "/api/v1/settings/jellyfin/library/sync") {
		t.Fatal("never asked Seerr to sync the Jellyfin libraries")
	}
	if app.saw("POST", "/api/v1/settings/initialize") {
		t.Error("completed initialization with no libraries to enable")
	}
}
