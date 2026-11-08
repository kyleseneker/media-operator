package curation

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	curationv1alpha1 "github.com/kyleseneker/media-operator/api/curation/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/controller/internal/contract"
	"github.com/kyleseneker/media-operator/internal/engine"
)

const conditionTrue = "True"

// fakeMaintainerr keeps the state the operator reads back, in the shapes
// Maintainerr 3 returns, so a second reconcile can be checked for writes.
type fakeMaintainerr struct {
	mu        sync.Mutex
	requests  []string
	settings  map[string]any
	jellyfin  map[string]any
	services  map[string]map[string]any
	servers   map[string][]map[string]any
	groups    []map[string]any
	libraries []map[string]any
	// notifications holds the notification configurations as Maintainerr lists them.
	notifications []map[string]any
	nextID        int
}

func newFakeMaintainerr() *fakeMaintainerr {
	return &fakeMaintainerr{
		settings: map[string]any{"media_server_type": nil, "plex_auth_token": nil},
		jellyfin: map[string]any{},
		services: map[string]map[string]any{"seerr": {}, "tautulli": {}},
		servers:  map[string][]map[string]any{"sonarr": {}, "radarr": {}},
		libraries: []map[string]any{
			{"id": "a1b2", "title": "Movies", "type": "movie"},
			{"id": "c3d4", "title": "TV Shows", "type": "show"},
		},
		nextID: 1,
	}
}

func (f *fakeMaintainerr) writes() []string {
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

func (f *fakeMaintainerr) resetRequests() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = nil
}

func (f *fakeMaintainerr) ok(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "OK", "code": 1, "message": "Success"})
}

func (f *fakeMaintainerr) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	var body map[string]any
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	w.Header().Set("Content-Type", "application/json")
	path := strings.TrimPrefix(r.URL.Path, "/api")
	route := r.Method + " " + path

	if strings.HasPrefix(path, "/settings") {
		f.handleSettings(w, r.Method, path, body)
		return
	}
	if strings.HasPrefix(path, "/notifications") {
		f.handleNotifications(w, r.Method, path, body)
		return
	}
	if id, ok := strings.CutPrefix(path, "/rules/"); ok && r.Method == http.MethodDelete {
		f.groups = slices.DeleteFunc(f.groups, func(g map[string]any) bool { return fmt.Sprint(g["id"]) == id })
		f.ok(w)
		return
	}
	switch route {
	case "GET /health/ready":
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	case "GET /rules/constants":
		_ = json.NewEncoder(w).Encode(fakeRuleConstants(len(f.servers["radarr"]) > 0))
	case "GET /media-server/libraries":
		_ = json.NewEncoder(w).Encode(f.libraries)
	case "GET /rules":
		groups := f.groups
		if groups == nil {
			groups = []map[string]any{}
		}
		_ = json.NewEncoder(w).Encode(groups)
	case "POST /rules":
		body["id"] = f.nextID
		f.nextID++
		f.groups = append(f.groups, storeRuleGroup(body, nil))
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 1, "result": "Success"})
	case "PUT /rules":
		for i, g := range f.groups {
			if fmt.Sprint(g["id"]) == fmt.Sprint(body["id"]) {
				f.groups[i] = storeRuleGroup(body, g)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 1, "result": "Success"})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeMaintainerr) handleNotifications(w http.ResponseWriter, method, path string, body map[string]any) {
	switch {
	case method == http.MethodGet && path == "/notifications/configurations":
		list := f.notifications
		if list == nil {
			list = []map[string]any{}
		}
		_ = json.NewEncoder(w).Encode(list)
	case method == http.MethodPost && path == "/notifications/configuration/add":
		if id, ok := body["id"]; ok {
			for i, n := range f.notifications {
				if fmt.Sprint(n["id"]) == fmt.Sprint(id) {
					f.notifications[i] = body
				}
			}
		} else {
			body["id"] = f.nextID
			f.nextID++
			f.notifications = append(f.notifications, body)
		}
		f.ok(w)
	case method == http.MethodDelete && strings.HasPrefix(path, "/notifications/configuration/"):
		id := strings.TrimPrefix(path, "/notifications/configuration/")
		f.notifications = slices.DeleteFunc(f.notifications, func(n map[string]any) bool { return fmt.Sprint(n["id"]) == id })
		f.ok(w)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeMaintainerr) handleSettings(w http.ResponseWriter, method, path string, body map[string]any) {
	route := method + " " + path
	switch {
	case route == "GET /settings":
		_ = json.NewEncoder(w).Encode(f.settings)
	case route == "POST /settings":
		maps.Copy(f.settings, body)
		f.ok(w)
	case route == "POST /settings/media-server/switch":
		f.settings["media_server_type"] = body["targetServerType"]
		f.ok(w)
	case route == "POST /settings/plex/token":
		token, _ := body["plex_auth_token"].(string)
		f.settings["plex_auth_token"] = maskPlexToken(token)
		f.ok(w)
	case route == "GET /settings/jellyfin":
		_ = json.NewEncoder(w).Encode(f.jellyfin)
	case route == "POST /settings/jellyfin":
		f.jellyfin = body
		if _, ok := body["jellyfin_user_id"]; !ok {
			f.jellyfin["jellyfin_user_id"] = "first-admin"
		}
		f.settings["media_server_type"] = "jellyfin"
		f.ok(w)
	case method == http.MethodGet && (path == "/settings/seerr" || path == "/settings/tautulli"):
		_ = json.NewEncoder(w).Encode(f.services[strings.TrimPrefix(path, "/settings/")])
	case method == http.MethodPost && (path == "/settings/seerr" || path == "/settings/tautulli"):
		f.services[strings.TrimPrefix(path, "/settings/")] = body
		f.ok(w)
	case method == http.MethodGet && (path == "/settings/sonarr" || path == "/settings/radarr"):
		_ = json.NewEncoder(w).Encode(f.servers[strings.TrimPrefix(path, "/settings/")])
	case method == http.MethodPost && (path == "/settings/sonarr" || path == "/settings/radarr"):
		kind := strings.TrimPrefix(path, "/settings/")
		body["id"] = f.nextID
		f.nextID++
		f.servers[kind] = append(f.servers[kind], body)
		f.ok(w)
	case method == http.MethodDelete && (strings.HasPrefix(path, "/settings/sonarr/") || strings.HasPrefix(path, "/settings/radarr/")):
		kind, id, _ := strings.Cut(strings.TrimPrefix(path, "/settings/"), "/")
		for _, g := range f.groups {
			if collection, _ := g["collection"].(map[string]any); fmt.Sprint(collection[kind+"SettingsId"]) == id {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"status": "NOK", "code": 0, "message": "Cannot delete setting with associated collections",
				})
				return
			}
		}
		f.servers[kind] = slices.DeleteFunc(f.servers[kind], func(s map[string]any) bool { return fmt.Sprint(s["id"]) == id })
		f.ok(w)
	case method == http.MethodPut && strings.HasPrefix(path, "/settings/radarr/"):
		id, _ := strconv.Atoi(strings.TrimPrefix(path, "/settings/radarr/"))
		for _, s := range f.servers["radarr"] {
			if fmt.Sprint(s["id"]) == strconv.Itoa(id) {
				maps.Copy(s, body)
			}
		}
		f.ok(w)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// storeRuleGroup saves a rule group the way Maintainerr reports it: group
// settings under its collection, each rule as the JSON it was sent.
func storeRuleGroup(body, previous map[string]any) map[string]any {
	collection := map[string]any{}
	if previous != nil {
		maps.Copy(collection, previous["collection"].(map[string]any))
	}
	if c, ok := body["collection"].(map[string]any); ok {
		maps.Copy(collection, c)
	}
	for _, k := range maintainerrGroupFieldsOnCollection {
		collection[k] = body[k]
	}
	raw, _ := body["rules"].([]any)
	rules := make([]any, 0, len(raw))
	for i, rule := range raw {
		text, _ := json.Marshal(rule)
		rules = append(rules, map[string]any{"id": 100 + i, "ruleJson": string(text), "section": rule.(map[string]any)["section"]})
	}
	// A save replaces the group's notifications with the ones it names.
	notifications, _ := body["notifications"].([]any)
	return map[string]any{
		"id": body["id"], "name": body["name"], "description": body["description"], "libraryId": body["libraryId"],
		"dataType": body["dataType"], "isActive": body["isActive"], "useRules": body["useRules"],
		"ruleHandlerCronSchedule": body["ruleHandlerCronSchedule"], "collectionId": 9,
		"collection": collection, "rules": rules, "notifications": notifications,
	}
}

func fakeRuleConstants(radarr bool) map[string]any {
	apps := []any{
		map[string]any{"id": 0, "name": "Plex", "props": []any{
			map[string]any{"id": 0, "name": "addDate", "humanName": "Date added"},
			map[string]any{"id": 5, "name": "viewCount", "humanName": "Times viewed"},
		}},
		map[string]any{"id": 6, "name": "Jellyfin", "props": []any{
			map[string]any{"id": 0, "name": "addDate", "humanName": "Date added"},
			map[string]any{"id": 5, "name": "viewCount", "humanName": "Times viewed"},
		}},
	}
	if radarr {
		apps = append(apps, map[string]any{"id": 1, "name": "Radarr", "props": []any{
			map[string]any{"id": 4, "name": "monitored", "humanName": "Is monitored (deprecated)"},
			map[string]any{"id": 9, "name": "monitored", "humanName": "Is monitored"},
		}})
	}
	return map[string]any{"applications": apps}
}

func (f *fakeMaintainerr) serve(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(routableAddr(t), "0"))
	if err != nil {
		t.Skipf("cannot bind a routable listener: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(f.handle))
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL
}

func maintainerrSecrets() *corev1.Secret {
	return &corev1.Secret{
		Name: "media-credentials", Namespace: "media",
		Data: map[string][]byte{
			"jellyfin-api-key": []byte("jellyfin-key"),
			"plex-token":       []byte("plex-token-abcdef"),
			"radarr-api-key":   []byte("radarr-key"),
			"seerr-api-key":    []byte("seerr-key"),
			"discord-webhook":  []byte("https://discord.example/hook"),
		},
	}
}

func secretRef(key string) commonv1alpha1.SecretKeyRef {
	return commonv1alpha1.SecretKeyRef{Name: "media-credentials", Key: key}
}

func jellyfinSpec(url string) curationv1alpha1.MaintainerrConfigSpec {
	return curationv1alpha1.MaintainerrConfigSpec{
		Connection: curationv1alpha1.MaintainerrConnection{URL: url},
		MediaServer: &curationv1alpha1.MaintainerrMediaServer{Jellyfin: &curationv1alpha1.MaintainerrJellyfin{
			URL: "http://jellyfin:8096/", APIKeySecretRef: secretRef("jellyfin-api-key"),
		}},
		Radarr: []curationv1alpha1.MaintainerrArrServer{{
			Name: "Radarr", URL: "http://Radarr:7878", APIKeySecretRef: secretRef("radarr-api-key"),
		}},
		Seerr: &curationv1alpha1.MaintainerrServiceConnection{URL: "http://seerr:5055", APIKeySecretRef: secretRef("seerr-api-key")},
		Jobs:  &curationv1alpha1.MaintainerrJobs{Rules: "0 0-23/8 * * *"},
		RuleGroups: []curationv1alpha1.MaintainerrRuleGroup{{
			Name: "Stale movies", Library: "Movies", ArrAction: "delete", RadarrServer: "Radarr",
			ActionAfterDays: new(int32(30)),
			Sections: []curationv1alpha1.MaintainerrRuleSection{{Rules: []curationv1alpha1.MaintainerrRule{
				{FirstValue: "Jellyfin.addDate", Action: "BEFORE",
					CustomValue: &curationv1alpha1.MaintainerrCustomValue{Type: "custom_days", Value: intstr.FromInt32(180)}},
				{Operator: "AND", FirstValue: "Jellyfin.viewCount", Action: "EQUALS",
					CustomValue: &curationv1alpha1.MaintainerrCustomValue{Type: "number", Value: intstr.FromInt32(0)}},
			}}},
		}},
	}
}

type maintainerrRun struct {
	c client.Client
	r *MaintainerrConfigReconciler
}

func newMaintainerrRun(t *testing.T, spec curationv1alpha1.MaintainerrConfigSpec) *maintainerrRun {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme), "adding client-go scheme")
	require.NoError(t, curationv1alpha1.AddToScheme(scheme), "adding curation scheme")
	cfg := &curationv1alpha1.MaintainerrConfig{Name: "maintainerr", Namespace: "media", Spec: spec}
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&curationv1alpha1.MaintainerrConfig{}).
		WithObjects(cfg, maintainerrSecrets()).Build()
	return &maintainerrRun{c: c, r: &MaintainerrConfigReconciler{Client: c, Scheme: scheme, Recorder: events.NewFakeRecorder(50)}}
}

func (m *maintainerrRun) reconcile(t *testing.T) {
	t.Helper()
	_, err := m.r.Reconcile(context.Background(), ctrl.Request{Name: "maintainerr", Namespace: "media"})
	require.NoError(t, err, "reconcile")
}

func (m *maintainerrRun) get(t *testing.T) *curationv1alpha1.MaintainerrConfig {
	t.Helper()
	var cfg curationv1alpha1.MaintainerrConfig
	require.NoError(t, m.c.Get(context.Background(), types.NamespacedName{Name: "maintainerr", Namespace: "media"}, &cfg))
	return &cfg
}

func (m *maintainerrRun) condition(t *testing.T, condType string) metav1.Condition {
	t.Helper()
	var cfg curationv1alpha1.MaintainerrConfig
	require.NoError(t, m.c.Get(context.Background(), types.NamespacedName{Name: "maintainerr", Namespace: "media"}, &cfg))
	for _, cond := range cfg.Status.Conditions {
		if cond.Type == condType {
			return cond
		}
	}
	return metav1.Condition{}
}

func TestMaintainerrConfiguresFreshInstallThenWritesNothing(t *testing.T) {
	app := newFakeMaintainerr()
	run := newMaintainerrRun(t, jellyfinSpec(app.serve(t)))

	run.reconcile(t)
	synced := run.condition(t, "Synced")
	require.Equal(t, conditionTrue, string(synced.Status), synced.Message)
	assert.Equal(t, "jellyfin", app.settings["media_server_type"])
	assert.Equal(t, "http://jellyfin:8096", app.jellyfin["jellyfin_url"], "Maintainerr stores URLs without a trailing slash")
	require.Len(t, app.servers["radarr"], 1)
	assert.Equal(t, "http://radarr:7878", app.servers["radarr"][0]["url"], "Maintainerr lowercases server URLs")
	assert.Equal(t, "0 0-23/8 * * *", app.settings["rules_handler_job_cron"])
	require.Len(t, app.groups, 1)

	group := app.groups[0]
	assert.Equal(t, "a1b2", group["libraryId"])
	assert.Equal(t, "movie", group["dataType"])
	collection := group["collection"].(map[string]any)
	assert.EqualValues(t, 30, collection["deleteAfterDays"])
	assert.EqualValues(t, 1, collection["radarrSettingsId"])
	rules := group["rules"].([]any)
	require.Len(t, rules, 2)
	var first, second map[string]any
	require.NoError(t, json.Unmarshal([]byte(rules[0].(map[string]any)["ruleJson"].(string)), &first))
	require.NoError(t, json.Unmarshal([]byte(rules[1].(map[string]any)["ruleJson"].(string)), &second))
	assert.Nil(t, first["operator"])
	assert.Equal(t, []any{6.0, 0.0}, first["firstVal"], "Jellyfin.addDate resolves to [6, 0]")
	assert.Equal(t, map[string]any{"ruleTypeId": 0.0, "value": "15552000"}, first["customVal"], "180 days in seconds")
	assert.EqualValues(t, 0, second["operator"], "AND")

	app.resetRequests()
	run.reconcile(t)
	assert.Empty(t, app.writes(), "a second reconcile with nothing changed must not write")
}

// Maintainerr deletes every collection when the media server changes; the
// operator must report that rather than do it.
func TestMaintainerrNeverSwitchesAnExistingMediaServer(t *testing.T) {
	app := newFakeMaintainerr()
	app.settings["media_server_type"] = "plex"
	spec := jellyfinSpec(app.serve(t))
	spec.RuleGroups = nil
	run := newMaintainerrRun(t, spec)

	run.reconcile(t)
	synced := run.condition(t, "Synced")
	assert.Equal(t, metav1.ConditionFalse, synced.Status)
	assert.Contains(t, synced.Message, "deletes every collection")
	assert.NotContains(t, app.writes(), "POST /api/settings/jellyfin")
	assert.NotContains(t, app.writes(), "POST /api/settings/media-server/switch")
	assert.Equal(t, "plex", app.settings["media_server_type"])
}

// Plex refuses server settings until a token is stored, and the token reads
// back only masked.
func TestMaintainerrSetsUpPlexInOrderAndComparesTheMaskedToken(t *testing.T) {
	app := newFakeMaintainerr()
	spec := curationv1alpha1.MaintainerrConfigSpec{
		Connection: curationv1alpha1.MaintainerrConnection{URL: app.serve(t)},
		MediaServer: &curationv1alpha1.MaintainerrMediaServer{Plex: &curationv1alpha1.MaintainerrPlex{
			Hostname: "https://Plex.media.svc", Port: 32400, TokenSecretRef: secretRef("plex-token"),
		}},
	}
	run := newMaintainerrRun(t, spec)

	run.reconcile(t)
	require.Equal(t, conditionTrue, string(run.condition(t, "Synced").Status), run.condition(t, "Synced").Message)
	assert.Equal(t, []string{
		"POST /api/settings/media-server/switch", "POST /api/settings/plex/token", "POST /api/settings",
	}, app.writes())
	assert.Equal(t, "plex.media.svc", app.settings["plex_hostname"])
	assert.EqualValues(t, 1, app.settings["plex_manual_mode"], "manual mode keeps Maintainerr from replacing the address")

	app.resetRequests()
	run.reconcile(t)
	assert.Empty(t, app.writes(), "an unchanged token compares equal in its masked form")
}

// An update replaces the whole group; settings the CR does not expose must be
// carried over, and notifications are dropped if they are left out.
func TestMaintainerrRuleGroupUpdateKeepsUnmanagedSettings(t *testing.T) {
	app := newFakeMaintainerr()
	spec := jellyfinSpec(app.serve(t))
	run := newMaintainerrRun(t, spec)
	run.reconcile(t)
	require.Len(t, app.groups, 1)
	app.groups[0]["notifications"] = []any{map[string]any{"id": 7, "name": "Discord"}}
	app.groups[0]["collection"].(map[string]any)["sortTitle"] = "zz"

	var cfg curationv1alpha1.MaintainerrConfig
	key := types.NamespacedName{Name: "maintainerr", Namespace: "media"}
	require.NoError(t, run.c.Get(context.Background(), key, &cfg))
	cfg.Spec.RuleGroups[0].ActionAfterDays = new(int32(60))
	require.NoError(t, run.c.Update(context.Background(), &cfg))

	app.resetRequests()
	run.reconcile(t)
	assert.Equal(t, []string{"PUT /api/rules"}, app.writes())
	collection := app.groups[0]["collection"].(map[string]any)
	assert.EqualValues(t, 60, collection["deleteAfterDays"])
	assert.Equal(t, "zz", collection["sortTitle"])
	assert.Equal(t, []any{map[string]any{"id": 7.0}}, app.groups[0]["notifications"])
}

// A rule group that names a server Maintainerr does not have fails alone; the
// rest of the config still applies.
func TestMaintainerrReportsAnUnknownServerWithoutBlockingTheRest(t *testing.T) {
	app := newFakeMaintainerr()
	spec := jellyfinSpec(app.serve(t))
	spec.RuleGroups[0].RadarrServer = "Missing"
	run := newMaintainerrRun(t, spec)

	run.reconcile(t)
	synced := run.condition(t, "Synced")
	assert.Equal(t, metav1.ConditionFalse, synced.Status)
	assert.Contains(t, synced.Message, `no radarr server named "Missing"`)
	assert.Len(t, app.servers["radarr"], 1, "the server section still applied")
	assert.Empty(t, app.groups)
}

func TestMaintainerrMarksUnhealthyAppNotReady(t *testing.T) {
	ln, err := net.Listen("tcp", net.JoinHostPort(routableAddr(t), "0"))
	if err != nil {
		t.Skipf("cannot bind: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)

	run := newMaintainerrRun(t, jellyfinSpec(srv.URL))
	run.reconcile(t)
	assert.NotEqual(t, conditionTrue, string(run.condition(t, "Ready").Status), "Ready = True for an app failing its health check")
}

func observeSpec(spec curationv1alpha1.MaintainerrConfigSpec) curationv1alpha1.MaintainerrConfigSpec {
	spec.Reconcile = &commonv1alpha1.ReconcileConfig{DriftPolicy: new("observe")}
	return spec
}

// A fresh Maintainerr has none of the config, and the rule groups cannot be
// resolved before the media server and Radarr exist: all of it is drift, and
// none of it is written.
func TestMaintainerrObserveReportsAFreshInstallWithoutWriting(t *testing.T) {
	app := newFakeMaintainerr()
	run := newMaintainerrRun(t, observeSpec(jellyfinSpec(app.serve(t))))

	run.reconcile(t)
	assert.Empty(t, app.writes(), "observe mode wrote to Maintainerr")
	synced := run.condition(t, "Synced")
	assert.Equal(t, metav1.ConditionFalse, synced.Status)
	assert.Equal(t, "DriftDetected", synced.Reason, synced.Message)
}

func TestMaintainerrObserveMatchesAConfiguredInstall(t *testing.T) {
	app := newFakeMaintainerr()
	url := app.serve(t)
	newMaintainerrRun(t, jellyfinSpec(url)).reconcile(t)
	app.resetRequests()

	run := newMaintainerrRun(t, observeSpec(jellyfinSpec(url)))
	run.reconcile(t)
	assert.Empty(t, app.writes())
	synced := run.condition(t, "Synced")
	assert.Equal(t, metav1.ConditionTrue, synced.Status, synced.Message)
	assert.Equal(t, "Observed", synced.Reason)
}

// Once the prerequisites match, a rule group that cannot be resolved is a
// configuration error again, not drift.
func TestMaintainerrObserveReportsUnresolvableGroupsOnceTheRestMatches(t *testing.T) {
	app := newFakeMaintainerr()
	url := app.serve(t)
	newMaintainerrRun(t, jellyfinSpec(url)).reconcile(t)

	spec := observeSpec(jellyfinSpec(url))
	spec.RuleGroups[0].Sections[0].Rules[0].FirstValue = "Jellyfin.nope"
	run := newMaintainerrRun(t, spec)
	run.reconcile(t)
	synced := run.condition(t, "Synced")
	assert.Equal(t, engine.ReasonSyncFailed, synced.Reason)
	assert.Contains(t, synced.Message, `no Jellyfin property "nope"`)
}

// Maintainerr strips settings keys it does not know without an error, so a spec
// field the builder never maps is silently inert.
func TestMaintainerrJobsFieldsAllReachThePayload(t *testing.T) {
	contract.AssertEveryFieldEmitted(t, &curationv1alpha1.MaintainerrJobs{}, buildMaintainerrJobs)
}

// An empty spec must produce no payload, so the reconcile skips the write entirely
// rather than clearing schedules the CR does not manage.
func TestMaintainerrEmptyJobsProduceNoPayload(t *testing.T) {
	assert.Empty(t, buildMaintainerrJobs(&curationv1alpha1.MaintainerrJobs{}), "empty jobs produced a payload")
}
