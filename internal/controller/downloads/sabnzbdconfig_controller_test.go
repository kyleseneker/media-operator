package downloads

import (
	"context"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	downloadsv1alpha1 "github.com/kyleseneker/media-operator/api/downloads/v1alpha1"
)

// fakeSabnzbd records the form parameters of every /api call. SABnzbd drives
// everything through one endpoint keyed by a "mode" parameter.
type fakeSabnzbd struct {
	mu    sync.Mutex
	calls []url.Values
}

func (f *fakeSabnzbd) sawMode(mode string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c.Get("mode") == mode {
			return true
		}
	}
	return false
}

// setValue returns the value written for a given config key, if any.
func (f *fakeSabnzbd) setValue(section, key string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c.Get("mode") == "set_config" && c.Get("section") == section && c.Get("keyword") == key {
			return c.Get("value"), true
		}
	}
	return "", false
}

func (f *fakeSabnzbd) serve(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(routableAddr(t), "0"))
	if err != nil {
		t.Skipf("cannot bind a routable listener: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		params := url.Values{}
		maps.Copy(params, r.Form)
		f.mu.Lock()
		f.calls = append(f.calls, params)
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status": true, "config": {}}`))
	}))
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL
}

func sabnzbdSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "sabnzbd-credentials", Namespace: "media"},
		Data:       map[string][]byte{"api-key": []byte("test-key")},
	}
}

func sabnzbdConfig(addr string) *downloadsv1alpha1.SabnzbdConfig {
	return &downloadsv1alpha1.SabnzbdConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "sabnzbd", Namespace: "media"},
		Spec: downloadsv1alpha1.SabnzbdConfigSpec{
			Connection: commonv1alpha1.AppConnection{
				URL:             addr,
				APIKeySecretRef: commonv1alpha1.SecretKeyRef{Name: "sabnzbd-credentials", Key: "api-key"},
			},
		},
	}
}

func runSabnzbd(t *testing.T, cfg *downloadsv1alpha1.SabnzbdConfig, withSecret bool) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("adding client-go scheme: %v", err)
	}
	if err := downloadsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding downloads scheme: %v", err)
	}
	b := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&downloadsv1alpha1.SabnzbdConfig{}).WithObjects(cfg)
	if withSecret {
		b = b.WithObjects(sabnzbdSecret())
	}
	c := b.Build()
	r := &SabnzbdConfigReconciler{Client: c, Scheme: scheme, Recorder: events.NewFakeRecorder(50)}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "sabnzbd", Namespace: "media"},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
}

func TestSabnzbdWritesDeclaredCategory(t *testing.T) {
	app := &fakeSabnzbd{}
	addr := app.serve(t)

	cfg := sabnzbdConfig(addr)
	cfg.Spec.Categories = []downloadsv1alpha1.SabnzbdCategory{{Name: "tv", Dir: "/downloads/tv"}}
	runSabnzbd(t, cfg, true)

	if !app.sawMode("version") {
		t.Fatal("never health-checked the app")
	}
	// Categories are written positionally as "categories,<index>".
	if got, ok := app.setValue("categories,0", "dir"); !ok || got != "/downloads/tv" {
		t.Errorf("category dir = %q (found=%v), want /downloads/tv", got, ok)
	}
	if got, ok := app.setValue("categories,0", "name"); !ok || got != "tv" {
		t.Errorf("category name = %q (found=%v), want tv", got, ok)
	}
}

func TestSabnzbdMissingSecretDoesNotContactApp(t *testing.T) {
	app := &fakeSabnzbd{}
	addr := app.serve(t)

	runSabnzbd(t, sabnzbdConfig(addr), false)

	if app.sawMode("version") {
		t.Error("contacted the app despite failing to resolve its API key")
	}
}
