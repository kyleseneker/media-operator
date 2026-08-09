package automation

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

	automationv1alpha1 "github.com/kyleseneker/media-operator/api/automation/v1alpha1"
	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
)

const conditionTrue = "True"

type fakeAutobrr struct {
	mu       sync.Mutex
	requests []string
	existing map[string][]map[string]any
}

func (f *fakeAutobrr) saw(method, path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.requests, method+" "+path)
}

func (f *fakeAutobrr) serve(t *testing.T) string {
	t.Helper()
	if f.existing == nil {
		f.existing = map[string][]map[string]any{}
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(routableAddr(t), "0"))
	if err != nil {
		t.Skipf("cannot bind a routable listener: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		list := f.existing[strings.TrimPrefix(r.URL.Path, "/api/")]
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/healthz"):
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet:
			if list == nil {
				list = []map[string]any{}
			}
			_ = json.NewEncoder(w).Encode(list)
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1})
		}
	}))
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL
}

func autobrrSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "autobrr-credentials", Namespace: "media"},
		Data:       map[string][]byte{"api-key": []byte("test-key")},
	}
}

func autobrrConfig(url string) *automationv1alpha1.AutobrrConfig {
	return &automationv1alpha1.AutobrrConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "autobrr", Namespace: "media"},
		Spec: automationv1alpha1.AutobrrConfigSpec{
			Connection: commonv1alpha1.AppConnection{
				URL:             url,
				APIKeySecretRef: commonv1alpha1.SecretKeyRef{Name: "autobrr-credentials", Key: "api-key"},
			},
		},
	}
}

func runAutobrr(t *testing.T, cfg *automationv1alpha1.AutobrrConfig, withSecret bool) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("adding client-go scheme: %v", err)
	}
	if err := automationv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding automation scheme: %v", err)
	}
	b := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&automationv1alpha1.AutobrrConfig{}).WithObjects(cfg)
	if withSecret {
		b = b.WithObjects(autobrrSecret())
	}
	c := b.Build()
	r := &AutobrrConfigReconciler{Client: c, Scheme: scheme, Recorder: events.NewFakeRecorder(50)}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "autobrr", Namespace: "media"},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return c
}

func autobrrCondition(t *testing.T, c client.Client, condType string) string {
	t.Helper()
	var cfg automationv1alpha1.AutobrrConfig
	if err := c.Get(context.Background(), types.NamespacedName{Name: "autobrr", Namespace: "media"}, &cfg); err != nil {
		t.Fatalf("fetching config: %v", err)
	}
	for _, cond := range cfg.Status.Conditions {
		if cond.Type == condType {
			return string(cond.Status)
		}
	}
	return ""
}

func TestAutobrrCreatesDeclaredFilter(t *testing.T) {
	app := &fakeAutobrr{}
	url := app.serve(t)

	cfg := autobrrConfig(url)
	cfg.Spec.Filters = []automationv1alpha1.AutobrrFilter{{Name: "1080p web-dl"}}
	c := runAutobrr(t, cfg, true)

	if !app.saw("POST", "/api/filters") {
		t.Error("declared filter was never created")
	}
	if got := autobrrCondition(t, c, "Synced"); got != conditionTrue {
		t.Errorf("Synced = %q, want True", got)
	}
}

// Filters are matched by name, so an existing one must not be recreated on
// every reconcile.
func TestAutobrrDoesNotDuplicateExistingFilter(t *testing.T) {
	app := &fakeAutobrr{existing: map[string][]map[string]any{
		"filters": {{"id": float64(4), "name": "1080p web-dl"}},
	}}
	url := app.serve(t)

	cfg := autobrrConfig(url)
	cfg.Spec.Filters = []automationv1alpha1.AutobrrFilter{{Name: "1080p web-dl"}}
	runAutobrr(t, cfg, true)

	if !app.saw("GET", "/api/filters") {
		t.Fatal("reconcile never listed filters, so the duplicate check is meaningless")
	}
	if app.saw("POST", "/api/filters") {
		t.Error("created a duplicate of a filter that already existed")
	}
}

func TestAutobrrMissingSecretDoesNotContactApp(t *testing.T) {
	app := &fakeAutobrr{}
	url := app.serve(t)

	c := runAutobrr(t, autobrrConfig(url), false)

	if app.saw("GET", "/api/healthz/liveness") {
		t.Error("contacted the app despite failing to resolve its API key")
	}
	if got := autobrrCondition(t, c, "Synced"); got == conditionTrue {
		t.Error("Synced = True despite a missing secret")
	}
}
