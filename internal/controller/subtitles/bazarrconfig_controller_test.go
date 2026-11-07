package subtitles

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	subtitlesv1alpha1 "github.com/kyleseneker/media-operator/api/subtitles/v1alpha1"
)

const conditionTrue = "True"

// fakeBazarr captures posted form bodies. Bazarr takes form-encoded settings
// rather than JSON, so the assertions are about which form keys were sent.
type fakeBazarr struct {
	mu    sync.Mutex
	forms []url.Values
	paths []string
}

// formValue returns the first value posted for key across all requests.
func (f *fakeBazarr) formValue(key string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, form := range f.forms {
		if v := form.Get(key); v != "" {
			return v, true
		}
	}
	return "", false
}

// anyKeyContains reports whether any posted form key contains the substring.
func (f *fakeBazarr) anyKeyContains(sub string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, form := range f.forms {
		for k := range form {
			if strings.Contains(k, sub) {
				return true
			}
		}
	}
	return false
}

func (f *fakeBazarr) sawPath(p string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.paths, p)
}

// routableAddr returns a non-loopback IPv4 address. The operator's HTTP client
// blocks loopback and link-local as SSRF protection, so a fake on 127.0.0.1 is
// unreachable by design and assertions against it pass for the wrong reason.
func routableAddr(t *testing.T) string {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Skipf("cannot enumerate interfaces: %v", err)
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipnet.IP.To4()
		if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		return ip.String()
	}
	t.Skip("no routable non-loopback IPv4 address on this host")
	return ""
}

func (f *fakeBazarr) serve(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort(routableAddr(t), "0"))
	if err != nil {
		t.Skipf("cannot bind a routable listener: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		if len(r.PostForm) > 0 {
			f.forms = append(f.forms, r.PostForm)
		}
		f.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))
	_ = srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL
}

func bazarrSecrets() *corev1.Secret {
	return &corev1.Secret{
		Name: "bazarr-secrets", Namespace: "media",
		Data: map[string][]byte{
			"bazarr-api-key": []byte("bazarr-key"),
			"sonarr-api-key": []byte("sonarr-key"),
		},
	}
}

func bazarrConfig(addr string) *subtitlesv1alpha1.BazarrConfig {
	return &subtitlesv1alpha1.BazarrConfig{
		Name: "bazarr", Namespace: "media",
		Spec: subtitlesv1alpha1.BazarrConfigSpec{
			Connection: commonv1alpha1.AppConnection{
				URL:             addr,
				APIKeySecretRef: commonv1alpha1.SecretKeyRef{Name: "bazarr-secrets", Key: "bazarr-api-key"},
			},
		},
	}
}

func runBazarr(t *testing.T, cfg *subtitlesv1alpha1.BazarrConfig, objs ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme), "adding client-go scheme")
	require.NoError(t, subtitlesv1alpha1.AddToScheme(scheme), "adding subtitles scheme")
	b := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&subtitlesv1alpha1.BazarrConfig{}).WithObjects(cfg)
	for _, o := range objs {
		b = b.WithObjects(o)
	}
	c := b.Build()
	r := &BazarrConfigReconciler{Client: c, Scheme: scheme, Recorder: events.NewFakeRecorder(50)}
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		Name: "bazarr", Namespace: "media",
	})
	require.NoError(t, err, "reconcile")
	return c
}

func bazarrCondition(t *testing.T, c client.Client, condType string) string {
	t.Helper()
	var cfg subtitlesv1alpha1.BazarrConfig
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "bazarr", Namespace: "media"}, &cfg), "fetching config")
	for _, cond := range cfg.Status.Conditions {
		if cond.Type == condType {
			return string(cond.Status)
		}
	}
	return ""
}

// The CR names a Secret for the Sonarr API key. Bazarr must receive the
// resolved value under its own "apikey" form field, and must never receive the
// "apiKeySecretRef" field name, which would send Bazarr a Kubernetes reference
// it cannot use in place of a credential.
func TestBazarrSubstitutesResolvedAPIKeyIntoForm(t *testing.T) {
	app := &fakeBazarr{}
	addr := app.serve(t)

	cfg := bazarrConfig(addr)
	port := 8989
	cfg.Spec.SonarrConnection = &subtitlesv1alpha1.BazarrAppConnection{
		Host:            "sonarr",
		Port:            &port,
		APIKeySecretRef: commonv1alpha1.SecretKeyRef{Name: "bazarr-secrets", Key: "sonarr-api-key"},
	}
	runBazarr(t, cfg, bazarrSecrets())

	got, ok := app.formValue("settings-sonarr-apikey")
	require.True(t, ok, "sonarr api key was never sent")
	assert.Equal(t, "sonarr-key", got, "settings-sonarr-apikey is not the resolved secret value")
	assert.False(t, app.anyKeyContains("apiKeySecretRef"), "sent the Kubernetes secret reference to Bazarr instead of the resolved value")
}

func TestBazarrSendsLanguagesAsJSON(t *testing.T) {
	app := &fakeBazarr{}
	addr := app.serve(t)

	cfg := bazarrConfig(addr)
	cfg.Spec.Languages = &subtitlesv1alpha1.BazarrLanguages{Enabled: []string{"en", "de"}}
	runBazarr(t, cfg, bazarrSecrets())

	got, ok := app.formValue("settings-general-enabled_languages")
	require.True(t, ok, "enabled languages were never sent")
	var langs []string
	require.NoError(t, json.Unmarshal([]byte(got), &langs), "enabled_languages is not JSON: %q", got)
	require.Len(t, langs, 2, "enabled languages")
	assert.Equal(t, "en", langs[0], "enabled languages")
}

func TestBazarrReportsSyncedOnHealthyApp(t *testing.T) {
	app := &fakeBazarr{}
	addr := app.serve(t)

	c := runBazarr(t, bazarrConfig(addr), bazarrSecrets())

	assert.True(t, app.sawPath("/api/system/health"), "never health-checked the app")
	assert.Equal(t, conditionTrue, bazarrCondition(t, c, "Synced"), "Synced")
}

func TestBazarrMissingSecretDoesNotContactApp(t *testing.T) {
	app := &fakeBazarr{}
	addr := app.serve(t)

	c := runBazarr(t, bazarrConfig(addr)) // no Secret

	assert.False(t, app.sawPath("/api/system/health"), "contacted the app despite failing to resolve its API key")
	assert.NotEqual(t, conditionTrue, bazarrCondition(t, c, "Synced"), "Synced = True despite a missing secret")
}

func TestBazarrObserveReportsDriftWithoutWriting(t *testing.T) {
	app := &fakeBazarr{}
	cfg := bazarrConfig(app.serve(t))
	cfg.Spec.General = &subtitlesv1alpha1.BazarrGeneral{UseSonarr: new(true)}
	policy := "observe"
	cfg.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{DriftPolicy: &policy}
	c := runBazarr(t, cfg, bazarrSecrets())

	assert.True(t, app.sawPath("/api/system/settings"), "observe must read the settings")
	assert.Empty(t, app.forms, "observe must not write")
	assert.Equal(t, string(metav1.ConditionFalse), bazarrCondition(t, c, "Synced"))
}

// Writing every section on every pass reconnected Bazarr to Sonarr and Radarr
// each time. Only settings that read back differently may be sent.
func TestBazarrChangesOnlyWhatDiffers(t *testing.T) {
	desired := url.Values{
		"settings-general-use_sonarr":        {"true"},
		"settings-general-minimum_score":     {"90"},
		"settings-general-enabled_providers": {"podnapisi", "tvsubtitles"},
		"settings-general-enabled_languages": {`["en","es"]`},
		"settings-sonarr-ip":                 {"10.0.0.5"},
		"languages-profiles": {`[{"profileId":1,"name":"English","cutoff":null,"originalFormat":false,` +
			`"items":[{"id":1,"language":"en","hi":"False","forced":"False","audio_exclude":"False"}]}]`},
	}
	settings := map[string]any{
		"general": map[string]any{
			"use_sonarr": true, "minimum_score": float64(90),
			"enabled_providers": []any{"tvsubtitles", "podnapisi"}, "enabled_languages": []any{"en", "es"},
		},
		"sonarr": map[string]any{"ip": "127.0.0.1"},
	}
	profiles := []map[string]any{{
		"profileId": float64(1), "name": "English", "cutoff": nil, "originalFormat": float64(0), "tag": nil,
		"items": []any{map[string]any{"id": float64(1), "language": "en", "hi": "False", "forced": "False", "audio_exclude": "False"}},
	}}

	changes := bazarrChanges(desired, settings, profiles)

	assert.Equal(t, url.Values{"settings-sonarr-ip": {"10.0.0.5"}}, changes,
		"typed values, provider order, originalFormat 0 and Bazarr's own tag key are not changes")
}
