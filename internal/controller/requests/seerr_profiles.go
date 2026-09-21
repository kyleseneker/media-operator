package requests

import (
	"context"
	"fmt"
	"math"
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
	profiles, err := hc.GetJSONList(ctx, "/api/v3/qualityprofile")
	if err != nil {
		return nil, fmt.Errorf("listing quality profiles: %w", err)
	}
	matches := 0
	var id int
	for _, profile := range profiles {
		if profile["name"] != svc.ActiveProfileName {
			continue
		}
		matches++
		value, ok := profile["id"].(float64)
		if !ok || value < 1 || value > math.MaxInt32 || value != math.Trunc(value) {
			return nil, fmt.Errorf("quality profile %q has an invalid ID", svc.ActiveProfileName)
		}
		id = int(value)
	}
	if matches != 1 {
		return nil, fmt.Errorf("quality profile %q matched %d profiles; expected exactly one", svc.ActiveProfileName, matches)
	}
	payload["activeProfileId"] = id
	return payload, nil
}
