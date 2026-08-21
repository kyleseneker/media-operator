package requests

import (
	"testing"

	requestsv1alpha1 "github.com/kyleseneker/media-operator/api/requests/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/controller/internal/contract"
)

// Seerr stores a service connection wholesale and answers 2xx for keys it does not
// use, so a spec field the builder never maps is silently inert. APIKeySecretRef is
// resolved before the payload is built and lands as "apiKey".
func TestSeerrServiceFieldsAllReachThePayload(t *testing.T) {
	contract.AssertEveryFieldEmitted(t, &requestsv1alpha1.SeerrServiceConnection{},
		func(s *requestsv1alpha1.SeerrServiceConnection) map[string]any {
			return buildServicePayload(s, "resolved-key")
		})
}

// Seerr rejects a service that omits these, so they are sent even when the CR
// leaves them unset.
func TestSeerrServicePayloadAlwaysSendsRequiredKeys(t *testing.T) {
	got := buildServicePayload(&requestsv1alpha1.SeerrServiceConnection{
		Name: "Sonarr", Hostname: "sonarr.arr.svc",
	}, "resolved-key")

	for _, k := range []string{"name", "hostname", "apiKey", "port", "useSsl", "activeProfileId", "is4k", "isDefault"} {
		if _, ok := got[k]; !ok {
			t.Errorf("required key %q missing from a minimal service payload: %v", k, got)
		}
	}
	if got["apiKey"] != "resolved-key" {
		t.Errorf("apiKey = %v, want the resolved secret value", got["apiKey"])
	}
}
