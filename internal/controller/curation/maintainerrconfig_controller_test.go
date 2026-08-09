package curation

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	corev1 "k8s.io/api/core/v1"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	curationv1alpha1 "github.com/kyleseneker/media-operator/api/curation/v1alpha1"
)

const conditionTrue = "True"

// fakeMaintainerr records requests and serves the rule list, so create-vs-update
// matching by rule name can be asserted.
type fakeMaintainerr struct {
	mu            sync.Mutex
	requests      []string
	existingRules []map[string]any
}

func (f *fakeMaintainerr) saw(method, path string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.requests, method+" "+path)
}

func (f *fakeMaintainerr) serve(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(routableAddr(t), "0"))
	if err != nil {
		t.Skipf("cannot bind a routable listener: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		rules := f.existingRules
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		case r.URL.Path == "/api/rules" && r.Method == http.MethodGet:
			if rules == nil {
				rules = []map[string]any{}
			}
			_ = json.NewEncoder(w).Encode(rules)
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

func maintainerrSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "maintainerr-credentials", Namespace: "media"},
		Data:       map[string][]byte{"api-key": []byte("test-key")},
	}
}

func maintainerrConfig(url string, rules ...curationv1alpha1.MaintainerrRule) *curationv1alpha1.MaintainerrConfig {
	return &curationv1alpha1.MaintainerrConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "maintainerr", Namespace: "media"},
		Spec: curationv1alpha1.MaintainerrConfigSpec{
			Connection: commonv1alpha1.AppConnection{
				URL:             url,
				APIKeySecretRef: commonv1alpha1.SecretKeyRef{Name: "maintainerr-credentials", Key: "api-key"},
			},
			Rules: rules,
		},
	}
}

func runMaintainerr(t *testing.T, cfg *curationv1alpha1.MaintainerrConfig) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("adding client-go scheme: %v", err)
	}
	if err := curationv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding curation scheme: %v", err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&curationv1alpha1.MaintainerrConfig{}).
		WithObjects(cfg, maintainerrSecret()).Build()
	r := &MaintainerrConfigReconciler{Client: c, Scheme: scheme, Recorder: events.NewFakeRecorder(50)}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "maintainerr", Namespace: "media"},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return c
}

func maintainerrCondition(t *testing.T, c client.Client, condType string) string {
	t.Helper()
	var cfg curationv1alpha1.MaintainerrConfig
	if err := c.Get(context.Background(), types.NamespacedName{Name: "maintainerr", Namespace: "media"}, &cfg); err != nil {
		t.Fatalf("fetching config: %v", err)
	}
	for _, cond := range cfg.Status.Conditions {
		if cond.Type == condType {
			return string(cond.Status)
		}
	}
	return ""
}

func sampleRule() curationv1alpha1.MaintainerrRule {
	return curationv1alpha1.MaintainerrRule{
		Name: "old movies", LibraryName: "Movies", MediaType: "movie", Action: "delete",
	}
}

func TestMaintainerrCreatesMissingRule(t *testing.T) {
	app := &fakeMaintainerr{}
	url := app.serve(t)

	c := runMaintainerr(t, maintainerrConfig(url, sampleRule()))

	if !app.saw("POST", "/api/rules") {
		t.Error("declared rule was never created")
	}
	if got := maintainerrCondition(t, c, "Synced"); got != conditionTrue {
		t.Errorf("Synced = %q, want True", got)
	}
}

// Rules are matched by name. An existing rule must be updated in place rather
// than duplicated on every reconcile.
func TestMaintainerrUpdatesExistingRuleRatherThanDuplicating(t *testing.T) {
	app := &fakeMaintainerr{existingRules: []map[string]any{
		{"id": float64(3), "name": "old movies"},
	}}
	url := app.serve(t)

	runMaintainerr(t, maintainerrConfig(url, sampleRule()))

	// Guard against a vacuous pass: without reaching the rule list, the absence
	// of a POST proves nothing.
	if !app.saw("GET", "/api/rules") {
		t.Fatal("reconcile never listed rules, so the duplicate check is meaningless")
	}
	if app.saw("POST", "/api/rules") {
		t.Error("created a duplicate of a rule that already existed")
	}
}

func TestMaintainerrMarksUnhealthyAppNotReady(t *testing.T) {
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

	c := runMaintainerr(t, maintainerrConfig(srv.URL, sampleRule()))
	if got := maintainerrCondition(t, c, "Ready"); got == conditionTrue {
		t.Error("Ready = True for an app failing its health check")
	}
}
