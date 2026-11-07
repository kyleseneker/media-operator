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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	"github.com/kyleseneker/media-operator/internal/controller/internal/contract"
)

func TestBuildServicePayloadAlwaysSendsRequiredFields(t *testing.T) {
	required := []string{
		"name", "hostname", "port", "apiKey", "useSsl",
		"activeProfileId", "activeProfileName", "activeDirectory",
		"is4k", "isDefault",
	}

	svc := &requestsv1alpha1.SeerrServiceConnection{Name: "Sonarr", Hostname: "sonarr.svc"}
	payload := buildServicePayload(svc, "key")

	for _, key := range required {
		assert.Contains(t, payload, key, "%q missing; Seerr's OpenAPI marks it required and rejects the request with 400", key)
	}
}

func observeSeerr(t *testing.T, cfg *requestsv1alpha1.SeerrConfig) metav1.Condition {
	t.Helper()
	cfg.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{DriftPolicy: new("observe")}
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, requestsv1alpha1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&requestsv1alpha1.SeerrConfig{}).WithObjects(cfg, seerrSecret()).Build()
	r := &SeerrConfigReconciler{Client: c, Scheme: scheme, Recorder: events.NewFakeRecorder(50)}
	key := types.NamespacedName{Namespace: cfg.Namespace, Name: cfg.Name}
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key})
	require.NoError(t, err)
	require.NoError(t, c.Get(context.Background(), key, cfg))
	i := slices.IndexFunc(cfg.Status.Conditions, func(c metav1.Condition) bool { return c.Type == "Synced" })
	require.GreaterOrEqual(t, i, 0, "no Synced condition")
	return cfg.Status.Conditions[i]
}

func (f *fakeSeerr) writes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, r := range f.requests {
		if !strings.HasPrefix(r, http.MethodGet) {
			out = append(out, r)
		}
	}
	return out
}

// Signing in to a Seerr that has not been set up configures its media server,
// so observe mode must not even log in.
func TestSeerrObserveReportsMissingSetupWithoutLoggingIn(t *testing.T) {
	app := &fakeSeerr{}
	cfg := seerrConfig(app.serve(t))
	cfg.Spec.JellyfinAuth = &requestsv1alpha1.SeerrJellyfinAuth{
		Hostname:          "jellyfin",
		UsernameSecretRef: commonv1alpha1.SecretKeyRef{Name: "seerr-secrets", Key: "username"},
		PasswordSecretRef: commonv1alpha1.SecretKeyRef{Name: "seerr-secrets", Key: "password"},
	}
	synced := observeSeerr(t, cfg)
	assert.Empty(t, app.writes())
	assert.Equal(t, "DriftDetected", synced.Reason)
}

func TestSeerrObserveReportsDifferingSettingsWithoutWriting(t *testing.T) {
	app := &fakeSeerr{initialized: true}
	cfg := seerrConfig(app.serve(t))
	cfg.Spec.Connection.APIKeySecretRef = &commonv1alpha1.SecretKeyRef{Name: "seerr-secrets", Key: "api-key"}
	cfg.Spec.Main = &requestsv1alpha1.SeerrMain{ApplicationTitle: "Requests"}
	synced := observeSeerr(t, cfg)
	require.True(t, app.saw("GET", "/api/v1/settings/main"), "observe never read the settings")
	assert.Empty(t, app.writes())
	assert.Equal(t, "DriftDetected", synced.Reason)
}

func TestSeerrObserveMatchesWithNothingToCompare(t *testing.T) {
	app := &fakeSeerr{initialized: true}
	cfg := seerrConfig(app.serve(t))
	cfg.Spec.Connection.APIKeySecretRef = &commonv1alpha1.SecretKeyRef{Name: "seerr-secrets", Key: "api-key"}
	synced := observeSeerr(t, cfg)
	assert.Empty(t, app.writes())
	assert.Equal(t, metav1.ConditionTrue, synced.Status, synced.Message)
}

// Seerr returns its whole settings document; only declared values decide
// whether a save is needed.
func TestSeerrChangedComparesOnlyDeclaredSettings(t *testing.T) {
	current := map[string]any{"applicationTitle": "Seerr", "apiKey": "x", "defaultPermissions": float64(32)}
	assert.False(t, seerrDiffers(current, map[string]any{"applicationTitle": "Seerr", "defaultPermissions": 32}))
	assert.True(t, seerrDiffers(current, map[string]any{"applicationTitle": "Requests"}))
	assert.True(t, seerrDiffers(nil, map[string]any{"enabled": true}), "nothing stored yet")
}

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
	// services holds the Sonarr and Radarr connections, keyed by "sonarr" or
	// "radarr". Like Seerr, a POST appends one rather than replacing by name.
	services map[string][]map[string]any
}

func (f *fakeSeerr) serviceCount(kind string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.services[kind])
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
	if f.services == nil {
		f.services = map[string][]map[string]any{}
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
		case r.URL.Path == "/api/v1/settings/sonarr" || r.URL.Path == "/api/v1/settings/radarr":
			kind := strings.TrimPrefix(r.URL.Path, "/api/v1/settings/")
			if r.Method == http.MethodPost {
				body["id"] = float64(len(f.services[kind]))
				f.services[kind] = append(f.services[kind], body)
				_ = json.NewEncoder(w).Encode(body)
				return
			}
			_ = json.NewEncoder(w).Encode(append([]map[string]any{}, f.services[kind]...))
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
	require.NoError(t, clientgoscheme.AddToScheme(scheme), "adding client-go scheme")
	require.NoError(t, requestsv1alpha1.AddToScheme(scheme), "adding requests scheme")
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&requestsv1alpha1.SeerrConfig{}).
		WithObjects(cfg, seerrSecret()).Build()
	r := &SeerrConfigReconciler{Client: c, Scheme: scheme, Recorder: events.NewFakeRecorder(50)}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		Name: "seerr", Namespace: "media",
	})
	require.NoError(t, err, "reconcile")
}

// A supplied API key is used directly. Logging in anyway would be pointless and,
// on a Jellyfin-backed Seerr, would need admin credentials the user never gave.
func TestSeerrSuppliedAPIKeySkipsLogin(t *testing.T) {
	app := &fakeSeerr{initialized: true}
	addr := app.serve(t)

	cfg := seerrConfig(addr)
	cfg.Spec.Connection.APIKeySecretRef = &commonv1alpha1.SecretKeyRef{Name: "seerr-secrets", Key: "api-key"}
	runSeerr(t, cfg)

	assert.False(t, app.saw("POST", "/api/v1/auth/jellyfin"), "logged in despite being given an API key")
	assert.False(t, app.saw("POST", "/api/v1/auth/plex"), "logged in despite being given an API key")
	require.True(t, app.saw("GET", "/api/v1/settings/public"), "reconcile never reached the app, so the login check is meaningless")
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

	require.True(t, app.saw("POST", "/api/v1/auth/jellyfin"), "never logged in")
	assert.True(t, app.saw("GET", "/api/v1/settings/main"), "never read the API key back after logging in")
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
	require.NotNil(t, body, "no jellyfin login body captured")
	assert.NotNil(t, body["serverType"], "login payload omits serverType")
	assert.Equal(t, "/jf", body["urlBase"], "urlBase")
	assert.Equal(t, "admin", body["username"], "username is not the resolved secret value")
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
	require.NotNil(t, body, "no plex login body captured")
	assert.Equal(t, "plex-token", body["authToken"], "authToken is not the resolved secret value")
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

	require.Empty(t, app.rejected, "sent query parameters Seerr does not declare")
	for _, id := range []string{"movies", "shows"} {
		assert.True(t, app.libraryEnabled(id), "library %s was not enabled", id)
	}
	initialize := app.order("POST", "/api/v1/settings/initialize")
	require.GreaterOrEqual(t, initialize, 0, "setup never completed initialization")
	assert.LessOrEqual(t, app.order("PUT", "/api/v1/settings/jellyfin/library/shows"), initialize,
		"initialized before every library was enabled")
}

// Setup that fails partway runs again from the start on the next pass. Seerr
// appends a connection on every POST, so posting Sonarr and Radarr without
// looking first would leave one copy of each per attempt.
func TestSeerrRetriedSetupDoesNotDuplicateConnections(t *testing.T) {
	app := &fakeSeerr{} // no Jellyfin libraries yet, so setup fails and is retried
	addr := app.serve(t)
	port := 8989
	cfg := firstRunJellyfinConfig(addr)
	key := commonv1alpha1.SecretKeyRef{Name: "seerr-secrets", Key: "api-key"}
	cfg.Spec.Sonarr = &requestsv1alpha1.SeerrServiceConnection{Name: "Sonarr", Hostname: "sonarr", Port: &port, APIKeySecretRef: key}
	cfg.Spec.Radarr = &requestsv1alpha1.SeerrServiceConnection{Name: "Radarr", Hostname: "radarr", Port: &port, APIKeySecretRef: key}

	runSeerr(t, cfg.DeepCopy())
	runSeerr(t, cfg.DeepCopy())

	require.False(t, app.saw("POST", "/api/v1/settings/initialize"), "precondition: setup must not have completed")
	assert.Equal(t, 1, app.serviceCount("sonarr"), "each setup attempt added another Sonarr connection")
	assert.Equal(t, 1, app.serviceCount("radarr"), "each setup attempt added another Radarr connection")
}

// Seerr's library sync answers 404 while Jellyfin has no libraries yet, as on a
// rebuild where the Jellyfin operator has not created them. Finishing setup then
// would leave Seerr with nothing enabled, and setup never runs again.
func TestSeerrFirstRunWaitsForJellyfinLibraries(t *testing.T) {
	app := &fakeSeerr{}
	addr := app.serve(t)

	runSeerr(t, firstRunJellyfinConfig(addr))

	require.True(t, app.saw("POST", "/api/v1/settings/jellyfin/library/sync"), "never asked Seerr to sync the Jellyfin libraries")
	assert.False(t, app.saw("POST", "/api/v1/settings/initialize"), "completed initialization with no libraries to enable")
}

// Seerr stores a service connection wholesale and answers 2xx for keys it does not
// use, so a spec field the builder never maps is silently inert. APIKeySecretRef is
// resolved before the payload is built and lands as "apiKey".
func TestSeerrServiceFieldsAllReachThePayload(t *testing.T) {
	contract.AssertEveryFieldEmitted(t, &requestsv1alpha1.SeerrServiceConnection{},
		func(s *requestsv1alpha1.SeerrServiceConnection) map[string]any {
			return buildServicePayload(s, "resolved-key")
		})
}

// Seerr rejects a service that omits these, so they are sent even when the CR
// leaves them unset.
func TestSeerrServicePayloadAlwaysSendsRequiredKeys(t *testing.T) {
	got := buildServicePayload(&requestsv1alpha1.SeerrServiceConnection{
		Name: "Sonarr", Hostname: "sonarr.arr.svc",
	}, "resolved-key")

	for _, k := range []string{"name", "hostname", "apiKey", "port", "useSsl", "activeProfileId", "is4k", "isDefault"} {
		assert.Contains(t, got, k, "required key %q missing from a minimal service payload", k)
	}
	assert.Equal(t, "resolved-key", got["apiKey"], "apiKey is not the resolved secret value")
}
