package transcode

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	transcodev1alpha1 "github.com/kyleseneker/media-operator/api/transcode/v1alpha1"
)

const (
	conditionTrue = "True"
	modeGetAll    = "getAll"
)

// fakeTdarr serves the cruddb endpoint Tdarr uses for every collection, plus
// status and node listing. Requests are recorded with their cruddb operation so
// insert-vs-update can be told apart.
type fakeTdarr struct {
	mu       sync.Mutex
	ops      []string
	docFound bool
	// stored, when set, is what getById returns for a found document.
	stored map[string]any

	// vars is what VariablesJSONDB returns for getAll, and writes records every
	// call made against that collection so the variable sync can be asserted.
	vars   []map[string]any
	writes []varWrite
}

type varWrite struct {
	mode  string
	docID string
	obj   map[string]any
}

func (f *fakeTdarr) ranOp(op string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.ops, op)
}

func (f *fakeTdarr) varWrites() []varWrite {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]varWrite(nil), f.writes...)
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
					Collection string         `json:"collection"`
					Mode       string         `json:"mode"`
					DocID      string         `json:"docID"`
					Obj        map[string]any `json:"obj"`
				} `json:"data"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.ops = append(f.ops, body.Data.Mode)
			found := f.docFound
			if body.Data.Collection == variablesCollection {
				if body.Data.Mode != modeGetAll {
					f.writes = append(f.writes, varWrite{mode: body.Data.Mode, docID: body.Data.DocID, obj: body.Data.Obj})
				}
				if body.Data.Mode == modeGetAll {
					vars := append([]map[string]any(nil), f.vars...)
					f.mu.Unlock()
					_ = json.NewEncoder(w).Encode(vars)
					return
				}
			}
			f.mu.Unlock()

			if body.Data.Mode == modeGetAll {
				_ = json.NewEncoder(w).Encode([]map[string]any{})
				return
			}

			if body.Data.Mode == "getById" {
				if !found {
					_ = json.NewEncoder(w).Encode(map[string]any{})
					return
				}
				doc := map[string]any{"_id": "lib1", "name": "Movies"}
				if f.stored != nil {
					doc = f.stored
				}
				_ = json.NewEncoder(w).Encode(doc)
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
		Name: "tdarr-credentials", Namespace: "media",
		Data: map[string][]byte{"api-key": []byte("test-key")},
	}
}

func tdarrConfig(url string) *transcodev1alpha1.TdarrConfig {
	return &transcodev1alpha1.TdarrConfig{
		Name: "tdarr", Namespace: "media",
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
	require.NoError(t, clientgoscheme.AddToScheme(scheme), "adding client-go scheme")
	require.NoError(t, transcodev1alpha1.AddToScheme(scheme), "adding transcode scheme")
	b := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&transcodev1alpha1.TdarrConfig{}).WithObjects(cfg)
	if withSecret {
		b = b.WithObjects(tdarrSecret())
	}
	c := b.Build()
	r := &TdarrConfigReconciler{Client: c, Scheme: scheme, Recorder: events.NewFakeRecorder(50)}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		Name: "tdarr", Namespace: "media",
	})
	require.NoError(t, err, "reconcile")
	return c
}

func tdarrCondition(t *testing.T, c client.Client, condType string) string {
	t.Helper()
	var cfg transcodev1alpha1.TdarrConfig
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "tdarr", Namespace: "media"}, &cfg), "fetching config")
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

	assert.True(t, app.ranOp("insert"), "missing library was never inserted")
	assert.Equal(t, conditionTrue, tdarrCondition(t, c, "Synced"), "Synced")
}

// Present means update, not a second insert that would duplicate the library.
func TestTdarrUpdatesExistingLibrary(t *testing.T) {
	app := &fakeTdarr{docFound: true}
	url := app.serve(t)

	cfg := tdarrConfig(url)
	cfg.Spec.Libraries = []transcodev1alpha1.TdarrLibrary{{ID: "lib1", Name: "Movies", Folder: "/media/movies"}}
	runTdarr(t, cfg, true)

	require.True(t, app.ranOp("getById"), "reconcile never looked the library up, so the insert check is meaningless")
	assert.False(t, app.ranOp("insert"), "inserted a library that already existed")
}

// Updating on every pass rewrote each library every few minutes. A stored
// document that already holds every declared value, plus Tdarr's own keys,
// must be left alone.
func TestTdarrLeavesAMatchingLibraryAlone(t *testing.T) {
	lib := transcodev1alpha1.TdarrLibrary{ID: "lib1", Name: "Movies", Folder: "/media/movies"}
	raw, err := json.Marshal(buildTdarrLibraryDoc(lib))
	require.NoError(t, err)
	var stored map[string]any
	require.NoError(t, json.Unmarshal(raw, &stored))
	stored["totalTranscodeCount"] = float64(12)
	app := &fakeTdarr{docFound: true, stored: stored}

	cfg := tdarrConfig(app.serve(t))
	cfg.Spec.Libraries = []transcodev1alpha1.TdarrLibrary{lib}
	runTdarr(t, cfg, true)

	require.True(t, app.ranOp("getById"))
	assert.False(t, app.ranOp("update"), "an unchanged library was rewritten")
	assert.False(t, app.ranOp("insert"))
}

func TestTdarrMissingSecretDoesNotContactApp(t *testing.T) {
	app := &fakeTdarr{}
	url := app.serve(t)

	c := runTdarr(t, tdarrConfig(url), false)

	assert.False(t, app.ranOp("/api/v2/status"), "contacted the app despite failing to resolve its API key")
	assert.NotEqual(t, conditionTrue, tdarrCondition(t, c, "Synced"), "Synced = True despite a missing secret")
}

// A library with no flow attached scans and then does nothing, so flowId must
// reach Tdarr's library document.
func TestLibraryDocCarriesFlowAttachment(t *testing.T) {
	lib := transcodev1alpha1.TdarrLibrary{
		ID: "movies", Name: "Movies", Folder: "/data/media/movies",
		FlowID: "strip-tracks", PluginStack: []string{"Tdarr_Plugin_MC93_Migz4CleanSubs"},
	}
	got := buildTdarrLibraryDoc(lib)
	assert.Equal(t, "strip-tracks", got["flowId"], "flowId missing from library doc")
	assert.Contains(t, got, "pluginStack", "pluginStack missing from library doc")
}

// Community flows read library variables as {{{args.variables.user.<name>}}},
// so they must land under the user namespace, not at the top level.
func TestLibraryVariablesNestUnderUser(t *testing.T) {
	lib := transcodev1alpha1.TdarrLibrary{
		ID: "movies", Name: "Movies",
		Variables: map[string]string{"audio_language": "eng", "do_audio_clean": "true"},
	}
	got := buildTdarrLibraryDoc(lib)

	vars, ok := got["variables"].(map[string]any)
	require.True(t, ok, "variables missing from library doc: %v", got)
	user, ok := vars["user"].(map[string]any)
	require.True(t, ok, "variables must nest under \"user\"; flows read args.variables.user.*, got %v", vars)
	assert.Equal(t, "eng", user["audio_language"], "unexpected user variables")
	assert.Equal(t, "true", user["do_audio_clean"], "unexpected user variables")
}

func TestLibraryDocAlwaysCarriesSchedule(t *testing.T) {
	got := buildTdarrLibraryDoc(transcodev1alpha1.TdarrLibrary{ID: "movies", Name: "Movies"})

	schedule, ok := got["schedule"].([]any)
	require.True(t, ok, "schedule missing from library doc: %v", got)
	require.Len(t, schedule, scheduleHoursPerWeek, "schedule must cover every hour of the week")
	for i, slot := range schedule {
		entry, ok := slot.(map[string]any)
		require.True(t, ok, "slot %d must be an enabled entry, got %#v", i, slot)
		require.Equal(t, true, entry["checked"], "slot %d must be an enabled entry", i)
	}
}

func TestLibraryDocAlwaysCarriesFoldersToIgnoreAsString(t *testing.T) {
	got := buildTdarrLibraryDoc(transcodev1alpha1.TdarrLibrary{ID: "movies", Name: "Movies"})
	require.Equal(t, "", got["foldersToIgnore"], "foldersToIgnore must default to an empty string")

	got = buildTdarrLibraryDoc(transcodev1alpha1.TdarrLibrary{
		ID: "movies", Name: "Movies", FoldersToIgnore: "extras,featurettes",
	})
	assert.Equal(t, "extras,featurettes", got["foldersToIgnore"], "foldersToIgnore not carried through")
}

// Flows read library variables as args.userVariables.library.<key>, which Tdarr
// serves from VariablesJSONDB. A variable written only onto the library document
// reaches the flow as undefined, which fails the flow at the first plugin to
// parse it.
func TestLibraryVariablesSyncToVariablesCollection(t *testing.T) {
	app := &fakeTdarr{docFound: true}
	url := app.serve(t)

	cfg := tdarrConfig(url)
	cfg.Spec.Libraries = []transcodev1alpha1.TdarrLibrary{{
		ID: "movies", Name: "Movies", Folder: "/data/media/movies",
		Variables: map[string]string{"bitrate_1080p": "2500"},
	}}
	runTdarr(t, cfg, true)

	writes := app.varWrites()
	require.Len(t, writes, 1, "expected one variable write")
	w := writes[0]
	assert.Equal(t, "insert", w.mode, "missing variable must be inserted")
	assert.Equal(t, "bitrate_1080p", w.obj["key"], "unexpected variable document: %v", w.obj)
	assert.Equal(t, "2500", w.obj["value"], "unexpected variable document: %v", w.obj)
	assert.Equal(t, "library:movies", w.obj["type"], "variable must be scoped to the library")
}

func TestLibraryVariablesUpdateChangedValueOnly(t *testing.T) {
	app := &fakeTdarr{docFound: true, vars: []map[string]any{
		{"_id": "v1", "key": "bitrate_1080p", "value": "2500", "type": "library:movies"},
		{"_id": "v2", "key": "v_cq", "value": "20", "type": "library:movies"},
	}}
	url := app.serve(t)

	cfg := tdarrConfig(url)
	cfg.Spec.Libraries = []transcodev1alpha1.TdarrLibrary{{
		ID: "movies", Name: "Movies", Folder: "/data/media/movies",
		Variables: map[string]string{"bitrate_1080p": "2500", "v_cq": "22"},
	}}
	runTdarr(t, cfg, true)

	writes := app.varWrites()
	require.Len(t, writes, 1, "only the changed variable should be written")
	assert.Equal(t, "update", writes[0].mode, "expected update of v2")
	assert.Equal(t, "v2", writes[0].docID, "expected update of v2")
	assert.Equal(t, "22", writes[0].obj["value"], "new value not written: %v", writes[0].obj)
}

// A variable dropped from the spec must disappear from Tdarr, otherwise a flow
// keeps reading a value that no longer exists in git.
func TestLibraryVariablesPruneUndeclared(t *testing.T) {
	app := &fakeTdarr{docFound: true, vars: []map[string]any{
		{"_id": "v1", "key": "bitrate_1080p", "value": "2500", "type": "library:movies"},
		{"_id": "v9", "key": "stale_key", "value": "x", "type": "library:movies"},
		{"_id": "v7", "key": "other_lib", "value": "x", "type": "library:tv"},
	}}
	url := app.serve(t)

	cfg := tdarrConfig(url)
	cfg.Spec.Libraries = []transcodev1alpha1.TdarrLibrary{{
		ID: "movies", Name: "Movies", Folder: "/data/media/movies",
		Variables: map[string]string{"bitrate_1080p": "2500"},
	}}
	runTdarr(t, cfg, true)

	writes := app.varWrites()
	require.Len(t, writes, 1, "expected a single removal")
	assert.Equal(t, "removeOne", writes[0].mode, "expected stale_key (v9) removed")
	assert.Equal(t, "v9", writes[0].docID, "expected stale_key (v9) removed")
}

func TestLibraryDocDecisionMode(t *testing.T) {
	got := buildTdarrLibraryDoc(transcodev1alpha1.TdarrLibrary{
		ID: "movies", Name: "Movies", DecisionMode: "flows",
	})
	dm, ok := got["decisionMaker"].(map[string]any)
	require.True(t, ok, "decisionMaker missing: %v", got)
	assert.Equal(t, true, dm["settingsFlows"], "flows mode not selected")
	assert.Equal(t, false, dm["settingsPlugin"], "flows mode not selected")

	got = buildTdarrLibraryDoc(transcodev1alpha1.TdarrLibrary{ID: "movies", Name: "Movies"})
	assert.NotContains(t, got, "decisionMaker", "decisionMaker must be omitted when unset")
}

func TestLibraryDocHealthCheckMode(t *testing.T) {
	got := buildTdarrLibraryDoc(transcodev1alpha1.TdarrLibrary{
		ID: "movies", Name: "Movies", HealthCheckMode: "thorough",
	})
	assert.Equal(t, true, got["ffmpegscan"], "thorough must select ffmpegscan")
	assert.Equal(t, false, got["handbrakescan"], "thorough must select ffmpegscan")

	got = buildTdarrLibraryDoc(transcodev1alpha1.TdarrLibrary{
		ID: "movies", Name: "Movies", HealthCheckMode: "quick",
	})
	assert.Equal(t, true, got["handbrakescan"], "quick must select handbrakescan")
	assert.Equal(t, false, got["ffmpegscan"], "quick must select handbrakescan")
}

func observedTdarr(url string, lib transcodev1alpha1.TdarrLibrary) *transcodev1alpha1.TdarrConfig {
	cfg := tdarrConfig(url)
	cfg.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{DriftPolicy: new("observe")}
	cfg.Spec.Libraries = []transcodev1alpha1.TdarrLibrary{lib}
	return cfg
}

func tdarrSynced(t *testing.T, c client.Client) metav1.Condition {
	t.Helper()
	var cfg transcodev1alpha1.TdarrConfig
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "tdarr", Namespace: "media"}, &cfg))
	i := slices.IndexFunc(cfg.Status.Conditions, func(c metav1.Condition) bool { return c.Type == "Synced" })
	require.GreaterOrEqual(t, i, 0, "no Synced condition")
	return cfg.Status.Conditions[i]
}

// A missing library and its variable are drift; observe mode reads them
// through cruddb but never inserts.
func TestTdarrObserveReportsMissingLibraryWithoutWriting(t *testing.T) {
	app := &fakeTdarr{docFound: false}
	lib := transcodev1alpha1.TdarrLibrary{
		ID: "lib1", Name: "Movies", Folder: "/media/movies", Variables: map[string]string{"codec": "hevc"},
	}
	c := runTdarr(t, observedTdarr(app.serve(t), lib), true)

	require.True(t, app.ranOp("getById"), "observe never read the library")
	for _, op := range []string{"insert", "update", "removeOne"} {
		assert.False(t, app.ranOp(op), "observe mode ran %s", op)
	}
	assert.Equal(t, "DriftDetected", tdarrSynced(t, c).Reason)
}

func TestTdarrObserveMatchesAStoredLibrary(t *testing.T) {
	lib := transcodev1alpha1.TdarrLibrary{ID: "lib1", Name: "Movies", Folder: "/media/movies"}
	app := &fakeTdarr{docFound: true, stored: mustJSONObject(t, buildTdarrLibraryDoc(lib))}
	c := runTdarr(t, observedTdarr(app.serve(t), lib), true)

	assert.False(t, app.ranOp("update"))
	synced := tdarrSynced(t, c)
	assert.Equal(t, metav1.ConditionTrue, synced.Status, synced.Message)
	assert.Equal(t, "Observed", synced.Reason)
}

func mustJSONObject(t *testing.T, v any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(v)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

// specFieldToDocKeys maps each TdarrLibrary field to the key(s) it must produce in
// Tdarr's library document. Tdarr accepts unknown keys and ignores missing ones
// while still answering 200, so an unmapped field is silently inert.
var specFieldToDocKeys = map[string][]string{
	"ID":                   {"_id"},
	"Name":                 {"name"},
	"Folder":               {"folder"},
	"Cache":                {"cache"},
	"Container":            {"container"},
	"ContainerFilter":      {"containerFilter"},
	"FoldersToIgnore":      {"foldersToIgnore"},
	"FolderWatching":       {"folderWatching"},
	"ProcessLibrary":       {"processLibrary"},
	"ProcessTranscodes":    {"processTranscodes"},
	"ProcessHealthChecks":  {"processHealthChecks"},
	"HealthCheckMode":      {"handbrakescan", "ffmpegscan"},
	"DecisionMode":         {"decisionMaker"},
	"ScanOnStart":          {"scanOnStart"},
	"ScheduledScanFindNew": {"scheduledScanFindNew"},
	"ScannerThreadCount":   {"scannerThreadCount"},
	"FFmpeg":               {"ffmpeg"},
	"Priority":             {"priority"},
	"FlowID":               {"flowId"},
	"PluginStack":          {"pluginStack"},
	"Variables":            {"variables"},
}

func fullyPopulatedLibrary() transcodev1alpha1.TdarrLibrary {
	return transcodev1alpha1.TdarrLibrary{
		ID: "movies", Name: "Movies", Folder: "/data/media/movies",
		Cache: "/temp", Container: "mkv", ContainerFilter: "mkv,mp4",
		FoldersToIgnore: "extras", FolderWatching: new(false),
		ProcessLibrary: new(true), ProcessTranscodes: new(true), ProcessHealthChecks: new(true),
		HealthCheckMode: "thorough", DecisionMode: "flows",
		ScanOnStart: new(true), ScheduledScanFindNew: new(true), ScannerThreadCount: new(1),
		FFmpeg: new(true), Priority: new(1), FlowID: "hevc-qsv",
		PluginStack: []string{"Tdarr_Plugin_x"},
		Variables:   map[string]string{"k": "v"},
	}
}

func TestEveryLibraryFieldReachesTdarr(t *testing.T) {
	lib := fullyPopulatedLibrary()
	doc := buildTdarrLibraryDoc(lib)

	declared := reflect.TypeOf(lib)
	for field := range declared.Fields() {
		name := field.Name
		keys, mapped := specFieldToDocKeys[name]
		if !assert.True(t, mapped, "spec field %s has no documented Tdarr key; Tdarr will accept the payload and ignore it", name) {
			continue
		}
		for _, k := range keys {
			assert.Contains(t, doc, k, "spec field %s should emit key %q, but the document has no such key", name, k)
		}
	}
}

// A field left unset must not emit its key, so an unset field never overwrites a
// value the CR does not manage.
func TestUnsetLibraryFieldsAreOmitted(t *testing.T) {
	doc := buildTdarrLibraryDoc(transcodev1alpha1.TdarrLibrary{ID: "movies", Name: "Movies"})

	alwaysEmitted := map[string]bool{"_id": true, "name": true, "schedule": true, "foldersToIgnore": true}
	for field, keys := range specFieldToDocKeys {
		for _, k := range keys {
			if alwaysEmitted[k] {
				continue
			}
			assert.NotContains(t, doc, k, "field %s emitted key %q despite being unset", field, k)
		}
	}
}

// Worker limits are per node, so with several nodes the spec must name one:
// choosing among them would step a different node each pass.
func TestWorkerNodeNeverGuessesAmongSeveral(t *testing.T) {
	node := func(name string) map[string]any { return map[string]any{"nodeName": name} }
	one := map[string]any{"id-a": node("InternalNode")}
	two := map[string]any{"id-a": node("InternalNode"), "id-b": node("GpuNode")}

	for _, tc := range []struct {
		name    string
		nodes   map[string]any
		want    string
		wantErr string
	}{
		{name: "", nodes: one, want: "id-a"},
		{name: "InternalNode", nodes: one, want: "id-a"},
		{name: "GpuNode", nodes: two, want: "id-b"},
		{name: "", nodes: two, wantErr: "found 2 nodes (GpuNode, InternalNode); set workers.nodeName"},
		{name: "Missing", nodes: two, wantErr: `no node named "Missing" (nodes: GpuNode, InternalNode)`},
		{name: "Twin", nodes: map[string]any{"id-a": node("Twin"), "id-b": node("Twin")}, wantErr: `node name "Twin" is ambiguous`},
		{name: "", nodes: map[string]any{}, wantErr: "no nodes registered"},
	} {
		t.Run(tc.name+"/"+tc.want+tc.wantErr, func(t *testing.T) {
			got, err := workerNode(tc.nodes, tc.name)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
