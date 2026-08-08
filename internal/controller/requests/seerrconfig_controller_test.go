package requests

import (
	"testing"

	requestsv1alpha1 "github.com/kyleseneker/media-operator/api/requests/v1alpha1"
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
