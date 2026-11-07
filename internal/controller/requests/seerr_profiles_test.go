package requests

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	requestsv1alpha1 "github.com/kyleseneker/media-operator/api/requests/v1alpha1"
)

func TestResolveServiceProfileByName(t *testing.T) {
	cases := []struct {
		name, response string
		status, wantID int
		wantError      bool
	}{
		{"recreated profile overrides stale ID", `[{"id":8,"name":"WEB-1080p"}]`, 200, 8, false},
		{"select exact name", `[{"id":3,"name":"Other"},{"id":12,"name":"WEB-1080p"}]`, 200, 12, false},
		{"missing name", `[{"id":7,"name":"Other"}]`, 200, 0, true},
		{"duplicate name", `[{"id":7,"name":"WEB-1080p"},{"id":8,"name":"WEB-1080p"}]`, 200, 0, true},
		{"missing ID", `[{"name":"WEB-1080p"}]`, 200, 0, true},
		{"fractional ID", `[{"id":1.5,"name":"WEB-1080p"}]`, 200, 0, true},
		{"zero ID", `[{"id":0,"name":"WEB-1080p"}]`, 200, 0, true},
		{"unauthorized", `{}`, 401, 0, true},
		{"unavailable", `{}`, 503, 0, true},
		{"malformed response", `{}`, 200, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ln, err := net.Listen("tcp", net.JoinHostPort(routableAddr(t), "0"))
			require.NoError(t, err)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "GET", r.Method, "unexpected request method")
				assert.Equal(t, "/sonarr/api/v3/qualityprofile", r.URL.Path, "unexpected request path")
				assert.Equal(t, "lab-key", r.Header.Get("X-Api-Key"), "missing service authentication")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.response))
			}))
			_ = server.Listener.Close()
			server.Listener = ln
			server.Start()
			defer server.Close()
			endpoint, _ := url.Parse(server.URL)
			port, _ := strconv.Atoi(endpoint.Port())
			oldID := 7
			svc := &requestsv1alpha1.SeerrServiceConnection{Hostname: endpoint.Hostname(), Port: &port, BaseUrl: "/sonarr/", ActiveProfileId: &oldID, ActiveProfileName: "WEB-1080p"}
			payload, err := resolvedServicePayload(context.Background(), svc, "lab-key")
			if tc.wantError {
				require.Error(t, err)
				require.Nil(t, payload, "failed resolution returned a writable payload")
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.wantID, payload["activeProfileId"], "profile ID")
			}
			require.Equal(t, oldID, *svc.ActiveProfileId, "resolution mutated the custom resource")
		})
	}
}

func TestResolveServiceProfileIDOnly(t *testing.T) {
	id := 7
	// Invalid hostname proves ID-only configurations do not need an API lookup.
	svc := &requestsv1alpha1.SeerrServiceConnection{Hostname: "", ActiveProfileId: &id}
	payload, err := resolvedServicePayload(context.Background(), svc, "lab-key")
	require.NoError(t, err, "legacy ID-only configuration changed")
	require.Equal(t, id, payload["activeProfileId"], "legacy ID-only configuration changed")
}
