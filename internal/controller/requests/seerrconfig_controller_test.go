package requests

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	requestsv1alpha1 "github.com/kyleseneker/media-operator/api/requests/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBuildServicePayloadAlwaysSendsRequiredFields(t *testing.T) {
	required := []string{
		"name", "hostname", "port", "apiKey", "useSsl",
		"activeProfileId", "activeProfileName", "activeDirectory",
		"is4k", "isDefault",
	}

	svc := &requestsv1alpha1.SeerrServiceConnection{Name: "Sonarr", Hostname: "sonarr.svc"}
	payload := buildServicePayload(svc, "key")

	for _, key := range required {
		assert.Contains(t, payload, key, "%q missing; Seerr's OpenAPI marks it required and rejects the request with 400", key)
	}
}

func observeSeerr(t *testing.T, cfg *requestsv1alpha1.SeerrConfig) metav1.Condition {
	t.Helper()
	cfg.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{DriftPolicy: new("observe")}
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, requestsv1alpha1.AddToScheme(scheme))
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&requestsv1alpha1.SeerrConfig{}).WithObjects(cfg, seerrSecret()).Build()
	r := &SeerrConfigReconciler{Client: c, Scheme: scheme, Recorder: events.NewFakeRecorder(50)}
	key := types.NamespacedName{Namespace: cfg.Namespace, Name: cfg.Name}
	_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key})
	require.NoError(t, err)
	require.NoError(t, c.Get(context.Background(), key, cfg))
	i := slices.IndexFunc(cfg.Status.Conditions, func(c metav1.Condition) bool { return c.Type == "Synced" })
	require.GreaterOrEqual(t, i, 0, "no Synced condition")
	return cfg.Status.Conditions[i]
}

func (f *fakeSeerr) writes() []string {
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

// Signing in to a Seerr that has not been set up configures its media server,
// so observe mode must not even log in.
func TestSeerrObserveReportsMissingSetupWithoutLoggingIn(t *testing.T) {
	app := &fakeSeerr{}
	cfg := seerrConfig(app.serve(t))
	cfg.Spec.JellyfinAuth = &requestsv1alpha1.SeerrJellyfinAuth{
		Hostname:          "jellyfin",
		UsernameSecretRef: commonv1alpha1.SecretKeyRef{Name: "seerr-secrets", Key: "username"},
		PasswordSecretRef: commonv1alpha1.SecretKeyRef{Name: "seerr-secrets", Key: "password"},
	}
	synced := observeSeerr(t, cfg)
	assert.Empty(t, app.writes())
	assert.Equal(t, "DriftDetected", synced.Reason)
}

func TestSeerrObserveReportsDifferingSettingsWithoutWriting(t *testing.T) {
	app := &fakeSeerr{initialized: true}
	cfg := seerrConfig(app.serve(t))
	cfg.Spec.Connection.APIKeySecretRef = &commonv1alpha1.SecretKeyRef{Name: "seerr-secrets", Key: "api-key"}
	cfg.Spec.Main = &requestsv1alpha1.SeerrMain{ApplicationTitle: "Requests"}
	synced := observeSeerr(t, cfg)
	require.True(t, app.saw("GET", "/api/v1/settings/main"), "observe never read the settings")
	assert.Empty(t, app.writes())
	assert.Equal(t, "DriftDetected", synced.Reason)
}

func TestSeerrObserveMatchesWithNothingToCompare(t *testing.T) {
	app := &fakeSeerr{initialized: true}
	cfg := seerrConfig(app.serve(t))
	cfg.Spec.Connection.APIKeySecretRef = &commonv1alpha1.SecretKeyRef{Name: "seerr-secrets", Key: "api-key"}
	synced := observeSeerr(t, cfg)
	assert.Empty(t, app.writes())
	assert.Equal(t, metav1.ConditionTrue, synced.Status, synced.Message)
}

// Seerr returns its whole settings document; only declared values decide
// whether a save is needed.
func TestSeerrChangedComparesOnlyDeclaredSettings(t *testing.T) {
	current := map[string]any{"applicationTitle": "Seerr", "apiKey": "x", "defaultPermissions": float64(32)}
	assert.False(t, seerrDiffers(current, map[string]any{"applicationTitle": "Seerr", "defaultPermissions": 32}))
	assert.True(t, seerrDiffers(current, map[string]any{"applicationTitle": "Requests"}))
	assert.True(t, seerrDiffers(nil, map[string]any{"enabled": true}), "nothing stored yet")
}
