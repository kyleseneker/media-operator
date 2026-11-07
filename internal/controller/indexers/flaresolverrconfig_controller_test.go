package indexers

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	indexersv1alpha1 "github.com/kyleseneker/media-operator/api/indexers/v1alpha1"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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
		Name: "flaresolverr", Namespace: "media",
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
	require.NoError(t, clientgoscheme.AddToScheme(scheme), "adding client-go scheme")
	require.NoError(t, indexersv1alpha1.AddToScheme(scheme), "adding indexers scheme")
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&indexersv1alpha1.FlareSolverrConfig{}).
		WithObjects(cfg).Build()
	r := &FlareSolverrConfigReconciler{Client: c, Scheme: scheme, Recorder: events.NewFakeRecorder(50)}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		Name: "flaresolverr", Namespace: "media",
	})
	require.NoError(t, err, "reconcile")
	return c
}

func TestFlareSolverrCreatesDeclaredSessions(t *testing.T) {
	app := &fakeFlareSolverr{}
	url := app.serve(t)

	c := runFlare(t, flareConfig(url, false, nil, "indexer-a"))

	assert.True(t, app.ran("sessions.create", "indexer-a"), "declared session was never created")
	var cfg indexersv1alpha1.FlareSolverrConfig
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "flaresolverr", Namespace: "media"}, &cfg), "fetching config")
	assert.Contains(t, cfg.Status.ManagedResources["sessions"], "indexer-a", "session not recorded as managed")
}

// Prowlarr opens FlareSolverr sessions on demand. Destroying one mid-request
// breaks the indexer it was opened for, so a session the operator did not
// create must survive even with prune enabled.
func TestFlareSolverrLeavesForeignSessionsAlone(t *testing.T) {
	app := &fakeFlareSolverr{sessions: []string{"opened-by-prowlarr"}}
	url := app.serve(t)

	runFlare(t, flareConfig(url, true, nil, "indexer-a"))

	require.True(t, app.ran("sessions.create", "indexer-a"), "reconcile never reached the app, so the destroy check is meaningless")
	assert.False(t, app.ran("sessions.destroy", "opened-by-prowlarr"), "destroyed a session the operator never created")
}

// Without prune, dropping a session from the spec leaves it running, matching
// every other resource type.
func TestFlareSolverrKeepsDroppedSessionWithoutPrune(t *testing.T) {
	app := &fakeFlareSolverr{sessions: []string{"retired"}}
	url := app.serve(t)

	runFlare(t, flareConfig(url, false, []string{"retired"}, "indexer-a"))

	assert.False(t, app.ran("sessions.destroy", "retired"), "destroyed a session with prune disabled")
}

// With prune on and the session recorded as operator-created, dropping it from
// the spec does tear it down.
func TestFlareSolverrPrunesItsOwnRetiredSession(t *testing.T) {
	app := &fakeFlareSolverr{sessions: []string{"retired"}}
	url := app.serve(t)

	runFlare(t, flareConfig(url, true, []string{"retired"}, "indexer-a"))

	assert.True(t, app.ran("sessions.destroy", "retired"), "prune did not remove a session it had previously created")
}

func TestFlareSolverrObserveDoesNotCreatePruneOrClaimSessions(t *testing.T) {
	app := &fakeFlareSolverr{sessions: []string{"owned", "foreign"}}
	cfg := flareConfig(app.serve(t), true, []string{"owned"}, "missing")
	policy := "observe"
	cfg.Spec.Reconcile.DriftPolicy = &policy
	c := runFlare(t, cfg)
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: cfg.Name, Namespace: cfg.Namespace}, cfg))
	require.Equal(t, 2, cfg.Status.ActiveSessions, "active sessions")
	require.Equal(t, []string{"owned"}, cfg.Status.ManagedResources["sessions"], "ownership changed")
	app.mu.Lock()
	defer app.mu.Unlock()
	for _, cmd := range app.commands {
		assert.Equal(t, "sessions.list ", cmd, "unexpected observe command")
	}
	require.True(t, slices.ContainsFunc(cfg.Status.Conditions, func(condition metav1.Condition) bool {
		return condition.Type == "Synced" && condition.Reason == "DriftDetected"
	}), "drift was not reported: %+v", cfg.Status.Conditions)
}

func TestFlareSolverrDeletePolicyAndOwnership(t *testing.T) {
	app := &fakeFlareSolverr{sessions: []string{"owned", "foreign"}}
	cfg := flareConfig(app.serve(t), false, []string{"owned"})
	policy := "delete"
	cfg.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{DeletionPolicy: &policy}
	cfg.Finalizers = []string{ctrlcommon.Finalizer}
	now := metav1.Now()
	cfg.DeletionTimestamp = &now
	c := runFlare(t, cfg)
	require.True(t, app.ran("sessions.destroy", "owned"), "cleanup violated ownership")
	require.False(t, app.ran("sessions.destroy", "foreign"), "cleanup violated ownership")
	err := c.Get(context.Background(), client.ObjectKeyFromObject(cfg), &indexersv1alpha1.FlareSolverrConfig{})
	require.True(t, apierrors.IsNotFound(err), "finalizer not released: %v", err)
}

func TestFlareSolverrDoesNotAdoptExistingSessions(t *testing.T) {
	app := &fakeFlareSolverr{sessions: []string{"manual"}}
	cfg := flareConfig(app.serve(t), false, nil, "manual")
	c := runFlare(t, cfg)
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(cfg), cfg))
	require.Empty(t, cfg.Status.ManagedResources["sessions"], "manual session adopted")
}

func TestFlareSolverrPruneLimitPreservesOwnership(t *testing.T) {
	names := make([]string, 0, 26)
	for i := range 26 {
		names = append(names, fmt.Sprintf("session-%d", i))
	}
	app := &fakeFlareSolverr{sessions: names}
	cfg := flareConfig(app.serve(t), true, names)
	c := runFlare(t, cfg)
	require.NoError(t, c.Get(context.Background(), client.ObjectKeyFromObject(cfg), cfg))
	require.Equal(t, names, cfg.Status.ManagedResources["sessions"], "ownership lost when pruning was refused")
	for _, name := range names {
		require.False(t, app.ran("sessions.destroy", name), "prune limit ignored")
	}
}
