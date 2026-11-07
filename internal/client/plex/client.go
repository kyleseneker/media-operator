package plex

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

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

func (c *Client) GetPreferences(ctx context.Context) (map[string]any, error) {
	return c.hc.GetJSON(ctx, "/:/prefs")
}

// GetSettings returns every preference Plex lists, by ID, as the text Plex
// accepts when setting it (booleans as 0/1).
func (c *Client) GetSettings(ctx context.Context) (map[string]string, error) {
	result, err := c.GetPreferences(ctx)
	if err != nil {
		return nil, err
	}
	mc, _ := result["MediaContainer"].(map[string]any)
	list, _ := mc["Setting"].([]any)
	settings := make(map[string]string, len(list))
	for _, item := range list {
		setting, _ := item.(map[string]any)
		id, _ := setting["id"].(string)
		if id == "" {
			continue
		}
		switch v := setting["value"].(type) {
		case bool:
			settings[id] = map[bool]string{true: "1", false: "0"}[v]
		case float64:
			settings[id] = strconv.FormatFloat(v, 'f', -1, 64)
		case nil:
			settings[id] = ""
		default:
			settings[id] = fmt.Sprint(v)
		}
	}
	return settings, nil
}

func (c *Client) SetPreferences(ctx context.Context, prefs map[string]string) error {
	params := url.Values{}
	for k, v := range prefs {
		params.Set(k, v)
	}
	_, err := c.hc.DoRaw(ctx, http.MethodPut, "/:/prefs?"+params.Encode(), nil, "")
	return err
}

func (c *Client) ListLibraries(ctx context.Context) ([]map[string]any, error) {
	result, err := c.hc.GetJSON(ctx, "/library/sections")
	if err != nil {
		return nil, err
	}
	mc, ok := result["MediaContainer"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected response: missing MediaContainer")
	}
	// Plex leaves Directory out when there are no libraries yet.
	dirs, _ := mc["Directory"].([]any)
	var libs []map[string]any
	for _, d := range dirs {
		if m, ok := d.(map[string]any); ok {
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
	// One location parameter per folder; Plex rejects location[0][path].
	for _, p := range paths {
		params.Add("location", p)
	}
	_, err := c.hc.DoRaw(ctx, http.MethodPost, "/library/sections?"+params.Encode(), nil, "")
	return err
}
