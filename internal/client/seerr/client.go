package seerr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/kyleseneker/media-operator/internal/engine"
)

// Client is an HTTP client for the Seerr API v1.
// It delegates all HTTP work to an engine.HTTPClient configured with AuthSession.
type Client struct {
	hc *engine.HTTPClient
}

// NewClient creates a new Seerr API client wrapping the given engine.HTTPClient.
func NewClient(hc *engine.HTTPClient) *Client {
	return &Client{hc: hc}
}

// IsInitialized checks whether Seerr has been set up by reading the public settings.
func (c *Client) IsInitialized(ctx context.Context) (bool, error) {
	data, err := c.hc.Do(ctx, http.MethodGet, "/api/v1/settings/public", nil)
	if err != nil {
		return false, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return false, fmt.Errorf("unmarshaling public settings: %w", err)
	}
	initialized, ok := result["initialized"].(bool)
	if !ok {
		return false, nil
	}
	return initialized, nil
}

// AuthenticatePlex logs in via the Plex auth provider.
// The session cookie is stored in the engine client's cookie jar.
func (c *Client) AuthenticatePlex(ctx context.Context, plexToken string) error {
	payload := map[string]interface{}{"authToken": plexToken}
	_, err := c.hc.Do(ctx, http.MethodPost, "/api/v1/auth/plex", payload)
	return err
}

// AuthenticateJellyfin logs in via the Jellyfin auth provider.
// The session cookie is stored in the engine client's cookie jar.
func (c *Client) AuthenticateJellyfin(ctx context.Context, username, password, jellyfinHost string, jellyfinPort int) error {
	payload := map[string]interface{}{
		"username": username,
		"password": password,
		"hostname": jellyfinHost,
		"port":     jellyfinPort,
	}
	_, err := c.hc.Do(ctx, http.MethodPost, "/api/v1/auth/jellyfin", payload)
	return err
}

// GetAPIKey fetches the main settings and returns the API key.
// This requires an active session (cookie auth).
func (c *Client) GetAPIKey(ctx context.Context) (string, error) {
	data, err := c.hc.Do(ctx, http.MethodGet, "/api/v1/settings/main", nil)
	if err != nil {
		return "", err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("unmarshaling main settings: %w", err)
	}
	key, ok := result["apiKey"].(string)
	if !ok || key == "" {
		return "", fmt.Errorf("no apiKey in main settings")
	}
	return key, nil
}

// SetAPIKey stores an API key for use in subsequent requests.
func (c *Client) SetAPIKey(apiKey string) {
	c.hc.SetAPIKey(apiKey)
}

// Get performs a GET request and returns the response as a map.
func (c *Client) Get(ctx context.Context, path string) (map[string]interface{}, error) {
	return c.hc.GetJSON(ctx, path)
}

// GetList performs a GET request and returns the response as a slice of maps.
func (c *Client) GetList(ctx context.Context, path string) ([]map[string]interface{}, error) {
	return c.hc.GetJSONList(ctx, path)
}

// Post performs a POST request and returns the response as a map.
func (c *Client) Post(ctx context.Context, path string, payload map[string]interface{}) (map[string]interface{}, error) {
	data, err := c.hc.Do(ctx, http.MethodPost, path, payload)
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}
	return result, nil
}

// Put performs a PUT request.
func (c *Client) Put(ctx context.Context, path string, payload map[string]interface{}) error {
	return c.hc.PutJSON(ctx, path, payload)
}

// Ping checks if Seerr is reachable.
func (c *Client) Ping(ctx context.Context) error {
	return c.hc.Ping(ctx, "/api/v1/settings/public")
}
