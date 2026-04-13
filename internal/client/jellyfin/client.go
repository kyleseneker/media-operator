package jellyfin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/kyleseneker/media-operator/internal/engine"
)

// Client is an HTTP client for the Jellyfin API.
// It delegates all HTTP and auth handling to engine.HTTPClient.
type Client struct {
	hc *engine.HTTPClient
}

// NewClient creates a new Jellyfin API client backed by an engine.HTTPClient.
func NewClient(hc *engine.HTTPClient) *Client {
	return &Client{hc: hc}
}

// IsSetupComplete checks whether the Jellyfin startup wizard has been completed.
func (c *Client) IsSetupComplete(ctx context.Context) (bool, error) {
	result, err := c.hc.GetJSON(ctx, "/System/Info/Public")
	if err != nil {
		return false, err
	}
	completed, ok := result["StartupWizardCompleted"].(bool)
	if !ok {
		return false, nil
	}
	return completed, nil
}

// RunSetupWizard drives the Jellyfin first-time setup wizard to completion.
func (c *Client) RunSetupWizard(ctx context.Context, username, password, serverName, metadataLang, countryCode string) error {
	// Step 1: Set initial user.
	userPayload := map[string]interface{}{
		"Name":     username,
		"Password": password,
	}
	if err := c.hc.PostJSON(ctx, "/Startup/User", userPayload); err != nil {
		return fmt.Errorf("setting startup user: %w", err)
	}

	// Step 2: Set server configuration.
	configPayload := map[string]interface{}{
		"MetadataCountryCode":       countryCode,
		"PreferredMetadataLanguage": metadataLang,
		"UICulture":                 metadataLang,
	}
	if err := c.hc.PostJSON(ctx, "/Startup/Configuration", configPayload); err != nil {
		return fmt.Errorf("setting startup configuration: %w", err)
	}

	// Step 3: Configure remote access.
	remotePayload := map[string]interface{}{
		"EnableRemoteAccess":         true,
		"EnableAutomaticPortMapping": false,
	}
	if err := c.hc.PostJSON(ctx, "/Startup/RemoteAccess", remotePayload); err != nil {
		return fmt.Errorf("setting remote access: %w", err)
	}

	// Step 4: Complete the wizard.
	if err := c.hc.PostJSON(ctx, "/Startup/Complete", nil); err != nil {
		return fmt.Errorf("completing startup wizard: %w", err)
	}

	return nil
}

// Authenticate logs in with username/password and stores the access token.
func (c *Client) Authenticate(ctx context.Context, username, password string) error {
	payload := map[string]interface{}{
		"Username": username,
		"Pw":       password,
	}
	data, err := c.hc.Do(ctx, http.MethodPost, "/Users/AuthenticateByName", payload)
	if err != nil {
		return fmt.Errorf("authenticating: %w", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("unmarshaling auth response: %w", err)
	}
	token, ok := result["AccessToken"].(string)
	if !ok || token == "" {
		return fmt.Errorf("no access token in auth response")
	}
	c.hc.SetMediaToken(token)
	return nil
}

// GetConfig fetches a configuration resource by path.
func (c *Client) GetConfig(ctx context.Context, path string) (map[string]interface{}, error) {
	return c.hc.GetJSON(ctx, path)
}

// PostConfig sends a configuration payload to the given path.
func (c *Client) PostConfig(ctx context.Context, path string, payload map[string]interface{}) error {
	return c.hc.PostJSON(ctx, path, payload)
}

// ListLibraries returns all virtual folder (library) entries.
func (c *Client) ListLibraries(ctx context.Context) ([]map[string]interface{}, error) {
	return c.hc.GetJSONList(ctx, "/Library/VirtualFolders")
}

// CreateLibrary adds a new virtual folder (library).
func (c *Client) CreateLibrary(ctx context.Context, name, collectionType string, libraryOptions map[string]interface{}) error {
	params := url.Values{}
	params.Set("name", name)
	params.Set("collectionType", collectionType)
	params.Set("refreshLibrary", "false")

	path := "/Library/VirtualFolders?" + params.Encode()
	return c.hc.PostJSON(ctx, path, libraryOptions)
}
