package requests

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	requestsv1alpha1 "github.com/kyleseneker/media-operator/api/requests/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/engine"
)

// Names survive a database rebuild; numeric profile IDs do not. Resolve before
// writing Seerr settings, and leave its existing connection intact on failure.
func resolvedServicePayload(ctx context.Context, svc *requestsv1alpha1.SeerrServiceConnection, apiKey string) (map[string]any, error) {
	payload := buildServicePayload(svc, apiKey)
	if svc.ActiveProfileName == "" {
		return payload, nil // Preserve ID-only configurations.
	}
	if svc.Port == nil || *svc.Port < 1 || *svc.Port > 65535 {
		return nil, fmt.Errorf("a valid service port is required for profile name resolution")
	}
	scheme := "http"
	if svc.UseSsl != nil && *svc.UseSsl {
		scheme = "https"
	}
	endpoint := url.URL{Scheme: scheme, Host: net.JoinHostPort(svc.Hostname, strconv.Itoa(*svc.Port)), Path: "/" + strings.Trim(svc.BaseUrl, "/")}
	hc, err := engine.NewHTTPClient(endpoint.String(), engine.AuthAPIKey, engine.WithAPIKey(apiKey))
	if err != nil {
		return nil, err
	}
	profiles, err := engine.FetchNamedIDs(ctx, hc, "/api/v3/qualityprofile", "name")
	if err != nil {
		return nil, err
	}
	id, err := profiles.Resolve(svc.ActiveProfileName)
	if err != nil {
		return nil, fmt.Errorf("activeProfileName: %w", err)
	}
	payload["activeProfileId"] = id
	return payload, nil
}
