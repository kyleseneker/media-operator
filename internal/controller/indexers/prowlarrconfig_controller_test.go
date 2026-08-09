package indexers

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
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
	indexersv1alpha1 "github.com/kyleseneker/media-operator/api/indexers/v1alpha1"
)

// fakeProwlarr records request bodies so payload shaping — notably tag label to
// ID resolution — can be asserted on, not just the fact that a call happened.
type fakeProwlarr struct {
	mu       sync.Mutex
	requests []string
	bodies   map[string][]map[string]any
	existing map[string][]map[string]any
}

func (f *fakeProwlarr) postsTo(path string) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bodies["POST "+path]
}

func (f *fakeProwlarr) sawPrefix(method, prefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.requests {
		if strings.HasPrefix(r, method+" "+prefix) {
			return true
		}
	}
	return false
}

func (f *fakeProwlarr) serve(t *testing.T) string {
	t.Helper()
	if f.bodies == nil {
		f.bodies = map[string][]map[string]any{}
	}
	if f.existing == nil {
		f.existing = map[string][]map[string]any{}
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(routableAddr(t), "0"))
	if err != nil {
		t.Skipf("cannot bind a routable listener: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		w.Header().Set("Content-Type", "application/json")

		switch r.Method {
		case http.MethodGet:
			f.mu.Lock()
			f.requests = append(f.requests, key)
			list := f.existing[strings.TrimPrefix(r.URL.Path, "/api/v1/")]
			f.mu.Unlock()
			if r.URL.Path == "/api/v1/system/status" {
				_ = json.NewEncoder(w).Encode(map[string]any{"version": "1.0.0"})
				return
			}
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
			f.mu.Lock()
			f.requests = append(f.requests, key)
			f.bodies[key] = append(f.bodies[key], body)
			// A created tag becomes visible to the follow-up ID lookup.
			if r.URL.Path == "/api/v1/tag" {
				body["id"] = float64(5)
				f.existing["tag"] = append(f.existing["tag"], body)
			} else {
				body["id"] = float64(99)
			}
			f.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(body)
		case http.MethodDelete:
			f.mu.Lock()
			f.requests = append(f.requests, key)
			f.mu.Unlock()
			w.WriteHeader(http.StatusOK)
		}
	}))
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL
}

func prowlarrSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "prowlarr-credentials", Namespace: "media"},
		Data:       map[string][]byte{"api-key": []byte("test-key")},
	}
}

func runProwlarr(t *testing.T, cfg *indexersv1alpha1.ProwlarrConfig, objs ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("adding client-go scheme: %v", err)
	}
	if err := indexersv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding indexers scheme: %v", err)
	}
	b := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&indexersv1alpha1.ProwlarrConfig{}).WithObjects(cfg)
	for _, o := range objs {
		b = b.WithObjects(o)
	}
	c := b.Build()
	r := &ProwlarrConfigReconciler{Client: c, Scheme: scheme, Recorder: events.NewFakeRecorder(50)}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "prowlarr", Namespace: "media"},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return c
}

func prowlarrConfig(url string) *indexersv1alpha1.ProwlarrConfig {
	return &indexersv1alpha1.ProwlarrConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "prowlarr", Namespace: "media"},
		Spec: indexersv1alpha1.ProwlarrConfigSpec{
			Connection: commonv1alpha1.AppConnection{
				URL:             url,
				APIKeySecretRef: commonv1alpha1.SecretKeyRef{Name: "prowlarr-credentials", Key: "api-key"},
			},
		},
	}
}

// Prowlarr takes tags as labels in the CR but its API wants IDs. The operator
// writes tags first, then resolves the labels it just created. Getting this
// wrong silently drops the association rather than erroring.
func TestProwlarrResolvesTagLabelsToIDs(t *testing.T) {
	app := &fakeProwlarr{}
	url := app.serve(t)

	cfg := prowlarrConfig(url)
	cfg.Spec.Tags = []commonv1alpha1.Tag{{Label: "flare"}}
	cfg.Spec.Indexers = []indexersv1alpha1.ProwlarrIndexer{{
		Name: "nzbgeek", Implementation: "Newznab", ConfigContract: "NewznabSettings",
		Tags: []string{"flare"},
	}}
	runProwlarr(t, cfg, prowlarrSecret())

	posts := app.postsTo("/api/v1/indexer")
	if len(posts) == 0 {
		t.Fatal("indexer was never created")
	}
	tags, _ := posts[0]["tags"].([]any)
	if len(tags) != 1 || tags[0] != float64(5) {
		t.Errorf("indexer tags = %v, want [5] resolved from label %q", tags, "flare")
	}
}

func TestProwlarrCreatesDeclaredIndexer(t *testing.T) {
	app := &fakeProwlarr{}
	url := app.serve(t)

	cfg := prowlarrConfig(url)
	cfg.Spec.Indexers = []indexersv1alpha1.ProwlarrIndexer{{
		Name: "nzbgeek", Implementation: "Newznab", ConfigContract: "NewznabSettings",
	}}
	c := runProwlarr(t, cfg, prowlarrSecret())

	if len(app.postsTo("/api/v1/indexer")) == 0 {
		t.Error("indexer was never created")
	}
	var got indexersv1alpha1.ProwlarrConfig
	if err := c.Get(context.Background(), types.NamespacedName{Name: "prowlarr", Namespace: "media"}, &got); err != nil {
		t.Fatalf("fetching config: %v", err)
	}
	if len(got.Status.ManagedResources["indexers"]) == 0 {
		t.Errorf("indexer not recorded as managed: %+v", got.Status.ManagedResources)
	}
}

func TestProwlarrPruneLeavesUnmanagedIndexerAlone(t *testing.T) {
	app := &fakeProwlarr{existing: map[string][]map[string]any{
		"indexer": {{"id": float64(7), "name": "hand-made"}},
	}}
	url := app.serve(t)

	yes := true
	cfg := prowlarrConfig(url)
	cfg.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{Prune: &yes}
	cfg.Spec.Indexers = []indexersv1alpha1.ProwlarrIndexer{{
		Name: "nzbgeek", Implementation: "Newznab", ConfigContract: "NewznabSettings",
	}}
	runProwlarr(t, cfg, prowlarrSecret())

	if len(app.postsTo("/api/v1/indexer")) == 0 {
		t.Fatal("reconcile never reached the app, so the DELETE check is meaningless")
	}
	if app.sawPrefix("DELETE", "/api/v1/indexer") {
		t.Error("prune deleted an indexer the operator never created")
	}
}

func TestProwlarrMissingSecretDoesNotContactApp(t *testing.T) {
	app := &fakeProwlarr{}
	url := app.serve(t)

	runProwlarr(t, prowlarrConfig(url)) // no Secret

	if app.sawPrefix("GET", "/api/v1/") {
		t.Error("contacted the app despite failing to resolve its API key")
	}
}
