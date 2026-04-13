package maintainerr

import (
	"context"
	"fmt"

	"github.com/kyleseneker/media-operator/internal/engine"
)

// Client wraps engine.HTTPClient with Maintainerr-specific API methods.
type Client struct {
	hc *engine.HTTPClient
}

// NewClient creates a new Maintainerr API client.
func NewClient(hc *engine.HTTPClient) *Client {
	return &Client{hc: hc}
}

// Ping checks if Maintainerr is reachable.
func (c *Client) Ping(ctx context.Context) error {
	return c.hc.Ping(ctx, "/api/status")
}

// --- Settings ---

// GetSettings returns the current Maintainerr settings.
func (c *Client) GetSettings(ctx context.Context) (map[string]interface{}, error) {
	return c.hc.GetJSON(ctx, "/api/settings")
}

// UpdateSettings updates the Maintainerr settings.
func (c *Client) UpdateSettings(ctx context.Context, settings map[string]interface{}) error {
	return c.hc.PutJSON(ctx, "/api/settings", settings)
}

// --- Plex Connection ---

// UpdatePlexSettings updates the Plex connection settings.
func (c *Client) UpdatePlexSettings(ctx context.Context, settings map[string]interface{}) error {
	return c.hc.PutJSON(ctx, "/api/settings/plex", settings)
}

// --- Sonarr Connection ---

// UpdateSonarrSettings updates the Sonarr connection settings.
func (c *Client) UpdateSonarrSettings(ctx context.Context, settings map[string]interface{}) error {
	return c.hc.PutJSON(ctx, "/api/settings/sonarr", settings)
}

// --- Radarr Connection ---

// UpdateRadarrSettings updates the Radarr connection settings.
func (c *Client) UpdateRadarrSettings(ctx context.Context, settings map[string]interface{}) error {
	return c.hc.PutJSON(ctx, "/api/settings/radarr", settings)
}

// --- Overseerr Connection ---

// UpdateOverseerrSettings updates the Overseerr/Jellyseerr connection settings.
func (c *Client) UpdateOverseerrSettings(ctx context.Context, settings map[string]interface{}) error {
	return c.hc.PutJSON(ctx, "/api/settings/overseerr", settings)
}

// --- Rules ---

// ListRules returns all rules.
func (c *Client) ListRules(ctx context.Context) ([]map[string]interface{}, error) {
	return c.hc.GetJSONList(ctx, "/api/rules")
}

// CreateRule creates a new rule.
func (c *Client) CreateRule(ctx context.Context, rule map[string]interface{}) error {
	return c.hc.PostJSON(ctx, "/api/rules", rule)
}

// UpdateRule updates an existing rule.
func (c *Client) UpdateRule(ctx context.Context, id int, rule map[string]interface{}) error {
	return c.hc.PutJSON(ctx, fmt.Sprintf("/api/rules/%d", id), rule)
}

// DeleteRule deletes a rule.
func (c *Client) DeleteRule(ctx context.Context, id int) error {
	return c.hc.DeleteJSON(ctx, fmt.Sprintf("/api/rules/%d", id))
}
