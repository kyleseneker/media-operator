package plex

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/kyleseneker/media-operator/internal/engine"
)

type Client struct {
	hc *engine.HTTPClient
}

func NewClient(hc *engine.HTTPClient) *Client {
	return &Client{hc: hc}
}

func (c *Client) Ping(ctx context.Context) error {
	return c.hc.Ping(ctx, "/")
}

func (c *Client) GetPreferences(ctx context.Context) (map[string]interface{}, error) {
	return c.hc.GetJSON(ctx, "/:/prefs")
}

func (c *Client) SetPreferences(ctx context.Context, prefs map[string]string) error {
	params := url.Values{}
	for k, v := range prefs {
		params.Set(k, v)
	}
	_, err := c.hc.DoRaw(ctx, http.MethodPut, "/:/prefs?"+params.Encode(), nil, "")
	return err
}

func (c *Client) ListLibraries(ctx context.Context) ([]map[string]interface{}, error) {
	result, err := c.hc.GetJSON(ctx, "/library/sections")
	if err != nil {
		return nil, err
	}
	mc, ok := result["MediaContainer"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected response: missing MediaContainer")
	}
	dirs, ok := mc["Directory"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected response: missing Directory in MediaContainer")
	}
	var libs []map[string]interface{}
	for _, d := range dirs {
		if m, ok := d.(map[string]interface{}); ok {
			libs = append(libs, m)
		}
	}
	return libs, nil
}

func (c *Client) CreateLibrary(ctx context.Context, name, libType, agent, scanner, language string, paths []string) error {
	params := url.Values{}
	params.Set("name", name)
	params.Set("type", libType)
	params.Set("agent", agent)
	params.Set("scanner", scanner)
	params.Set("language", language)
	for i, p := range paths {
		params.Set(fmt.Sprintf("location[%d][path]", i), p)
	}
	_, err := c.hc.DoRaw(ctx, http.MethodPost, "/library/sections?"+params.Encode(), nil, "")
	return err
}
