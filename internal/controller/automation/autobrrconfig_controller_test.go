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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		Name: "autobrr-credentials", Namespace: "media",
		Data: map[string][]byte{"api-key": []byte("test-key")},
	}
}

func autobrrConfig(url string) *automationv1alpha1.AutobrrConfig {
	return &automationv1alpha1.AutobrrConfig{
		Name: "autobrr", Namespace: "media",
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
	require.NoError(t, clientgoscheme.AddToScheme(scheme), "adding client-go scheme")
	require.NoError(t, automationv1alpha1.AddToScheme(scheme), "adding automation scheme")
	b := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&automationv1alpha1.AutobrrConfig{}).WithObjects(cfg)
	if withSecret {
		b = b.WithObjects(autobrrSecret())
	}
	c := b.Build()
	r := &AutobrrConfigReconciler{Client: c, Scheme: scheme, Recorder: events.NewFakeRecorder(50)}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		Name: "autobrr", Namespace: "media",
	})
	require.NoError(t, err, "reconcile")
	return c
}

func autobrrCondition(t *testing.T, c client.Client, condType string) string {
	t.Helper()
	var cfg automationv1alpha1.AutobrrConfig
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "autobrr", Namespace: "media"}, &cfg), "fetching config")
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

	assert.True(t, app.saw("POST", "/api/filters"), "declared filter was never created")
	assert.Equal(t, conditionTrue, autobrrCondition(t, c, "Synced"), "Synced")
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

	require.True(t, app.saw("GET", "/api/filters"), "reconcile never listed filters, so the duplicate check is meaningless")
	assert.False(t, app.saw("POST", "/api/filters"), "created a duplicate of a filter that already existed")
}

func TestAutobrrMissingSecretDoesNotContactApp(t *testing.T) {
	app := &fakeAutobrr{}
	url := app.serve(t)

	c := runAutobrr(t, autobrrConfig(url), false)

	assert.False(t, app.saw("GET", "/api/healthz/liveness"), "contacted the app despite failing to resolve its API key")
	assert.NotEqual(t, conditionTrue, autobrrCondition(t, c, "Synced"), "Synced = True despite a missing secret")
}

func observedAutobrr(url string, indexer string) *automationv1alpha1.AutobrrConfig {
	cfg := autobrrConfig(url)
	cfg.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{DriftPolicy: new("observe")}
	cfg.Spec.Indexers = []automationv1alpha1.AutobrrIndexer{{Name: indexer, Implementation: "torznab"}}
	return cfg
}

func autobrrWrites(f *fakeAutobrr) []string {
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

func TestAutobrrObserveReportsMissingResourcesWithoutWriting(t *testing.T) {
	app := &fakeAutobrr{}
	c := runAutobrr(t, observedAutobrr(app.serve(t), "torznab-a"), true)

	assert.Empty(t, autobrrWrites(app), "observe mode wrote to Autobrr")
	var cfg automationv1alpha1.AutobrrConfig
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "autobrr", Namespace: "media"}, &cfg))
	synced := cfg.Status.Conditions[slices.IndexFunc(cfg.Status.Conditions, func(c metav1.Condition) bool { return c.Type == "Synced" })]
	assert.Equal(t, "DriftDetected", synced.Reason)
}

func TestAutobrrObserveMatchesWhatExists(t *testing.T) {
	app := &fakeAutobrr{existing: map[string][]map[string]any{
		"indexer": {{"id": float64(2), "name": "torznab-a", "enabled": true, "implementation": "torznab"}},
	}}
	c := runAutobrr(t, observedAutobrr(app.serve(t), "torznab-a"), true)

	require.True(t, app.saw("GET", "/api/indexer"), "observe never read the indexers")
	assert.Empty(t, autobrrWrites(app))
	assert.Equal(t, conditionTrue, autobrrCondition(t, c, "Synced"))
}

// Autobrr accepts only its own constants; "qBittorrent" is answered with
// "invalid download client type".
func TestAutobrrTypeMapsToAutobrrConstants(t *testing.T) {
	for in, want := range map[string]string{
		"qBittorrent": "QBITTORRENT", "Deluge": "DELUGE_V2", "WatchFolder": "WATCH_FOLDER", "Webhook": "WEBHOOK",
	} {
		assert.Equal(t, want, autobrrType(in), in)
	}
	assert.Equal(t, "NONE", autobrrIRCAuth("None"))
	assert.Equal(t, "NICKSERV", autobrrIRCAuth("NickServ"))
}

// A stored resource that already holds every declared value, with Autobrr's
// redacted password, must not be rewritten.
func TestUpsertAutobrrSkipsWhatAlreadyMatches(t *testing.T) {
	existing := []map[string]any{{
		"id": float64(4), "name": "qBittorrent", "type": "QBITTORRENT", "password": "<redacted>", "tls": false,
	}}
	obj := map[string]any{"name": "qBittorrent", "type": "QBITTORRENT", "password": "s3cret"}
	updated := false
	ab := &autobrrSync{}
	err := ab.upsert("download client", "qBittorrent", existing, obj,
		func() error { require.Fail(t, "must not create"); return nil },
		func(int) error { updated = true; return nil })
	require.NoError(t, err)
	assert.False(t, updated, "an unchanged download client was rewritten")

	obj["type"] = "DELUGE_V2"
	require.NoError(t, ab.upsert("download client", "qBittorrent", existing, obj,
		func() error { return nil }, func(int) error { updated = true; return nil }))
	assert.True(t, updated, "a real change must be written")
}

func TestAddRequiredFilterListsKeepsDeclaredValues(t *testing.T) {
	filter := map[string]any{"codecs": []string{"x265"}}
	addRequiredFilterLists(filter)
	assert.Equal(t, []string{"x265"}, filter["codecs"])
	assert.Equal(t, []string{}, filter["containers"], "Autobrr rejects a null containers column")
}
