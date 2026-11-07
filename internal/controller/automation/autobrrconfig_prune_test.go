package automation

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	automationv1alpha1 "github.com/kyleseneker/media-operator/api/automation/v1alpha1"
	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
)

func autobrrManaged(t *testing.T, cfg *automationv1alpha1.AutobrrConfig, withSecret bool) map[string][]string {
	t.Helper()
	c := runAutobrr(t, cfg, withSecret)
	var got automationv1alpha1.AutobrrConfig
	require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "autobrr", Namespace: "media"}, &got))
	return got.Status.ManagedResources
}

func TestAutobrrPrunesOnlyFeedsItCreated(t *testing.T) {
	app := &fakeAutobrr{existing: map[string][]map[string]any{"feeds": {
		{"id": float64(5), "name": "old"}, {"id": float64(6), "name": "hand-made"}, {"id": float64(7), "name": "kept"},
	}}}
	cfg := autobrrConfig(app.serve(t))
	cfg.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{Prune: new(true)}
	cfg.Spec.Feeds = []automationv1alpha1.AutobrrFeed{{Name: "kept", Type: "RSS", URL: "http://feed"}}
	cfg.Status.ManagedResources = map[string][]string{"feeds": {"old", "kept"}}

	managed := autobrrManaged(t, cfg, true)

	assert.True(t, app.saw(http.MethodDelete, "/api/feeds/5"), "an owned feed left the spec and was not deleted")
	assert.False(t, app.saw(http.MethodDelete, "/api/feeds/6"), "deleted a feed the operator did not create")
	assert.False(t, app.saw(http.MethodDelete, "/api/feeds/7"), "deleted a declared feed")
	assert.Equal(t, []string{"kept"}, managed["feeds"])
}

func TestAutobrrRecordsWhatItCreates(t *testing.T) {
	app := &fakeAutobrr{}
	cfg := autobrrConfig(app.serve(t))
	cfg.Spec.Feeds = []automationv1alpha1.AutobrrFeed{{Name: "feed", Type: "RSS", URL: "http://feed"}}
	cfg.Spec.Filters = []automationv1alpha1.AutobrrFilter{{
		Name:    "TV",
		Actions: []automationv1alpha1.AutobrrFilterAction{{Name: "notify", Type: "Webhook", WebhookURL: "http://hook"}},
	}}

	managed := autobrrManaged(t, cfg, true)

	assert.Equal(t, []string{"feed"}, managed["feeds"])
	assert.Equal(t, []string{"TV"}, managed["filters"])
	assert.Equal(t, []string{"TV/notify"}, managed["actions"])
}

func TestAutobrrDeletionPolicyRemovesWhatItCreated(t *testing.T) {
	app := &fakeAutobrr{existing: map[string][]map[string]any{
		"filters":     {{"id": float64(3), "name": "TV"}},
		"actions":     {{"id": float64(8), "name": "send"}},
		"downloaders": {{"id": float64(4), "name": "qBit"}, {"id": float64(9), "name": "hand-made"}},
	}}
	cfg := autobrrConfig(app.serve(t))
	policy := ctrlcommon.DeletionPolicyDelete
	cfg.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{DeletionPolicy: &policy}
	cfg.Status.ManagedResources = map[string][]string{"filters": {"TV"}, "actions": {"TV/send"}, "downloadClients": {"qBit"}}
	now := metav1.NewTime(time.Now())
	cfg.DeletionTimestamp = &now
	cfg.Finalizers = []string{ctrlcommon.Finalizer}

	runAutobrr(t, cfg, true)

	for _, path := range []string{"/api/actions/8", "/api/filters/3", "/api/downloaders/4"} {
		assert.True(t, app.saw(http.MethodDelete, path), "cleanup did not delete %s", path)
	}
	assert.False(t, app.saw(http.MethodDelete, "/api/downloaders/9"), "cleanup deleted a download client the operator did not create")
}
