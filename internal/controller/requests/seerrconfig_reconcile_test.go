package requests

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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	requestsv1alpha1 "github.com/kyleseneker/media-operator/api/requests/v1alpha1"
)

// fakeSeerr records requests and the bodies posted to the auth endpoints, so
// the login payload can be asserted rather than just the fact of a call.
type fakeSeerr struct {
	mu          sync.Mutex
	requests    []string
	authBodies  map[string]map[string]any
	initialized bool
}

func (f *fakeSeerr) saw(method, path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.requests, method+" "+path)
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
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		if body != nil {
			f.authBodies[r.URL.Path] = body
		}
		init := f.initialized
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/settings/public":
			_ = json.NewEncoder(w).Encode(map[string]any{"initialized": init})
		case "/api/v1/settings/main":
			_ = json.NewEncoder(w).Encode(map[string]any{"apiKey": "seerr-generated-key"})
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
		ObjectMeta: metav1.ObjectMeta{Name: "seerr-secrets", Namespace: "media"},
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
		ObjectMeta: metav1.ObjectMeta{Name: "seerr", Namespace: "media"},
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
		NamespacedName: types.NamespacedName{Name: "seerr", Namespace: "media"},
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
