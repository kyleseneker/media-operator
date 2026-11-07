package mediaservers

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
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
	mediaserversv1alpha1 "github.com/kyleseneker/media-operator/api/mediaservers/v1alpha1"
)

// fakePlex serves the two endpoints the reconciler drives: the section list and
// the preferences endpoint.
type fakePlex struct {
	mu           sync.Mutex
	requests     []string
	existingLibs []any
}

// sawSections reports whether the section-list endpoint saw the given method.
func (f *fakePlex) sawSections(method string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.requests, method+" /library/sections")
}

func (f *fakePlex) serve(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(routableAddr(t), "0"))
	if err != nil {
		t.Skipf("cannot bind a routable listener: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.Path)
		libs := f.existingLibs
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if libs == nil {
			libs = []any{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"MediaContainer": map[string]any{"Directory": libs},
		})
	}))
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL
}

func plexSecret() *corev1.Secret {
	return &corev1.Secret{
		Name: "plex-token", Namespace: "media",
		Data: map[string][]byte{"token": []byte("plex-token")},
	}
}

func plexConfig(addr string, libs ...mediaserversv1alpha1.PlexLibrary) *mediaserversv1alpha1.PlexConfig {
	return &mediaserversv1alpha1.PlexConfig{
		Name: "plex", Namespace: "media",
		Spec: mediaserversv1alpha1.PlexConfigSpec{
			Connection: mediaserversv1alpha1.PlexConnection{
				URL:            addr,
				TokenSecretRef: commonv1alpha1.SecretKeyRef{Name: "plex-token", Key: "token"},
			},
			Libraries: libs,
		},
	}
}

func runPlex(t *testing.T, cfg *mediaserversv1alpha1.PlexConfig, withSecret bool) {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme), "adding client-go scheme")
	require.NoError(t, mediaserversv1alpha1.AddToScheme(scheme), "adding mediaservers scheme")
	b := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&mediaserversv1alpha1.PlexConfig{}).WithObjects(cfg)
	if withSecret {
		b = b.WithObjects(plexSecret())
	}
	c := b.Build()
	r := &PlexConfigReconciler{Client: c, Scheme: scheme, Recorder: events.NewFakeRecorder(50)}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		Name: "plex", Namespace: "media",
	})
	require.NoError(t, err, "reconcile")
}

func TestPlexCreatesMissingLibrary(t *testing.T) {
	app := &fakePlex{}
	addr := app.serve(t)

	runPlex(t, plexConfig(addr, mediaserversv1alpha1.PlexLibrary{
		Name: "Movies", Type: "movie", Paths: []string{"/media/movies"},
	}), true)

	assert.True(t, app.sawSections("POST"), "declared library was never created")
}

// Plex libraries are create-only: an existing section by the same name must be
// left alone rather than added a second time.
func TestPlexLeavesExistingLibraryAlone(t *testing.T) {
	app := &fakePlex{existingLibs: []any{map[string]any{"title": "Movies", "key": "1"}}}
	addr := app.serve(t)

	runPlex(t, plexConfig(addr, mediaserversv1alpha1.PlexLibrary{
		Name: "Movies", Type: "movie", Paths: []string{"/media/movies"},
	}), true)

	require.True(t, app.sawSections("GET"), "reconcile never listed sections, so the create check is meaningless")
	assert.False(t, app.sawSections("POST"), "recreated a library that already existed")
}

func TestPlexMissingTokenDoesNotContactApp(t *testing.T) {
	app := &fakePlex{}
	addr := app.serve(t)

	runPlex(t, plexConfig(addr), false)

	assert.False(t, app.sawSections("GET"), "contacted the app despite failing to resolve its token")
}

func TestPlexObserveRejectedBeforeApplicationAccess(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, mediaserversv1alpha1.AddToScheme(scheme))
	policy := "observe"
	cfg := &mediaserversv1alpha1.PlexConfig{Name: "observe", Namespace: "media"}
	cfg.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{DriftPolicy: &policy}
	// No URL or Secrets: rejection must happen before application access or authentication.
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(cfg).WithObjects(cfg).Build()
	r := &PlexConfigReconciler{Client: c, Scheme: scheme}
	key := types.NamespacedName{Namespace: cfg.Namespace, Name: cfg.Name}
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key})
	require.NoError(t, err)
	require.NoError(t, c.Get(context.Background(), key, cfg))
	require.True(t, slices.ContainsFunc(cfg.Status.Conditions, func(condition metav1.Condition) bool {
		return condition.Type == "Synced" && condition.Reason == "InvalidConfig" && condition.Status == metav1.ConditionFalse
	}), "expected explicit unsupported-policy status, got %+v", cfg.Status.Conditions)
}
