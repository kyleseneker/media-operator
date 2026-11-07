package sabnzbd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/kyleseneker/media-operator/internal/engine"
)

type Client struct {
	hc     *engine.HTTPClient
	apiKey string
}

func NewClient(hc *engine.HTTPClient, apiKey string) *Client {
	return &Client{
		hc:     hc,
		apiKey: apiKey,
	}
}

// apiParams returns form values containing the common API parameters.
// The API key is kept out of the URL and sent in the POST body instead,
// so it does not leak into server/proxy access logs.
func (c *Client) apiParams(mode string, extra string) url.Values {
	params := url.Values{}
	params.Set("mode", mode)
	params.Set("apikey", c.apiKey)
	params.Set("output", "json")
	if extra != "" {
		parsed, err := url.ParseQuery(extra)
		if err == nil {
			for k, vs := range parsed {
				for _, v := range vs {
					params.Set(k, v)
				}
			}
		}
	}
	return params
}

func (c *Client) Ping(ctx context.Context) error {
	_, err := c.hc.DoForm(ctx, "/api", c.apiParams("version", ""))
	return err
}

func (c *Client) GetConfig(ctx context.Context) (map[string]any, error) {
	data, err := c.hc.DoForm(ctx, "/api", c.apiParams("get_config", ""))
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if config, ok := result["config"].(map[string]any); ok {
		return config, nil
	}
	return result, nil
}

func (c *Client) SetConfig(ctx context.Context, section, keyword, value string) error {
	params := c.apiParams("set_config", "")
	params.Set("section", section)
	params.Set("keyword", keyword)
	params.Set("value", value)
	return c.write(ctx, params)
}

// SetNamed creates or updates one entry of a named section (servers,
// categories) in a single call, the only form SABnzbd accepts for them.
func (c *Client) SetNamed(ctx context.Context, section, name string, values map[string]string) error {
	params := c.apiParams("set_config", "")
	params.Set("section", section)
	params.Set("keyword", name)
	for k, v := range values {
		params.Set(k, v)
	}
	return c.write(ctx, params)
}

// write sends a configuration change. SABnzbd reports a rejected change with
// HTTP 200 and {"status": false, "error": ...}, so the body decides success.
func (c *Client) write(ctx context.Context, params url.Values) error {
	data, err := c.hc.DoForm(ctx, "/api", params)
	if err != nil {
		return err
	}
	var reply struct {
		Status *bool  `json:"status"`
		Error  string `json:"error"`
	}
	if json.Unmarshal(data, &reply) == nil && reply.Status != nil && !*reply.Status {
		return fmt.Errorf("sabnzbd rejected the change: %s", reply.Error)
	}
	return nil
}

func (c *Client) SetConfigMulti(ctx context.Context, section string, values map[string]string) error {
	for k, v := range values {
		if err := c.SetConfig(ctx, section, k, v); err != nil {
			return fmt.Errorf("setting %s.%s: %w", section, k, err)
		}
	}
	return nil
}

// DeleteNamed removes a server or category by name.
func (c *Client) DeleteNamed(ctx context.Context, section, name string) error {
	params := c.apiParams("del_config", "")
	params.Set("section", section)
	params.Set("keyword", name)
	return c.write(ctx, params)
}
