package export

import (
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
		r.autobrrSecret(dc, "apiKeySecretRef", c["api_key"], "download-client", name, "api-key")
		dcs = append(dcs, dc)
	}
	setList(spec, "downloadClients", dcs)

	indexers, err := ac.ListIndexers(r.ctx)
	if err != nil {
		return nil, err
	}
	var idxs []any
	for _, i := range indexers {
		name, _ := i["name"].(string)
		idx := pick(i, "name", "implementation")
		idx["enable"] = i["enabled"]
		setText(idx, "baseURL", i["base_url"])
		setText(idx, "feedURL", i["feed_url"])
		r.autobrrSecret(idx, "apiKeySecretRef", i["api_key"], "indexer", name, "api-key")
		idxs = append(idxs, idx)
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
		feed := pick(f, "name", "type", "url")
		feed["enable"] = f["enabled"]
		if interval := num(f["interval"]); interval != 0 {
			feed["interval"] = interval
		}
		setText(feed, "indexerRef", f["indexer"])
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
	name, _ := f["name"].(string)
	filter := pick(f, "name", "priority", "delay")
	filter["enable"] = f["enabled"]
	if v, ok := f["use_regex"]; ok {
		filter["useRegex"] = v
	}
	for field, key := range map[string]string{
		"matchReleases": "match_releases", "exceptReleases": "except_releases", "matchCategories": "match_categories",
		"minSize": "min_size", "maxSize": "max_size",
	} {
		setText(filter, field, f[key])
	}
	for _, field := range []string{"resolutions", "sources", "codecs", "tags", "indexers"} {
		values, ok := f[field].([]any)
		if !ok || len(values) == 0 {
			continue
		}
		if texts := texts(values); len(texts) == len(values) {
			filter[field] = texts
		} else {
			r.warn("filter %q: AutobrrConfig lists %s by name, so they stay as set in Autobrr", name, field)
		}
	}
	as := make([]any, 0, len(actions))
	for _, a := range actions {
		action := pick(a, "name")
		action["enable"] = a["enabled"]
		action["type"] = crdName(autobrrclient.Types, a["type"])
		if id := num(a["client_id"]); id != 0 {
			action["clientRef"] = clientNames[id]
		}
		for field, key := range map[string]string{
			"category": "category", "savePath": "save_path", "webhookURL": "webhook_host",
			"execCommand": "exec_cmd", "execArgs": "exec_args", "watchFolder": "watch_folder",
		} {
			setText(action, field, a[key])
		}
		as = append(as, action)
	}
	if len(as) > 0 {
		filter["actions"] = as
	}
	return filter
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
