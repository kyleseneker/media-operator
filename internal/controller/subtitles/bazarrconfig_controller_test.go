package subtitles

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	subtitlesv1alpha1 "github.com/kyleseneker/media-operator/api/subtitles/v1alpha1"
)

const conditionTrue = "True"

// fakeBazarr captures posted form bodies. Bazarr takes form-encoded settings
// rather than JSON, so the assertions are about which form keys were sent.
type fakeBazarr struct {
	mu    sync.Mutex
	forms []url.Values
	paths []string
}

// formValue returns the first value posted for key across all requests.
func (f *fakeBazarr) formValue(key string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, form := range f.forms {
		if v := form.Get(key); v != "" {
			return v, true
		}
	}
	return "", false
}

// anyKeyContains reports whether any posted form key contains the substring.
func (f *fakeBazarr) anyKeyContains(sub string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, form := range f.forms {
		for k := range form {
			if strings.Contains(k, sub) {
				return true
			}
		}
	}
	return false
}

func (f *fakeBazarr) sawPath(p string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.paths, p)
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

func (f *fakeBazarr) serve(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(routableAddr(t), "0"))
	if err != nil {
		t.Skipf("cannot bind a routable listener: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		if len(r.PostForm) > 0 {
			f.forms = append(f.forms, r.PostForm)
		}
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL
}

func bazarrSecrets() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "bazarr-secrets", Namespace: "media"},
		Data: map[string][]byte{
			"bazarr-api-key": []byte("bazarr-key"),
			"sonarr-api-key": []byte("sonarr-key"),
		},
	}
}

func bazarrConfig(addr string) *subtitlesv1alpha1.BazarrConfig {
	return &subtitlesv1alpha1.BazarrConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "bazarr", Namespace: "media"},
		Spec: subtitlesv1alpha1.BazarrConfigSpec{
			Connection: commonv1alpha1.AppConnection{
				URL:             addr,
				APIKeySecretRef: commonv1alpha1.SecretKeyRef{Name: "bazarr-secrets", Key: "bazarr-api-key"},
			},
		},
	}
}

func runBazarr(t *testing.T, cfg *subtitlesv1alpha1.BazarrConfig, objs ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("adding client-go scheme: %v", err)
	}
	if err := subtitlesv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding subtitles scheme: %v", err)
	}
	b := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&subtitlesv1alpha1.BazarrConfig{}).WithObjects(cfg)
	for _, o := range objs {
		b = b.WithObjects(o)
	}
	c := b.Build()
	r := &BazarrConfigReconciler{Client: c, Scheme: scheme, Recorder: events.NewFakeRecorder(50)}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "bazarr", Namespace: "media"},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return c
}

func bazarrCondition(t *testing.T, c client.Client, condType string) string {
	t.Helper()
	var cfg subtitlesv1alpha1.BazarrConfig
	if err := c.Get(context.Background(), types.NamespacedName{Name: "bazarr", Namespace: "media"}, &cfg); err != nil {
		t.Fatalf("fetching config: %v", err)
	}
	for _, cond := range cfg.Status.Conditions {
		if cond.Type == condType {
			return string(cond.Status)
		}
	}
	return ""
}

// The CR names a Secret for the Sonarr API key. Bazarr must receive the
// resolved value under its own "apikey" form field, and must never receive the
// "apiKeySecretRef" field name, which would send Bazarr a Kubernetes reference
// it cannot use in place of a credential.
func TestBazarrSubstitutesResolvedAPIKeyIntoForm(t *testing.T) {
	app := &fakeBazarr{}
	addr := app.serve(t)

	cfg := bazarrConfig(addr)
	port := 8989
	cfg.Spec.SonarrConnection = &subtitlesv1alpha1.BazarrAppConnection{
		Host:            "sonarr",
		Port:            &port,
		APIKeySecretRef: commonv1alpha1.SecretKeyRef{Name: "bazarr-secrets", Key: "sonarr-api-key"},
	}
	runBazarr(t, cfg, bazarrSecrets())

	got, ok := app.formValue("settings-sonarr-apikey")
	if !ok {
		t.Fatal("sonarr api key was never sent")
	}
	if got != "sonarr-key" {
		t.Errorf("settings-sonarr-apikey = %q, want the resolved secret value", got)
	}
	if app.anyKeyContains("apiKeySecretRef") {
		t.Error("sent the Kubernetes secret reference to Bazarr instead of the resolved value")
	}
}

func TestBazarrSendsLanguagesAsJSON(t *testing.T) {
	app := &fakeBazarr{}
	addr := app.serve(t)

	cfg := bazarrConfig(addr)
	cfg.Spec.Languages = &subtitlesv1alpha1.BazarrLanguages{Enabled: []string{"en", "de"}}
	runBazarr(t, cfg, bazarrSecrets())

	got, ok := app.formValue("settings-general-enabled_languages")
	if !ok {
		t.Fatal("enabled languages were never sent")
	}
	var langs []string
	if err := json.Unmarshal([]byte(got), &langs); err != nil {
		t.Fatalf("enabled_languages is not JSON: %q", got)
	}
	if len(langs) != 2 || langs[0] != "en" {
		t.Errorf("enabled languages = %v, want [en de]", langs)
	}
}

func TestBazarrReportsSyncedOnHealthyApp(t *testing.T) {
	app := &fakeBazarr{}
	addr := app.serve(t)

	c := runBazarr(t, bazarrConfig(addr), bazarrSecrets())

	if !app.sawPath("/api/system/health") {
		t.Error("never health-checked the app")
	}
	if got := bazarrCondition(t, c, "Synced"); got != conditionTrue {
		t.Errorf("Synced = %q, want True", got)
	}
}

func TestBazarrMissingSecretDoesNotContactApp(t *testing.T) {
	app := &fakeBazarr{}
	addr := app.serve(t)

	c := runBazarr(t, bazarrConfig(addr)) // no Secret

	if app.sawPath("/api/system/health") {
		t.Error("contacted the app despite failing to resolve its API key")
	}
	if got := bazarrCondition(t, c, "Synced"); got == conditionTrue {
		t.Error("Synced = True despite a missing secret")
	}
}
