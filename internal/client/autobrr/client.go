package autobrr

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"

	"github.com/kyleseneker/media-operator/internal/engine"
)

// Client wraps engine.HTTPClient with Autobrr-specific API methods.
// FilterFields maps the AutobrrFilter fields Autobrr stores as given to its keys.
var FilterFields = map[string]string{
	"priority":             "priority",
	"matchReleases":        "match_releases",
	"exceptReleases":       "except_releases",
	"matchCategories":      "match_categories",
	"resolutions":          "resolutions",
	"sources":              "sources",
	"codecs":               "codecs",
	"minSize":              "min_size",
	"maxSize":              "max_size",
	"delay":                "delay",
	"useRegex":             "use_regex",
	"matchReleaseGroups":   "match_release_groups",
	"exceptReleaseGroups":  "except_release_groups",
	"exceptCategories":     "except_categories",
	"matchUploaders":       "match_uploaders",
	"exceptUploaders":      "except_uploaders",
	"matchReleaseTags":     "match_release_tags",
	"exceptReleaseTags":    "except_release_tags",
	"useRegexReleaseTags":  "use_regex_release_tags",
	"matchDescription":     "match_description",
	"exceptDescription":    "except_description",
	"useRegexDescription":  "use_regex_description",
	"containers":           "containers",
	"matchHdr":             "match_hdr",
	"exceptHdr":            "except_hdr",
	"matchOther":           "match_other",
	"exceptOther":          "except_other",
	"origins":              "origins",
	"exceptOrigins":        "except_origins",
	"announceTypes":        "announce_types",
	"matchLanguage":        "match_language",
	"exceptLanguage":       "except_language",
	"shows":                "shows",
	"seasons":              "seasons",
	"episodes":             "episodes",
	"smartEpisode":         "smart_episode",
	"years":                "years",
	"months":               "months",
	"days":                 "days",
	"freeleech":            "freeleech",
	"freeleechPercent":     "freeleech_percent",
	"maxDownloads":         "max_downloads",
	"maxDownloadsUnit":     "max_downloads_unit",
	"minSeeders":           "min_seeders",
	"maxSeeders":           "max_seeders",
	"minLeechers":          "min_leechers",
	"maxLeechers":          "max_leechers",
	"tagsMatchLogic":       "tags_match_logic",
	"exceptTagsMatchLogic": "except_tags_match_logic",
	"artists":              "artists",
	"albums":               "albums",
	"matchReleaseTypes":    "match_release_types",
	"exceptReleaseTypes":   "except_release_types",
	"formats":              "formats",
	"quality":              "quality",
	"media":                "media",
	"perfectFlac":          "perfect_flac",
	"cue":                  "cue",
	"log":                  "log",
	"logScore":             "log_score",
}

// ActionFields maps the AutobrrFilterAction fields Autobrr stores as given to its keys.
var ActionFields = map[string]string{
	"category":               "category",
	"savePath":               "save_path",
	"webhookURL":             "webhook_host",
	"execCommand":            "exec_cmd",
	"execArgs":               "exec_args",
	"watchFolder":            "watch_folder",
	"tags":                   "tags",
	"label":                  "label",
	"downloadPath":           "download_path",
	"paused":                 "paused",
	"ignoreRules":            "ignore_rules",
	"skipHashCheck":          "skip_hash_check",
	"firstLastPiecePrio":     "first_last_piece_prio",
	"contentLayout":          "content_layout",
	"priority":               "priority",
	"limitUploadSpeed":       "limit_upload_speed",
	"limitDownloadSpeed":     "limit_download_speed",
	"limitRatio":             "limit_ratio",
	"limitSeedTime":          "limit_seed_time",
	"reannounceSkip":         "reannounce_skip",
	"reannounceDelete":       "reannounce_delete",
	"reannounceInterval":     "reannounce_interval",
	"reannounceMaxAttempts":  "reannounce_max_attempts",
	"webhookType":            "webhook_type",
	"webhookMethod":          "webhook_method",
	"webhookData":            "webhook_data",
	"webhookHeaders":         "webhook_headers",
	"externalDownloadClient": "external_download_client",
}

type Client struct {
	hc *engine.HTTPClient
}

// Types maps AutobrrConfig's download client and action types to Autobrr's
// constants, which are all Autobrr accepts.
var Types = map[string]string{
	"qBittorrent": "QBITTORRENT", "Deluge": "DELUGE_V2", "Transmission": "TRANSMISSION",
	"rTorrent": "RTORRENT", "SABnzbd": "SABNZBD", "Sonarr": "SONARR", "Radarr": "RADARR",
	"Lidarr": "LIDARR", "Readarr": "READARR", "Whisparr": "WHISPARR",
	"Webhook": "WEBHOOK", "Exec": "EXEC", "WatchFolder": "WATCH_FOLDER",
}

// IRCAuthMechanisms maps AutobrrConfig's IRC auth mechanisms to Autobrr's constants.
var IRCAuthMechanisms = map[string]string{"None": "NONE", "NickServ": "NICKSERV", "SASL_PLAIN": "SASL_PLAIN"}

// NewClient creates a new Autobrr API client.
func NewClient(hc *engine.HTTPClient) *Client {
	return &Client{hc: hc}
}

// Ping checks if Autobrr is reachable.
func (c *Client) Ping(ctx context.Context) error {
	return c.hc.Ping(ctx, "/api/healthz/liveness")
}

// --- Download Clients ---
// Autobrr serves download clients as "downloaders" and updates them without an
// ID in the path; the ID travels in the body.

func (c *Client) ListDownloadClients(ctx context.Context) ([]map[string]any, error) {
	return c.hc.GetJSONList(ctx, "/api/downloaders")
}

func (c *Client) CreateDownloadClient(ctx context.Context, dc map[string]any) error {
	return c.hc.PostJSON(ctx, "/api/downloaders", dc)
}

func (c *Client) UpdateDownloadClient(ctx context.Context, id int, dc map[string]any) error {
	body := maps.Clone(dc)
	body["id"] = id
	return c.hc.PutJSON(ctx, "/api/downloaders", body)
}

// --- Indexers ---

// ImplementationIRC is the implementation of an indexer that announces over IRC.
const ImplementationIRC = "irc"

// IndexerDefinitions returns the indexer definitions Autobrr knows, each with
// its identifier, implementation and urls.
func (c *Client) IndexerDefinitions(ctx context.Context) ([]map[string]any, error) {
	return c.hc.GetJSONList(ctx, "/api/indexer/schema")
}

func (c *Client) ListIndexers(ctx context.Context) ([]map[string]any, error) {
	return c.hc.GetJSONList(ctx, "/api/indexer")
}

// GetIndexer returns one indexer as stored, with its settings. The indexer
// list returns each indexer's definition instead.
func (c *Client) GetIndexer(ctx context.Context, id int) (map[string]any, error) {
	return c.hc.GetJSON(ctx, fmt.Sprintf("/api/indexer/%d", id))
}

func (c *Client) CreateIndexer(ctx context.Context, idx map[string]any) error {
	return c.hc.PostJSON(ctx, "/api/indexer", idx)
}

func (c *Client) UpdateIndexer(ctx context.Context, id int, idx map[string]any) error {
	return c.hc.PutJSON(ctx, fmt.Sprintf("/api/indexer/%d", id), idx)
}

// --- IRC Networks ---

func (c *Client) ListIRCNetworks(ctx context.Context) ([]map[string]any, error) {
	return c.hc.GetJSONList(ctx, "/api/irc")
}

func (c *Client) CreateIRCNetwork(ctx context.Context, net map[string]any) error {
	return c.hc.PostJSON(ctx, "/api/irc", net)
}

func (c *Client) UpdateIRCNetwork(ctx context.Context, id int, net map[string]any) error {
	body := maps.Clone(net)
	body["id"] = id
	return c.hc.PutJSON(ctx, fmt.Sprintf("/api/irc/network/%d", id), body)
}

// --- Feeds ---

func (c *Client) ListFeeds(ctx context.Context) ([]map[string]any, error) {
	return c.hc.GetJSONList(ctx, "/api/feeds")
}

func (c *Client) CreateFeed(ctx context.Context, feed map[string]any) error {
	return c.hc.PostJSON(ctx, "/api/feeds", feed)
}

func (c *Client) UpdateFeed(ctx context.Context, id int, feed map[string]any) error {
	return c.hc.PutJSON(ctx, fmt.Sprintf("/api/feeds/%d", id), feed)
}

// --- Filters ---

func (c *Client) ListFilters(ctx context.Context) ([]map[string]any, error) {
	return c.hc.GetJSONList(ctx, "/api/filters")
}

func (c *Client) CreateFilter(ctx context.Context, filter map[string]any) (map[string]any, error) {
	data, err := c.hc.Do(ctx, http.MethodPost, "/api/filters", filter)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling created filter: %w", err)
	}
	return result, nil
}

func (c *Client) UpdateFilter(ctx context.Context, id int, filter map[string]any) error {
	return c.hc.PutJSON(ctx, fmt.Sprintf("/api/filters/%d", id), filter)
}

// GetFilter returns one filter.
func (c *Client) GetFilter(ctx context.Context, id int) (map[string]any, error) {
	return c.hc.GetJSON(ctx, fmt.Sprintf("/api/filters/%d", id))
}

// --- Actions ---
// Autobrr stores a filter's actions separately and ignores actions sent with
// the filter itself.

// ListActions returns a filter's actions; the filter itself does not carry them.
func (c *Client) ListActions(ctx context.Context, filterID int) ([]map[string]any, error) {
	return c.hc.GetJSONList(ctx, fmt.Sprintf("/api/actions?filter_id=%d", filterID))
}

func (c *Client) CreateAction(ctx context.Context, action map[string]any) error {
	return c.hc.PostJSON(ctx, "/api/actions", action)
}

func (c *Client) UpdateAction(ctx context.Context, id int, action map[string]any) error {
	return c.hc.PutJSON(ctx, fmt.Sprintf("/api/actions/%d", id), action)
}

// DeleteDownloadClient removes a download client.
func (c *Client) DeleteDownloadClient(ctx context.Context, id int) error {
	return c.hc.DeleteJSON(ctx, fmt.Sprintf("/api/downloaders/%d", id))
}

// DeleteIndexer removes an indexer.
func (c *Client) DeleteIndexer(ctx context.Context, id int) error {
	return c.hc.DeleteJSON(ctx, fmt.Sprintf("/api/indexer/%d", id))
}

// DeleteIRCNetwork removes an IRC network.
func (c *Client) DeleteIRCNetwork(ctx context.Context, id int) error {
	return c.hc.DeleteJSON(ctx, fmt.Sprintf("/api/irc/network/%d", id))
}

// DeleteFeed removes a feed.
func (c *Client) DeleteFeed(ctx context.Context, id int) error {
	return c.hc.DeleteJSON(ctx, fmt.Sprintf("/api/feeds/%d", id))
}

// DeleteFilter removes a filter and its actions.
func (c *Client) DeleteFilter(ctx context.Context, id int) error {
	return c.hc.DeleteJSON(ctx, fmt.Sprintf("/api/filters/%d", id))
}

// DeleteAction removes a filter action.
func (c *Client) DeleteAction(ctx context.Context, id int) error {
	return c.hc.DeleteJSON(ctx, fmt.Sprintf("/api/actions/%d", id))
}
