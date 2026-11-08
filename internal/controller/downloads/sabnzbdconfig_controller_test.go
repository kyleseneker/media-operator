package downloads

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

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
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/controller/internal/contract"
	"github.com/kyleseneker/media-operator/internal/metrics"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

// fakeSabnzbd records the form parameters of every /api call. SABnzbd drives
// everything through one endpoint keyed by a "mode" parameter.
type fakeSabnzbd struct {
	mu     sync.Mutex
	calls  []url.Values
	config map[string]any
}

func (f *fakeSabnzbd) deleted(keyword string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c.Get("mode") == "del_config" && c.Get("keyword") == keyword {
			return true
		}
	}
	return false
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
		config := f.config
		if config == nil {
			config = map[string]any{}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": true, "config": config})
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
	contract.AssertEveryFieldEmitted(t, &downloadsv1alpha1.SabnzbdGeneral{}, buildSabnzbdGeneralValues,
		func(g *downloadsv1alpha1.SabnzbdGeneral) {
			g.UnwantedExtensions, g.CleanupList, g.HostWhitelist = []string{"exe"}, []string{"nfo"}, []string{"sabnzbd"}
			g.EncryptedRarAction, g.UnwantedExtensionsAction, g.UnwantedExtensionsMode = "fail", "pause", "allowlist"
		})
}

func TestSabnzbdValuesUseSABnzbdsForms(t *testing.T) {
	got := buildSabnzbdGeneralValues(&downloadsv1alpha1.SabnzbdGeneral{
		FailHopelessJobs:         new(true),
		EncryptedRarAction:       "fail",
		UnwantedExtensions:       []string{"exe", "lnk"},
		UnwantedExtensionsMode:   "blocklist",
		UnwantedExtensionsAction: "pause",
		HistoryRetention:         "days-delete",
		HistoryRetentionNumber:   new(30),
		Permissions:              "775",
	})
	assert.Equal(t, map[string]string{
		"fail_hopeless_jobs":            "1",
		"pause_on_pwrar":                "2",
		"unwanted_extensions":           "exe,lnk",
		"unwanted_extensions_mode":      "0",
		"action_on_unwanted_extensions": "1",
		"history_retention_option":      "days-delete",
		"history_retention_number":      "30",
		"permissions":                   "775",
	}, got)

	server := sabnzbdServerValues(downloadsv1alpha1.SabnzbdServer{
		Host: "news.example.com", Port: 563, SSLVerify: "medium", Optional: new(true), Required: new(false), Timeout: new(30),
	})
	assert.Equal(t, map[string]string{
		"host": "news.example.com", "port": "563", "ssl_verify": "2", "optional": "1", "required": "0", "timeout": "30",
	}, server)
	assert.Equal(t, map[string]string{"pp": "2"}, sabnzbdCategoryValues(downloadsv1alpha1.SabnzbdCategory{PostProcessing: "unpack"}))
}

// SABnzbd takes a list comma-separated and returns it as an array.
func TestSabnzbdPlanComparesListsAsWritten(t *testing.T) {
	spec := downloadsv1alpha1.SabnzbdConfigSpec{General: &downloadsv1alpha1.SabnzbdGeneral{UnwantedExtensions: []string{"exe", "lnk"}}}
	assert.Empty(t, planSabnzbd(map[string]any{"misc": map[string]any{"unwanted_extensions": []any{"exe", "lnk"}}}, spec, nil, "list"))
	assert.Len(t, planSabnzbd(map[string]any{"misc": map[string]any{"unwanted_extensions": []any{"exe"}}}, spec, nil, "list"), 1)
}

func TestSabnzbdHostWhitelistMustKeepTheOperatorsHost(t *testing.T) {
	for _, tt := range []struct {
		url       string
		whitelist []string
		ok        bool
	}{
		{"http://sabnzbd.media:8080", []string{"sabnzbd.media", "sab.example.com"}, true},
		{"http://sabnzbd.media:8080", []string{"sab.example.com"}, false},
		{"http://sabnzbd.media.svc.cluster.local:8080", []string{"sab.example.com"}, true},
		{"http://sabnzbd.media.svc.cluster.local.:8080", []string{"sab.example.com"}, true},
		{"http://10.0.0.5:8080", []string{"sab.example.com"}, true},
		{"http://localhost:8080", []string{"sab.example.com"}, true},
		{"http://sabnzbd:8080", nil, true},
	} {
		err := sabnzbdHostAllowed(tt.url, tt.whitelist)
		if tt.ok {
			assert.NoError(t, err, tt.url)
		} else {
			assert.ErrorContains(t, err, "must include sabnzbd.media,", tt.url)
		}
	}
}

func TestSabnzbdFolderValuesCoversEveryField(t *testing.T) {
	contract.AssertEveryFieldEmitted(t, &downloadsv1alpha1.SabnzbdFolders{}, buildSabnzbdFolderValues)
}

func runSabnzbdConfig(t *testing.T, cfg *downloadsv1alpha1.SabnzbdConfig) map[string][]string {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, downloadsv1alpha1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&downloadsv1alpha1.SabnzbdConfig{}).WithObjects(cfg, sabnzbdSecret()).Build()
	r := &SabnzbdConfigReconciler{Client: c, Scheme: scheme, Recorder: events.NewFakeRecorder(50)}
	_, err := r.Reconcile(context.Background(), ctrl.Request{Name: "sabnzbd", Namespace: "media"})
	require.NoError(t, err)
	var got downloadsv1alpha1.SabnzbdConfig
	if err := c.Get(context.Background(), ctrl.Request{Name: "sabnzbd", Namespace: "media"}.NamespacedName, &got); err != nil {
		return nil
	}
	return got.Status.ManagedResources
}

func sabnzbdEntries(names ...string) []any {
	out := make([]any, 0, len(names))
	for _, n := range names {
		out = append(out, map[string]any{"name": n})
	}
	return out
}

func TestSabnzbdPrunesOnlyServersItCreated(t *testing.T) {
	app := &fakeSabnzbd{config: map[string]any{"servers": sabnzbdEntries("old", "kept", "hand-made")}}
	cfg := sabnzbdConfig(app.serve(t))
	cfg.Spec.Servers = []downloadsv1alpha1.SabnzbdServer{{Name: "kept", Host: "news", Port: 563}}
	cfg.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{Prune: new(true)}
	cfg.Status.ManagedResources = map[string][]string{"servers": {"old", "kept"}}

	managed := runSabnzbdConfig(t, cfg)

	assert.True(t, app.deleted("old"), "an owned server left the spec and was not deleted")
	assert.False(t, app.deleted("hand-made"), "deleted a server the operator did not create")
	assert.False(t, app.deleted("kept"), "deleted a declared server")
	assert.Equal(t, []string{"kept"}, managed["servers"])
}

func TestSabnzbdRecordsTheServersItCreates(t *testing.T) {
	app := &fakeSabnzbd{}
	cfg := sabnzbdConfig(app.serve(t))
	cfg.Spec.Servers = []downloadsv1alpha1.SabnzbdServer{{Name: "Primary", Host: "news", Port: 563}}

	managed := runSabnzbdConfig(t, cfg)

	assert.Equal(t, []string{"Primary"}, managed["servers"])
}

func TestSabnzbdDeletionPolicyRemovesWhatItCreated(t *testing.T) {
	app := &fakeSabnzbd{config: map[string]any{
		"servers": sabnzbdEntries("old", "hand-made"), "categories": sabnzbdEntries("*", "tv", "movies"),
	}}
	cfg := sabnzbdConfig(app.serve(t))
	policy := ctrlcommon.DeletionPolicyDelete
	cfg.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{DeletionPolicy: &policy}
	cfg.Status.ManagedResources = map[string][]string{"servers": {"old"}, "categories": {"tv"}}
	now := metav1.NewTime(time.Now())
	cfg.DeletionTimestamp = &now
	cfg.Finalizers = []string{ctrlcommon.Finalizer}

	runSabnzbdConfig(t, cfg)

	assert.True(t, app.deleted("old"))
	assert.True(t, app.deleted("tv"))
	assert.False(t, app.deleted("hand-made"), "deleted a server the operator did not create")
	assert.False(t, app.deleted("movies"), "deleted a category the operator did not create")
}

func TestSabnzbdObserveReportsWhatPruneWouldDelete(t *testing.T) {
	app := &fakeSabnzbd{config: map[string]any{"servers": sabnzbdEntries("observe-old")}}
	cfg := sabnzbdConfig(app.serve(t))
	cfg.Spec.Servers = []downloadsv1alpha1.SabnzbdServer{{Name: "kept", Host: "news", Port: 563}}
	observe := "observe"
	cfg.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{Prune: new(true), DriftPolicy: &observe}
	cfg.Status.ManagedResources = map[string][]string{"servers": {"observe-old"}}
	observed := metrics.DriftObservedTotal.WithLabelValues("sabnzbd", "servers", "observe-old")
	before := testutil.ToFloat64(observed)

	managed := runSabnzbdConfig(t, cfg)

	assert.False(t, app.deleted("observe-old"), "observe mode deleted a server")
	assert.Equal(t, before+1, testutil.ToFloat64(observed))
	assert.Equal(t, []string{"observe-old"}, managed["servers"])
}

// Since SABnzbd 4, auto_sort holds a field and direction; the old "1" sorted nothing.
func TestSabnzbdAutoSortWritesASortOrder(t *testing.T) {
	assert.Equal(t, sabnzbdAgeSort, buildSabnzbdGeneralValues(&downloadsv1alpha1.SabnzbdGeneral{AutoSort: new(true)})["auto_sort"])
	assert.Empty(t, buildSabnzbdGeneralValues(&downloadsv1alpha1.SabnzbdGeneral{AutoSort: new(false)})["auto_sort"])

	spec := downloadsv1alpha1.SabnzbdConfigSpec{General: &downloadsv1alpha1.SabnzbdGeneral{AutoSort: new(true)}}
	assert.Empty(t, planSabnzbd(map[string]any{"misc": map[string]any{"auto_sort": "avg_age asc"}}, spec, nil, "sort"))
	assert.Len(t, planSabnzbd(map[string]any{"misc": map[string]any{"auto_sort": "1"}}, spec, nil, "sort"), 1,
		"the value earlier versions wrote must be corrected")
}

func TestSabnzbdFolderScriptDirWinsOverTheDeprecatedGeneralOne(t *testing.T) {
	spec := downloadsv1alpha1.SabnzbdConfigSpec{
		General: &downloadsv1alpha1.SabnzbdGeneral{ScriptDir: "/general"},
		Folders: &downloadsv1alpha1.SabnzbdFolders{ScriptDir: "/folders"},
	}
	writes := planSabnzbd(map[string]any{"misc": map[string]any{}}, spec, nil, "dirs")
	require.Len(t, writes, 1)
	assert.Equal(t, "/folders", writes[0].values["value"])
}
