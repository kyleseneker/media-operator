package export

import (
	"maps"
	"slices"
	"strings"

	autobrrclient "github.com/kyleseneker/media-operator/internal/client/autobrr"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

func exportAutobrr(r *reader) (map[string]any, error) {
	ac := autobrrclient.NewClient(r.hc)
	spec := map[string]any{}

	clients, err := ac.ListDownloadClients(r.ctx)
	if err != nil {
		return nil, err
	}
	clientNames := map[float64]any{}
	var dcs []any
	for _, c := range clients {
		clientNames[num(c["id"])] = c["name"]
		name, _ := c["name"].(string)
		dc := pick(c, "name", "host", "tls")
		dc["enable"] = c["enabled"]
		dc["type"] = crdName(autobrrclient.Types, c["type"])
		if port := num(c["port"]); port != 0 {
			dc["port"] = port
		}
		r.autobrrSecret(dc, "usernameSecretRef", c["username"], "download-client", name, "username")
		r.autobrrSecret(dc, "passwordSecretRef", c["password"], "download-client", name, "password")
		settings, _ := c["settings"].(map[string]any)
		r.autobrrSecret(dc, "apiKeySecretRef", settings["apikey"], "download-client", name, "api-key")
		dcs = append(dcs, dc)
	}
	setList(spec, "downloadClients", dcs)

	indexers, err := ac.ListIndexers(r.ctx)
	if err != nil {
		return nil, err
	}
	var idxs []any
	for _, summary := range indexers {
		i, err := ac.GetIndexer(r.ctx, int(num(summary["id"])))
		if err != nil {
			return nil, err
		}
		idxs = append(idxs, r.autobrrIndexer(i))
	}
	setList(spec, "indexers", idxs)

	networks, err := ac.ListIRCNetworks(r.ctx)
	if err != nil {
		return nil, err
	}
	var ircs []any
	for _, n := range networks {
		name, _ := n["name"].(string)
		irc := pick(n, "name", "server", "port", "tls", "nick")
		irc["enable"] = n["enabled"]
		setText(irc, "inviteCommand", n["invite_command"])
		auth, _ := n["auth"].(map[string]any)
		if mechanism, _ := auth["mechanism"].(string); mechanism != "" {
			irc["authMechanism"] = crdName(autobrrclient.IRCAuthMechanisms, mechanism)
		}
		r.autobrrSecret(irc, "authAccountSecretRef", auth["account"], "irc", name, "account")
		r.autobrrSecret(irc, "authPasswordSecretRef", auth["password"], "irc", name, "password")
		var channels []any
		for _, ch := range list(n["channels"]) {
			channel := pick(ch, "name")
			chName, _ := ch["name"].(string)
			r.autobrrSecret(channel, "passwordSecretRef", ch["password"], "irc", name, chName, "password")
			channels = append(channels, channel)
		}
		if len(channels) > 0 {
			irc["channels"] = channels
		}
		ircs = append(ircs, irc)
	}
	setList(spec, "ircNetworks", ircs)

	feeds, err := ac.ListFeeds(r.ctx)
	if err != nil {
		return nil, err
	}
	var fs []any
	for _, f := range feeds {
		name, _ := f["name"].(string)
		feed := pick(f, "name", "url")
		feed["type"] = crdName(autobrrFeedTypes, f["type"])
		feed["enable"] = f["enabled"]
		if interval := num(f["interval"]); interval != 0 {
			feed["interval"] = interval
		}
		indexer, _ := f["indexer"].(map[string]any)
		setText(feed, "indexerRef", indexer["name"])
		r.autobrrSecret(feed, "apiKeySecretRef", f["api_key"], "feed", name, "api-key")
		fs = append(fs, feed)
	}
	setList(spec, "feeds", fs)

	filters, err := ac.ListFilters(r.ctx)
	if err != nil {
		return nil, err
	}
	var out []any
	for _, summary := range filters {
		id := int(num(summary["id"]))
		full, err := ac.GetFilter(r.ctx, id)
		if err != nil {
			return nil, err
		}
		actions, err := ac.ListActions(r.ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, r.autobrrFilter(full, actions, clientNames))
	}
	setList(spec, "filters", out)
	return spec, nil
}

func (r *reader) autobrrFilter(f map[string]any, actions []map[string]any, clientNames map[float64]any) map[string]any {
	filter := pick(f, "name")
	filter["enable"] = f["enabled"]
	autobrrSetFields(filter, f, autobrrclient.FilterFields)
	for field, key := range map[string]string{"tags": "tags", "exceptTags": "except_tags"} {
		if tags, _ := f[key].(string); tags != "" {
			filter[field] = strings.Split(tags, ",")
		}
	}
	var indexers []string
	for _, i := range list(f["indexers"]) {
		if n, _ := i["name"].(string); n != "" {
			indexers = append(indexers, n)
		}
	}
	if len(indexers) > 0 {
		filter["indexers"] = indexers
	}
	as := make([]any, 0, len(actions))
	for _, a := range actions {
		action := pick(a, "name")
		action["enable"] = a["enabled"]
		action["type"] = crdName(autobrrclient.Types, a["type"])
		if id := num(a["client_id"]); id != 0 {
			action["clientRef"] = clientNames[id]
		}
		autobrrSetFields(action, a, autobrrclient.ActionFields)
		as = append(as, action)
	}
	if len(as) > 0 {
		filter["actions"] = as
	}
	return filter
}

// autobrrSetFields exports the values a table names, leaving out Autobrr's
// empty defaults: an unset criterion is stored as "", false, 0 or [].
func autobrrSetFields(out, item map[string]any, table map[string]string) {
	for field, key := range table {
		switch v := item[key].(type) {
		case string:
			if v != "" {
				out[field] = v
			}
		case bool:
			if v {
				out[field] = v
			}
		case float64:
			if v != 0 {
				out[field] = v
			}
		case []any:
			if len(v) > 0 {
				out[field] = texts(v)
			}
		}
	}
}

var autobrrFeedTypes = map[string]string{"Torznab": "TORZNAB", "Newznab": "NEWZNAB", "RSS": "RSS"}

// autobrrIndexer exports an indexer by its definition. Autobrr names a feed
// indexer's definition after its implementation.
func (r *reader) autobrrIndexer(i map[string]any) map[string]any {
	name, _ := i["name"].(string)
	idx := pick(i, "name")
	idx["enable"] = i["enabled"]
	idx["identifier"] = i["identifier"]
	if implementation, _ := i["implementation"].(string); implementation != autobrrclient.ImplementationIRC {
		idx["identifier"] = implementation
	} else {
		setText(idx, "baseURL", i["base_url"])
	}
	settings, _ := i["settings"].(map[string]any)
	var out []any
	for _, key := range slices.Sorted(maps.Keys(settings)) {
		value, _ := settings[key].(string)
		switch {
		case value == "":
		case reconciler.IsMasked(value):
			out = append(out, map[string]any{"name": key, "valueFrom": r.creds.ref("", "indexer", name, key)})
		default:
			out = append(out, map[string]any{"name": key, "value": value})
		}
	}
	if len(out) > 0 {
		idx["settings"] = out
	}
	return idx
}

func (r *reader) autobrrSecret(out map[string]any, field string, value any, parts ...string) {
	text, _ := value.(string)
	if text == "" {
		return
	}
	if reconciler.IsMasked(text) {
		text = ""
	}
	out[field] = r.creds.ref(text, parts...)
}

func crdName(names map[string]string, value any) any {
	for crd, constant := range names {
		if constant == value {
			return crd
		}
	}
	return value
}

func setText(out map[string]any, field string, value any) {
	if s, _ := value.(string); s != "" {
		out[field] = s
	}
}

func setList(spec map[string]any, key string, items []any) {
	if len(items) > 0 {
		spec[key] = items
	}
}
