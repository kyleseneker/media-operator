package qbittorrent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/kyleseneker/media-operator/internal/engine"
)

// Client is an HTTP client for the qBittorrent WebUI API v2.
// It authenticates via session cookies obtained from the login endpoint.
type Client struct {
	hc       *engine.HTTPClient
	username string
	password string
}

// NewClient creates a new qBittorrent API client backed by the given engine.HTTPClient.
func NewClient(hc *engine.HTTPClient, username, password string) *Client {
	return &Client{
		hc:       hc,
		username: username,
		password: password,
	}
}

// Login authenticates with qBittorrent and stores the session cookie.
func (c *Client) Login(ctx context.Context) error {
	form := url.Values{}
	form.Set("username", c.username)
	form.Set("password", c.password)

	body, err := c.hc.DoForm(ctx, "/api/v2/auth/login", form)
	if err != nil {
		return fmt.Errorf("executing login request: %w", err)
	}

	if resp := strings.TrimSpace(string(body)); resp != "" && !strings.EqualFold(resp, "Ok.") {
		return fmt.Errorf("login failed: %s", resp)
	}

	// The cookie jar captured the session cookie automatically. Register it with
	// the engine's cookie-based auth so applyAuth includes it on later requests.
	name, value := c.hc.CookieValuePrefix("QBT_SID_")
	if value == "" {
		name, value = "SID", c.hc.CookieValue("SID")
	}
	if value == "" {
		return fmt.Errorf("login succeeded but no session cookie returned")
	}
	c.hc.SetCookieSession(name, value)

	return nil
}

// Ping checks if qBittorrent is reachable.
func (c *Client) Ping(ctx context.Context) error {
	return c.hc.Ping(ctx, "/api/v2/app/version")
}

// SetPreferences updates qBittorrent application preferences.
func (c *Client) SetPreferences(ctx context.Context, prefs map[string]any) error {
	prefsJSON, err := json.Marshal(prefs)
	if err != nil {
		return fmt.Errorf("marshaling preferences: %w", err)
	}

	form := url.Values{}
	form.Set("json", string(prefsJSON))

	_, err = c.hc.DoForm(ctx, "/api/v2/app/setPreferences", form)
	return err
}

// GetPreferences returns the current qBittorrent application preferences.
func (c *Client) GetPreferences(ctx context.Context) (map[string]any, error) {
	return c.hc.GetJSON(ctx, "/api/v2/app/preferences")
}

// ListCategories returns all torrent categories.
func (c *Client) ListCategories(ctx context.Context) (map[string]any, error) {
	return c.hc.GetJSON(ctx, "/api/v2/torrents/categories")
}

// CreateCategory creates a new torrent category with the given save path.
func (c *Client) CreateCategory(ctx context.Context, name, savePath string) error {
	form := url.Values{}
	form.Set("category", name)
	form.Set("savePath", savePath)

	_, err := c.hc.DoForm(ctx, "/api/v2/torrents/createCategory", form)
	return err
}

// EditCategory updates an existing torrent category's save path.
func (c *Client) EditCategory(ctx context.Context, name, savePath string) error {
	form := url.Values{}
	form.Set("category", name)
	form.Set("savePath", savePath)

	_, err := c.hc.DoForm(ctx, "/api/v2/torrents/editCategory", form)
	return err
}
