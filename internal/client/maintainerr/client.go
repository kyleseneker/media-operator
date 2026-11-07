package maintainerr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/kyleseneker/media-operator/internal/engine"
)

// Client wraps engine.HTTPClient with the Maintainerr 3 API.
//
// Maintainerr's settings endpoints answer 2xx even when they refuse a write and
// carry the outcome in the body as {status, code, message}; code 0 is a failure.
// Every settings write here checks it.
type Client struct {
	hc *engine.HTTPClient
}

// NewClient creates a new Maintainerr API client.
func NewClient(hc *engine.HTTPClient) *Client {
	return &Client{hc: hc}
}

// Arr is a kind of server Maintainerr keeps a list of.
type Arr string

// The server lists Maintainerr keeps.
const (
	Sonarr Arr = "sonarr"
	Radarr Arr = "radarr"
)

// Service is an integration Maintainerr stores as a URL and an API key.
type Service string

// The services Maintainerr stores as {url, api_key}.
const (
	Seerr    Service = "seerr"
	Tautulli Service = "tautulli"
)

// Ping checks that Maintainerr is up and can reach its database.
func (c *Client) Ping(ctx context.Context) error {
	return c.hc.Ping(ctx, "/api/health/ready")
}

// GetSettings returns the settings row. Secrets in it are masked; never write a
// value read here back.
func (c *Client) GetSettings(ctx context.Context) (map[string]any, error) {
	return c.hc.GetJSON(ctx, "/api/settings")
}

// UpdateSettings merges the given fields over the stored settings.
func (c *Client) UpdateSettings(ctx context.Context, fields map[string]any) error {
	return c.post(ctx, "/api/settings", fields)
}

// SetMediaServerType sets the media server on a Maintainerr that has none. On a
// Maintainerr that has one, the same call deletes every collection.
func (c *Client) SetMediaServerType(ctx context.Context, serverType string) error {
	return c.post(ctx, "/api/settings/media-server/switch",
		map[string]any{"targetServerType": serverType, "migrateRules": false})
}

// SavePlexToken stores the Plex token. Maintainerr does not validate it.
func (c *Client) SavePlexToken(ctx context.Context, token string) error {
	return c.post(ctx, "/api/settings/plex/token", map[string]any{"plex_auth_token": token})
}

// GetJellyfin returns the stored Jellyfin connection, API key included.
func (c *Client) GetJellyfin(ctx context.Context) (map[string]any, error) {
	return c.hc.GetJSON(ctx, "/api/settings/jellyfin")
}

// SaveJellyfin tests and stores the Jellyfin connection, and makes Jellyfin the
// media server without the clean-up a switch does.
func (c *Client) SaveJellyfin(ctx context.Context, conn map[string]any) error {
	return c.post(ctx, "/api/settings/jellyfin", conn)
}

// ListArr returns the configured servers of a kind, API keys included.
func (c *Client) ListArr(ctx context.Context, kind Arr) ([]map[string]any, error) {
	return c.hc.GetJSONList(ctx, "/api/settings/"+string(kind))
}

// AddArr adds a server.
func (c *Client) AddArr(ctx context.Context, kind Arr, server map[string]any) error {
	return c.post(ctx, "/api/settings/"+string(kind), server)
}

// UpdateArr replaces a server's fields.
func (c *Client) UpdateArr(ctx context.Context, kind Arr, id int, server map[string]any) error {
	return c.checked(c.hc.Do(ctx, http.MethodPut, fmt.Sprintf("/api/settings/%s/%d", kind, id), server))
}

// QualityProfiles returns a server's quality profiles as {id, name}.
func (c *Client) QualityProfiles(ctx context.Context, kind Arr, id int) ([]map[string]any, error) {
	return c.hc.GetJSONList(ctx, fmt.Sprintf("/api/servarr/%s/%d/profiles", kind, id))
}

// GetService returns a service's stored {url, api_key}.
func (c *Client) GetService(ctx context.Context, svc Service) (map[string]any, error) {
	return c.hc.GetJSON(ctx, "/api/settings/"+string(svc))
}

// SaveService stores a service's {url, api_key}.
func (c *Client) SaveService(ctx context.Context, svc Service, conn map[string]any) error {
	return c.post(ctx, "/api/settings/"+string(svc), conn)
}

// ListLibraries returns the media server's libraries as {id, title, type}.
func (c *Client) ListLibraries(ctx context.Context) ([]map[string]any, error) {
	return c.hc.GetJSONList(ctx, "/api/media-server/libraries")
}

// RuleConstants returns the rule applications and their properties.
// Applications that are not configured are left out.
func (c *Client) RuleConstants(ctx context.Context) (map[string]any, error) {
	return c.hc.GetJSON(ctx, "/api/rules/constants")
}

// ListRuleGroups returns every rule group with its rules and collection.
func (c *Client) ListRuleGroups(ctx context.Context) ([]map[string]any, error) {
	return c.hc.GetJSONList(ctx, "/api/rules")
}

// CreateRuleGroup creates a rule group and its collection. Maintainerr does not
// return the new group's id.
func (c *Client) CreateRuleGroup(ctx context.Context, group map[string]any) error {
	return c.checked(ruleGroupRejection(c.hc.Do(ctx, http.MethodPost, "/api/rules", group)))
}

// UpdateRuleGroup replaces a rule group, which carries its id. Every rule is
// deleted and recreated.
func (c *Client) UpdateRuleGroup(ctx context.Context, group map[string]any) error {
	return c.checked(ruleGroupRejection(c.hc.Do(ctx, http.MethodPut, "/api/rules", group)))
}

// ruleGroupRejection adds Maintainerr's reason to a rejected rule group save.
// The reason names the rule at fault ("First value is not available") and a
// rule group carries no secrets, unlike the bodies APIError leaves out.
func ruleGroupRejection(data []byte, err error) ([]byte, error) {
	var apiErr *engine.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode >= http.StatusInternalServerError {
		return data, err
	}
	var body struct {
		Message any `json:"message"`
	}
	if json.Unmarshal(apiErr.Body, &body) != nil || body.Message == nil {
		return data, err
	}
	return data, fmt.Errorf("maintainerr rejected the rule group (%d): %v", apiErr.StatusCode, body.Message)
}

func (c *Client) post(ctx context.Context, path string, body map[string]any) error {
	return c.checked(c.hc.Do(ctx, http.MethodPost, path, body))
}

// checked turns a {code: 0} body into an error. Bodies without a code, such as
// a bare true, are successes.
func (c *Client) checked(data []byte, err error) error {
	if err != nil {
		return err
	}
	var result struct {
		Code    *int   `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &result) != nil || result.Code == nil || *result.Code != 0 {
		return nil
	}
	if result.Message == "" {
		result.Message = "no reason given"
	}
	return fmt.Errorf("maintainerr refused the change: %s", result.Message)
}
