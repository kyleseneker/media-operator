package downloads

import (
	"context"
	"crypto/rand"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	downloadsv1alpha1 "github.com/kyleseneker/media-operator/api/downloads/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/controller/internal/contract"
	"github.com/kyleseneker/media-operator/internal/reconciler"
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
		Name: "sabnzbd-credentials", Namespace: "media",
		Data: map[string][]byte{"api-key": []byte("test-key")},
	}
}

func sabnzbdConfig(addr string) *downloadsv1alpha1.SabnzbdConfig {
	return &downloadsv1alpha1.SabnzbdConfig{
		Name: "sabnzbd", Namespace: "media",
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
	require.NoError(t, clientgoscheme.AddToScheme(scheme), "adding client-go scheme")
	require.NoError(t, downloadsv1alpha1.AddToScheme(scheme), "adding downloads scheme")
	b := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&downloadsv1alpha1.SabnzbdConfig{}).WithObjects(cfg)
	if withSecret {
		b = b.WithObjects(sabnzbdSecret())
	}
	c := b.Build()
	r := &SabnzbdConfigReconciler{Client: c, Scheme: scheme, Recorder: events.NewFakeRecorder(50)}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		Name: "sabnzbd", Namespace: "media",
	})
	require.NoError(t, err, "reconcile")
}

func TestSabnzbdMissingSecretDoesNotContactApp(t *testing.T) {
	app := &fakeSabnzbd{}
	addr := app.serve(t)

	runSabnzbd(t, sabnzbdConfig(addr), false)

	assert.False(t, app.sawMode("version"), "contacted the app despite failing to resolve its API key")
}

func sabnzbdSpec() downloadsv1alpha1.SabnzbdConfigSpec {
	return downloadsv1alpha1.SabnzbdConfigSpec{
		Folders:    &downloadsv1alpha1.SabnzbdFolders{CompleteDir: "/done"},
		Servers:    []downloadsv1alpha1.SabnzbdServer{{Name: "Primary", Host: "news.example.com", Port: 563, SSL: new(true)}},
		Categories: []downloadsv1alpha1.SabnzbdCategory{{Name: "tv", Dir: "tv", Priority: new(0)}},
	}
}

// SABnzbd only accepts servers and categories addressed by name; the operator
// used to write "servers,0" and "categories,0", which SABnzbd rejected.
func TestSabnzbdPlanWritesServersAndCategoriesByName(t *testing.T) {
	servers := map[string]map[string]string{"Primary": sabnzbdServerValues(sabnzbdSpec().Servers[0])}
	writes := planSabnzbd(map[string]any{}, sabnzbdSpec(), servers, "plan-by-name")

	require.Len(t, writes, 3)
	byName := map[string]sabWrite{}
	for _, w := range writes {
		byName[w.section+"/"+w.name] = w
	}
	assert.Equal(t, "/done", byName["misc/complete_dir"].values["value"])
	assert.Equal(t, "news.example.com", byName["servers/Primary"].values["host"])
	assert.NotContains(t, byName["servers/Primary"].values, "name", "the name is the keyword; sending it as a value renames")
	assert.Equal(t, "tv", byName["categories/tv"].values["dir"])
}

// get_config reports numbers and 0/1 flags typed and the password masked, so a
// matching config needs no writes once the password's digest is remembered.
func TestSabnzbdPlanSkipsWhatAlreadyMatches(t *testing.T) {
	vals := sabnzbdServerValues(sabnzbdSpec().Servers[0])
	vals["password"] = "s3cret"
	servers := map[string]map[string]string{"Primary": vals}
	current := map[string]any{
		"misc": map[string]any{"complete_dir": "/done"},
		"servers": []any{map[string]any{
			"name": "Primary", "host": "news.example.com", "port": float64(563), "ssl": float64(1),
			"password": "**********", "timeout": float64(60),
		}},
		"categories": []any{map[string]any{"name": "tv", "dir": "tv", "priority": float64(0), "order": float64(2)}},
	}

	owner := freshOwner()
	first := planSabnzbd(current, sabnzbdSpec(), servers, owner)
	require.Len(t, first, 1, "only the server, whose masked password cannot be compared")
	assert.Equal(t, "servers", first[0].section)
	reconciler.RecordSecrets(first[0].secretKey, first[0].secretDigest)

	assert.Empty(t, planSabnzbd(current, sabnzbdSpec(), servers, owner))

	vals["password"] = "rotated"
	assert.Len(t, planSabnzbd(current, sabnzbdSpec(), servers, owner), 1, "a rotated password must be written")
}

// Observe mode has no record of a masked password it never wrote, so a server
// that otherwise matches is not drift after a restart.
func TestSabnzbdObservePlanDoesNotCountAnUnseenPassword(t *testing.T) {
	vals := sabnzbdServerValues(sabnzbdSpec().Servers[0])
	vals["password"] = "s3cret"
	current := map[string]any{
		"misc": map[string]any{"complete_dir": "/done"},
		"servers": []any{map[string]any{
			"name": "Primary", "host": "news.example.com", "port": float64(563), "ssl": float64(1),
			"password": "**********", "timeout": float64(60),
		}},
		"categories": []any{map[string]any{"name": "tv", "dir": "tv", "priority": float64(0), "order": float64(2)}},
	}
	spec := sabnzbdSpec()
	spec.Reconcile = &commonv1alpha1.ReconcileConfig{DriftPolicy: new("observe")}
	assert.Empty(t, planSabnzbd(current, spec, map[string]map[string]string{"Primary": vals}, "observe-unseen"))
}

func TestSabnzbdObserveReportsDriftWithoutWriting(t *testing.T) {
	app := &fakeSabnzbd{}
	cfg := sabnzbdConfig(app.serve(t))
	cfg.Spec.Categories = []downloadsv1alpha1.SabnzbdCategory{{Name: "tv", Dir: "tv"}}
	policy := "observe"
	cfg.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{DriftPolicy: &policy}

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, downloadsv1alpha1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&downloadsv1alpha1.SabnzbdConfig{}).WithObjects(cfg, sabnzbdSecret()).Build()
	r := &SabnzbdConfigReconciler{Client: c, Scheme: scheme}
	_, err := r.Reconcile(context.Background(), ctrl.Request{Name: "sabnzbd", Namespace: "media"})
	require.NoError(t, err)

	assert.True(t, app.sawMode("get_config"))
	assert.False(t, app.sawMode("set_config"), "observe must not write")
	var got downloadsv1alpha1.SabnzbdConfig
	require.NoError(t, c.Get(context.Background(), ctrl.Request{Name: "sabnzbd", Namespace: "media"}.NamespacedName, &got))
	synced := metav1.Condition{}
	for _, cond := range got.Status.Conditions {
		if cond.Type == "Synced" {
			synced = cond
		}
	}
	assert.Equal(t, metav1.ConditionFalse, synced.Status)
	assert.Equal(t, "DriftDetected", synced.Reason)
}

// A created server reads back with its password masked; the digest remembered
// at creation keeps the next pass from rewriting it.
func TestSabnzbdPlanRemembersTheCreatedPassword(t *testing.T) {
	vals := sabnzbdServerValues(sabnzbdSpec().Servers[0])
	vals["password"] = "s3cret"
	servers := map[string]map[string]string{"Primary": vals}
	spec := sabnzbdSpec()
	spec.Folders, spec.Categories = nil, nil

	owner := freshOwner()
	created := planSabnzbd(map[string]any{}, spec, servers, owner)
	require.Len(t, created, 1)
	reconciler.RecordSecrets(created[0].secretKey, created[0].secretDigest)

	readBack := map[string]any{"servers": []any{map[string]any{
		"name": "Primary", "host": "news.example.com", "port": float64(563), "ssl": float64(1), "password": "**********",
	}}}
	assert.Empty(t, planSabnzbd(readBack, spec, servers, owner))
}

// freshOwner stands in for a config's UID. The remembered password digests
// live for the process, keyed by owner, so a fixed owner would let a repeated
// run (-count) find the record an earlier run left.
func freshOwner() string {
	return "test-" + rand.Text()
}

// SABnzbd reports a misc switch as 0/1 until it is written and true/false after.
func TestSabnzbdPlanTreatsWrittenSwitchesAsSet(t *testing.T) {
	spec := downloadsv1alpha1.SabnzbdConfigSpec{General: &downloadsv1alpha1.SabnzbdGeneral{PreCheck: new(true)}}
	assert.Empty(t, planSabnzbd(map[string]any{"misc": map[string]any{"pre_check": true}}, spec, nil, "switch"))
	assert.Empty(t, planSabnzbd(map[string]any{"misc": map[string]any{"pre_check": float64(1)}}, spec, nil, "switch"))
	assert.Len(t, planSabnzbd(map[string]any{"misc": map[string]any{"pre_check": false}}, spec, nil, "switch"), 1)
}

func TestSabnzbdGeneralValuesCoversEveryField(t *testing.T) {
	contract.AssertEveryFieldEmitted(t, &downloadsv1alpha1.SabnzbdGeneral{}, buildSabnzbdGeneralValues)
}

func TestSabnzbdFolderValuesCoversEveryField(t *testing.T) {
	contract.AssertEveryFieldEmitted(t, &downloadsv1alpha1.SabnzbdFolders{}, buildSabnzbdFolderValues)
}
