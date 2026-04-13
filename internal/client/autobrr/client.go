package autobrr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/kyleseneker/media-operator/internal/engine"
)

// Client wraps engine.HTTPClient with Autobrr-specific API methods.
type Client struct {
	hc *engine.HTTPClient
}

// NewClient creates a new Autobrr API client.
func NewClient(hc *engine.HTTPClient) *Client {
	return &Client{hc: hc}
}

// Ping checks if Autobrr is reachable.
func (c *Client) Ping(ctx context.Context) error {
	return c.hc.Ping(ctx, "/api/healthz/liveness")
}

// --- Download Clients ---

func (c *Client) ListDownloadClients(ctx context.Context) ([]map[string]interface{}, error) {
	return c.hc.GetJSONList(ctx, "/api/download_clients")
}

func (c *Client) CreateDownloadClient(ctx context.Context, dc map[string]interface{}) error {
	return c.hc.PostJSON(ctx, "/api/download_clients", dc)
}

func (c *Client) UpdateDownloadClient(ctx context.Context, id int, dc map[string]interface{}) error {
	return c.hc.PutJSON(ctx, fmt.Sprintf("/api/download_clients/%d", id), dc)
}

// --- Indexers ---

func (c *Client) ListIndexers(ctx context.Context) ([]map[string]interface{}, error) {
	return c.hc.GetJSONList(ctx, "/api/indexer")
}

func (c *Client) CreateIndexer(ctx context.Context, idx map[string]interface{}) error {
	return c.hc.PostJSON(ctx, "/api/indexer", idx)
}

func (c *Client) UpdateIndexer(ctx context.Context, id int, idx map[string]interface{}) error {
	return c.hc.PutJSON(ctx, fmt.Sprintf("/api/indexer/%d", id), idx)
}

// --- IRC Networks ---

func (c *Client) ListIRCNetworks(ctx context.Context) ([]map[string]interface{}, error) {
	return c.hc.GetJSONList(ctx, "/api/irc")
}

func (c *Client) CreateIRCNetwork(ctx context.Context, net map[string]interface{}) error {
	return c.hc.PostJSON(ctx, "/api/irc", net)
}

func (c *Client) UpdateIRCNetwork(ctx context.Context, id int, net map[string]interface{}) error {
	return c.hc.PutJSON(ctx, fmt.Sprintf("/api/irc/%d", id), net)
}

// --- Feeds ---

func (c *Client) ListFeeds(ctx context.Context) ([]map[string]interface{}, error) {
	return c.hc.GetJSONList(ctx, "/api/feeds")
}

func (c *Client) CreateFeed(ctx context.Context, feed map[string]interface{}) error {
	return c.hc.PostJSON(ctx, "/api/feeds", feed)
}

func (c *Client) UpdateFeed(ctx context.Context, id int, feed map[string]interface{}) error {
	return c.hc.PutJSON(ctx, fmt.Sprintf("/api/feeds/%d", id), feed)
}

// --- Filters ---

func (c *Client) ListFilters(ctx context.Context) ([]map[string]interface{}, error) {
	return c.hc.GetJSONList(ctx, "/api/filters")
}

func (c *Client) CreateFilter(ctx context.Context, filter map[string]interface{}) (map[string]interface{}, error) {
	data, err := c.hc.Do(ctx, http.MethodPost, "/api/filters", filter)
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling created filter: %w", err)
	}
	return result, nil
}

func (c *Client) UpdateFilter(ctx context.Context, id int, filter map[string]interface{}) error {
	return c.hc.PutJSON(ctx, fmt.Sprintf("/api/filters/%d", id), filter)
}
