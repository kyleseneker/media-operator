package tdarr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/kyleseneker/media-operator/internal/engine"
)

// Client wraps engine.HTTPClient with Tdarr-specific API methods.
type Client struct {
	hc *engine.HTTPClient
}

// NewClient creates a new Tdarr API client.
func NewClient(hc *engine.HTTPClient) *Client {
	return &Client{hc: hc}
}

// CrudDB performs a generic CRUD operation against /api/v2/cruddb.
func (c *Client) CrudDB(ctx context.Context, collection, mode, docID string, obj map[string]any) (map[string]any, error) {
	payload := map[string]any{
		"data": map[string]any{
			"collection": collection,
			"mode":       mode,
			"docID":      docID,
			"obj":        obj,
		},
		"timeout": 20000,
	}

	data, err := c.hc.Do(ctx, http.MethodPost, "/api/v2/cruddb", payload)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling cruddb response: %w", err)
	}
	return result, nil
}

// GetByID fetches a single document by ID from the given collection.
func (c *Client) GetByID(ctx context.Context, collection, docID string) (map[string]any, error) {
	return c.CrudDB(ctx, collection, "getById", docID, nil)
}

// Insert creates a new document in the given collection.
func (c *Client) Insert(ctx context.Context, collection, docID string, obj map[string]any) error {
	_, err := c.CrudDB(ctx, collection, "insert", docID, obj)
	return err
}

// Update modifies an existing document in the given collection.
func (c *Client) Update(ctx context.Context, collection, docID string, obj map[string]any) error {
	_, err := c.CrudDB(ctx, collection, "update", docID, obj)
	return err
}

// Upsert inserts or updates a document. It attempts a GetByID first;
// if the document exists it updates, otherwise it inserts.
func (c *Client) Upsert(ctx context.Context, collection, docID string, obj map[string]any) error {
	existing, err := c.GetByID(ctx, collection, docID)
	if err != nil {
		// If GetByID fails, assume the document does not exist and insert.
		return c.Insert(ctx, collection, docID, obj)
	}
	// A nil or empty result means the document was not found.
	if len(existing) == 0 {
		return c.Insert(ctx, collection, docID, obj)
	}
	return c.Update(ctx, collection, docID, obj)
}

// GetNodes returns all registered Tdarr nodes.
func (c *Client) GetNodes(ctx context.Context) (map[string]any, error) {
	return c.hc.GetJSON(ctx, "/api/v2/get-nodes")
}

// SetWorkerLimit adjusts the worker limit for a specific node and worker type.
func (c *Client) SetWorkerLimit(ctx context.Context, nodeID, workerType string, limit int) error {
	payload := map[string]any{
		"nodeID":     nodeID,
		"workerType": workerType,
		"limit":      limit,
	}
	return c.hc.PostJSON(ctx, "/api/v2/alter-worker-limit", payload)
}

// Ping checks if Tdarr is reachable.
func (c *Client) Ping(ctx context.Context) error {
	return c.hc.Ping(ctx, "/api/v2/status")
}
