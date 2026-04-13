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

func (c *Client) GetConfig(ctx context.Context) (map[string]interface{}, error) {
	data, err := c.hc.DoForm(ctx, "/api", c.apiParams("get_config", ""))
	if err != nil {
		return nil, err
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if config, ok := result["config"].(map[string]interface{}); ok {
		return config, nil
	}
	return result, nil
}

func (c *Client) SetConfig(ctx context.Context, section, keyword, value string) error {
	params := c.apiParams("set_config", "")
	params.Set("section", section)
	params.Set("keyword", keyword)
	params.Set("value", value)
	_, err := c.hc.DoForm(ctx, "/api", params)
	return err
}

func (c *Client) SetConfigMulti(ctx context.Context, section string, values map[string]string) error {
	for k, v := range values {
		if err := c.SetConfig(ctx, section, k, v); err != nil {
			return fmt.Errorf("setting %s.%s: %w", section, k, err)
		}
	}
	return nil
}
