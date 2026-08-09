package transcode

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
	transcodev1alpha1 "github.com/kyleseneker/media-operator/api/transcode/v1alpha1"
)

const conditionTrue = "True"

// fakeTdarr serves the cruddb endpoint Tdarr uses for every collection, plus
// status and node listing. Requests are recorded with their cruddb operation so
// insert-vs-update can be told apart.
type fakeTdarr struct {
	mu       sync.Mutex
	ops      []string
	docFound bool
}

func (f *fakeTdarr) ranOp(op string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.ops, op)
}

func (f *fakeTdarr) serve(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(routableAddr(t), "0"))
	if err != nil {
		t.Skipf("cannot bind a routable listener: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v2/status":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
		case "/api/v2/get-nodes":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		case "/api/v2/cruddb":
			var body struct {
				Data struct {
					Collection string `json:"collection"`
					Mode       string `json:"mode"`
				} `json:"data"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.ops = append(f.ops, body.Data.Mode)
			found := f.docFound
			f.mu.Unlock()

			if body.Data.Mode == "getById" {
				if !found {
					_ = json.NewEncoder(w).Encode(map[string]any{})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"_id": "lib1", "name": "Movies"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			f.mu.Lock()
			f.ops = append(f.ops, r.URL.Path)
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{})
		}
	}))
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL
}

func tdarrSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tdarr-credentials", Namespace: "media"},
		Data:       map[string][]byte{"api-key": []byte("test-key")},
	}
}

func tdarrConfig(url string) *transcodev1alpha1.TdarrConfig {
	return &transcodev1alpha1.TdarrConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "tdarr", Namespace: "media"},
		Spec: transcodev1alpha1.TdarrConfigSpec{
			Connection: commonv1alpha1.AppConnection{
				URL:             url,
				APIKeySecretRef: commonv1alpha1.SecretKeyRef{Name: "tdarr-credentials", Key: "api-key"},
			},
		},
	}
}

func runTdarr(t *testing.T, cfg *transcodev1alpha1.TdarrConfig, withSecret bool) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("adding client-go scheme: %v", err)
	}
	if err := transcodev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding transcode scheme: %v", err)
	}
	b := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&transcodev1alpha1.TdarrConfig{}).WithObjects(cfg)
	if withSecret {
		b = b.WithObjects(tdarrSecret())
	}
	c := b.Build()
	r := &TdarrConfigReconciler{Client: c, Scheme: scheme, Recorder: events.NewFakeRecorder(50)}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "tdarr", Namespace: "media"},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return c
}

func tdarrCondition(t *testing.T, c client.Client, condType string) string {
	t.Helper()
	var cfg transcodev1alpha1.TdarrConfig
	if err := c.Get(context.Background(), types.NamespacedName{Name: "tdarr", Namespace: "media"}, &cfg); err != nil {
		t.Fatalf("fetching config: %v", err)
	}
	for _, cond := range cfg.Status.Conditions {
		if cond.Type == condType {
			return string(cond.Status)
		}
	}
	return ""
}

// Libraries are upserted by ID: absent means insert.
func TestTdarrInsertsMissingLibrary(t *testing.T) {
	app := &fakeTdarr{docFound: false}
	url := app.serve(t)

	cfg := tdarrConfig(url)
	cfg.Spec.Libraries = []transcodev1alpha1.TdarrLibrary{{ID: "lib1", Name: "Movies", Folder: "/media/movies"}}
	c := runTdarr(t, cfg, true)

	if !app.ranOp("insert") {
		t.Error("missing library was never inserted")
	}
	if got := tdarrCondition(t, c, "Synced"); got != conditionTrue {
		t.Errorf("Synced = %q, want True", got)
	}
}

// Present means update, not a second insert that would duplicate the library.
func TestTdarrUpdatesExistingLibrary(t *testing.T) {
	app := &fakeTdarr{docFound: true}
	url := app.serve(t)

	cfg := tdarrConfig(url)
	cfg.Spec.Libraries = []transcodev1alpha1.TdarrLibrary{{ID: "lib1", Name: "Movies", Folder: "/media/movies"}}
	runTdarr(t, cfg, true)

	if !app.ranOp("getById") {
		t.Fatal("reconcile never looked the library up, so the insert check is meaningless")
	}
	if app.ranOp("insert") {
		t.Error("inserted a library that already existed")
	}
}

func TestTdarrMissingSecretDoesNotContactApp(t *testing.T) {
	app := &fakeTdarr{}
	url := app.serve(t)

	c := runTdarr(t, tdarrConfig(url), false)

	if app.ranOp("/api/v2/status") {
		t.Error("contacted the app despite failing to resolve its API key")
	}
	if got := tdarrCondition(t, c, "Synced"); got == conditionTrue {
		t.Error("Synced = True despite a missing secret")
	}
}
