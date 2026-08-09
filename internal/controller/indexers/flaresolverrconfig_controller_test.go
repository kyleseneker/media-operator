package indexers

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

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	indexersv1alpha1 "github.com/kyleseneker/media-operator/api/indexers/v1alpha1"
)

// fakeFlareSolverr speaks the single-endpoint /v1 command protocol and records
// which commands it was asked to run.
type fakeFlareSolverr struct {
	mu       sync.Mutex
	commands []string
	sessions []string
}

func (f *fakeFlareSolverr) ran(cmd, session string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.commands, cmd+" "+session)
}

func (f *fakeFlareSolverr) serve(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(routableAddr(t), "0"))
	if err != nil {
		t.Skipf("cannot bind a routable listener: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Cmd     string `json:"cmd"`
			Session string `json:"session"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		f.mu.Lock()
		f.commands = append(f.commands, body.Cmd+" "+body.Session)
		sessions := slices.Clone(f.sessions)
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{"status": "ok", "message": ""}
		if body.Cmd == "sessions.list" {
			resp["sessions"] = sessions
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL
}

func flareConfig(url string, prune bool, managed []string, sessions ...string) *indexersv1alpha1.FlareSolverrConfig {
	cfg := &indexersv1alpha1.FlareSolverrConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "flaresolverr", Namespace: "media"},
		Spec: indexersv1alpha1.FlareSolverrConfigSpec{
			Connection: indexersv1alpha1.FlareSolverrConnection{URL: url},
		},
	}
	for _, s := range sessions {
		cfg.Spec.Sessions = append(cfg.Spec.Sessions, indexersv1alpha1.FlareSolverrSession{Name: s})
	}
	if prune {
		yes := true
		cfg.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{Prune: &yes}
	}
	if managed != nil {
		cfg.Status.ManagedResources = map[string][]string{"sessions": managed}
	}
	return cfg
}

func runFlare(t *testing.T, cfg *indexersv1alpha1.FlareSolverrConfig) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("adding client-go scheme: %v", err)
	}
	if err := indexersv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("adding indexers scheme: %v", err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&indexersv1alpha1.FlareSolverrConfig{}).
		WithObjects(cfg).Build()
	r := &FlareSolverrConfigReconciler{Client: c, Scheme: scheme, Recorder: events.NewFakeRecorder(50)}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "flaresolverr", Namespace: "media"},
	}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return c
}

func TestFlareSolverrCreatesDeclaredSessions(t *testing.T) {
	app := &fakeFlareSolverr{}
	url := app.serve(t)

	c := runFlare(t, flareConfig(url, false, nil, "indexer-a"))

	if !app.ran("sessions.create", "indexer-a") {
		t.Error("declared session was never created")
	}
	var cfg indexersv1alpha1.FlareSolverrConfig
	if err := c.Get(context.Background(), types.NamespacedName{Name: "flaresolverr", Namespace: "media"}, &cfg); err != nil {
		t.Fatalf("fetching config: %v", err)
	}
	if !slices.Contains(cfg.Status.ManagedResources["sessions"], "indexer-a") {
		t.Errorf("session not recorded as managed: %+v", cfg.Status.ManagedResources)
	}
}

// Prowlarr opens FlareSolverr sessions on demand. Destroying one mid-request
// breaks the indexer it was opened for, so a session the operator did not
// create must survive even with prune enabled.
func TestFlareSolverrLeavesForeignSessionsAlone(t *testing.T) {
	app := &fakeFlareSolverr{sessions: []string{"opened-by-prowlarr"}}
	url := app.serve(t)

	runFlare(t, flareConfig(url, true, nil, "indexer-a"))

	if !app.ran("sessions.create", "indexer-a") {
		t.Fatal("reconcile never reached the app, so the destroy check is meaningless")
	}
	if app.ran("sessions.destroy", "opened-by-prowlarr") {
		t.Error("destroyed a session the operator never created")
	}
}

// Without prune, dropping a session from the spec leaves it running, matching
// every other resource type.
func TestFlareSolverrKeepsDroppedSessionWithoutPrune(t *testing.T) {
	app := &fakeFlareSolverr{sessions: []string{"retired"}}
	url := app.serve(t)

	runFlare(t, flareConfig(url, false, []string{"retired"}, "indexer-a"))

	if app.ran("sessions.destroy", "retired") {
		t.Error("destroyed a session with prune disabled")
	}
}

// With prune on and the session recorded as operator-created, dropping it from
// the spec does tear it down.
func TestFlareSolverrPrunesItsOwnRetiredSession(t *testing.T) {
	app := &fakeFlareSolverr{sessions: []string{"retired"}}
	url := app.serve(t)

	runFlare(t, flareConfig(url, true, []string{"retired"}, "indexer-a"))

	if !app.ran("sessions.destroy", "retired") {
		t.Error("prune did not remove a session it had previously created")
	}
}
