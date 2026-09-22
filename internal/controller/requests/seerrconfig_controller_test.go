package requests

import (
	"context"
	"testing"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	requestsv1alpha1 "github.com/kyleseneker/media-operator/api/requests/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
		if _, ok := payload[key]; !ok {
			t.Errorf("%q missing; Seerr's OpenAPI marks it required and rejects the request with 400", key)
		}
	}
}

func TestSeerrObserveRejectedBeforeApplicationAccess(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := requestsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	policy := "observe"
	cfg := &requestsv1alpha1.SeerrConfig{ObjectMeta: metav1.ObjectMeta{Name: "observe", Namespace: "media"}}
	cfg.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{DriftPolicy: &policy}
	// No URL or Secrets: rejection must happen before application access or authentication.
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(cfg).WithObjects(cfg).Build()
	r := &SeerrConfigReconciler{Client: c, Scheme: scheme}
	key := types.NamespacedName{Namespace: cfg.Namespace, Name: cfg.Name}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(context.Background(), key, cfg); err != nil {
		t.Fatal(err)
	}
	for _, condition := range cfg.Status.Conditions {
		if condition.Type == "Synced" && condition.Reason == "InvalidConfig" && condition.Status == metav1.ConditionFalse {
			return
		}
	}
	t.Fatalf("expected explicit unsupported-policy status, got %+v", cfg.Status.Conditions)
}
