package flaresolverr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/kyleseneker/media-operator/internal/engine"
)

// Client is an HTTP client for the FlareSolverr API v1.
// FlareSolverr uses no authentication by default.
type Client struct {
	hc *engine.HTTPClient
}

// NewClient creates a new FlareSolverr API client.
func NewClient(hc *engine.HTTPClient) *Client {
	return &Client{hc: hc}
}

// response represents a FlareSolverr API response.
type response struct {
	Status   string   `json:"status"`
	Message  string   `json:"message"`
	Sessions []string `json:"sessions,omitempty"`
	Version  string   `json:"version,omitempty"`
}

func (c *Client) do(ctx context.Context, body interface{}) (*response, error) {
	data, err := c.hc.Do(ctx, http.MethodPost, "/v1", body)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	var result response
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	if result.Status != "ok" {
		return nil, fmt.Errorf("FlareSolverr error: %s", result.Message)
	}

	return &result, nil
}

// Ping checks if FlareSolverr is reachable by listing sessions.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.ListSessions(ctx)
	return err
}

// ListSessions returns all active session names.
func (c *Client) ListSessions(ctx context.Context) ([]string, error) {
	result, err := c.do(ctx, map[string]string{
		"cmd": "sessions.list",
	})
	if err != nil {
		return nil, err
	}
	return result.Sessions, nil
}

// CreateSession creates a named browser session.
func (c *Client) CreateSession(ctx context.Context, name string) error {
	_, err := c.do(ctx, map[string]string{
		"cmd":     "sessions.create",
		"session": name,
	})
	return err
}

// DestroySession destroys a named browser session.
func (c *Client) DestroySession(ctx context.Context, name string) error {
	_, err := c.do(ctx, map[string]string{
		"cmd":     "sessions.destroy",
		"session": name,
	})
	return err
}
