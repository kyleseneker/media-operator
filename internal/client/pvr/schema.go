package pvr

import (
	"context"
	"fmt"
	"sort"
	"strings"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/engine"
)

// SchemaFieldNames returns the field names an implementation accepts, taken from
// the app's own /schema listing. The second return is false when the schema does
// not describe the implementation, in which case callers must not validate.
func SchemaFieldNames(schema []map[string]any, implementation string) (map[string]bool, bool) {
	for _, def := range schema {
		impl, _ := def["implementation"].(string)
		if !strings.EqualFold(impl, implementation) {
			continue
		}
		raw, ok := def["fields"].([]any)
		if !ok {
			return nil, false
		}
		names := make(map[string]bool, len(raw))
		for _, f := range raw {
			fm, ok := f.(map[string]any)
			if !ok {
				continue
			}
			if n, ok := fm["name"].(string); ok {
				names[n] = true
			}
		}
		return names, len(names) > 0
	}
	return nil, false
}

// SchemaConfigContract keeps a declared contract, otherwise takes the one the
// app's schema lists for the implementation. Contracts follow no single naming
// rule (Sonarr's CustomImport takes CustomSettings), so a guessed name fails the
// write. An empty result leaves the payload builders' guess in place.
func SchemaConfigContract(schema []map[string]any, implementation, declared string) string {
	if declared != "" {
		return declared
	}
	for _, def := range schema {
		impl, _ := def["implementation"].(string)
		if !strings.EqualFold(impl, implementation) {
			continue
		}
		contract, _ := def["configContract"].(string)
		return contract
	}
	return ""
}

// SchemaProtocol returns the protocol the app's schema gives an implementation.
// Each implementation has exactly one, and the apps spell it differently
// (Lidarr: TorrentDownloadProtocol, the others: torrent), so the declared value
// is used only when the schema cannot be read.
func SchemaProtocol(schema []map[string]any, implementation, declared string) string {
	if def := schemaDefinition(schema, implementation); def != nil {
		if protocol, _ := def["protocol"].(string); protocol != "" {
			return protocol
		}
	}
	return declared
}

// UnsupportedEvents lists the event switches in a notification payload that the
// implementation's schema lacks. The app drops them, so they would read back
// missing and be rewritten on every pass.
func UnsupportedEvents(schema []map[string]any, implementation string, payload map[string]any) []string {
	def := schemaDefinition(schema, implementation)
	if def == nil {
		return nil
	}
	var unsupported []string
	for key := range payload {
		if !strings.HasPrefix(key, "on") && !strings.HasPrefix(key, "include") {
			continue
		}
		if _, ok := def[key]; !ok {
			unsupported = append(unsupported, key)
		}
	}
	sort.Strings(unsupported)
	return unsupported
}

func schemaDefinition(schema []map[string]any, implementation string) map[string]any {
	for _, def := range schema {
		if impl, _ := def["implementation"].(string); strings.EqualFold(impl, implementation) {
			return def
		}
	}
	return nil
}

// ValidateFieldNames reports field names the implementation does not accept. These
// apps store an unknown field and answer 2xx, so an unvalidated typo is inert
// rather than an error.
func ValidateFieldNames(schema []map[string]any, implementation string, fieldNames []string) error {
	valid, ok := SchemaFieldNames(schema, implementation)
	if !ok {
		return nil
	}

	var unknown []string
	for _, n := range fieldNames {
		if !valid[n] {
			unknown = append(unknown, n)
		}
	}
	if len(unknown) == 0 {
		return nil
	}

	accepted := make([]string, 0, len(valid))
	for n := range valid {
		accepted = append(accepted, n)
	}
	sort.Strings(accepted)
	sort.Strings(unknown)

	return fmt.Errorf("%s does not accept field(s) %s; it accepts: %s",
		implementation, strings.Join(unknown, ", "), strings.Join(accepted, ", "))
}

// FetchSchema reads the schema listing for a kind. A schema that cannot be read
// yields no definitions, so validation is skipped rather than blocking a write.
func FetchSchema(ctx context.Context, client *engine.HTTPClient, apiVersion, kind string) []map[string]any {
	schema, err := client.GetJSONList(ctx, fmt.Sprintf("/api/%s/%s/schema", apiVersion, kind))
	if err != nil {
		return nil
	}
	return schema
}

// ValidateOptionFields checks each resource's free-form fields against the app's
// own schema. Failures are returned as messages rather than an error, so one bad
// field does not abandon the rest of the reconcile.
func ValidateOptionFields(ctx context.Context, client *engine.HTTPClient, apiVersion string, opts Options) []string {
	var problems []string

	check := func(kind, name, implementation string, fields []commonv1alpha1.ConfigField, schema []map[string]any) {
		if len(fields) == 0 {
			return
		}
		names := make([]string, 0, len(fields))
		for _, f := range fields {
			names = append(names, f.Name)
		}
		if err := ValidateFieldNames(schema, implementation, names); err != nil {
			problems = append(problems, fmt.Sprintf("%s(%s): %v", kind, name, err))
		}
	}

	if len(opts.DownloadClients) > 0 {
		schema := opts.schema(ctx, client, apiVersion, "downloadclient")
		for _, dc := range opts.DownloadClients {
			check("downloadClient", dc.Name, dc.Implementation, dc.Fields, schema)
		}
	}
	if len(opts.Indexers) > 0 {
		schema := opts.schema(ctx, client, apiVersion, "indexer")
		for _, idx := range opts.Indexers {
			check("indexer", idx.Name, idx.Implementation, idx.Fields, schema)
		}
	}
	if len(opts.Notifications) > 0 {
		schema := opts.schema(ctx, client, apiVersion, "notification")
		for _, n := range opts.Notifications {
			check("notification", n.Name, n.Implementation, n.Fields, schema)
			if events := UnsupportedEvents(schema, n.Implementation, BuildNotificationPayload(n)); len(events) > 0 {
				problems = append(problems, fmt.Sprintf("notification(%s): %s has no %s event; it was not sent",
					n.Name, n.Implementation, strings.Join(events, ", ")))
			}
		}
	}
	if len(opts.ImportLists) > 0 {
		schema := opts.schema(ctx, client, apiVersion, "importlist")
		for _, il := range opts.ImportLists {
			check("importList", il.Name, il.Implementation, il.Fields, schema)
		}
	}

	return problems
}

// withSchemas reads each provider kind's schema once, so validation and every
// reconcile stage share it.
func withSchemas(ctx context.Context, client *engine.HTTPClient, apiVersion string, opts Options) Options {
	opts.schemas = map[string][]map[string]any{}
	for kind, used := range map[string]bool{
		"downloadclient": len(opts.DownloadClients) > 0,
		"indexer":        len(opts.Indexers) > 0,
		"notification":   len(opts.Notifications) > 0,
		"importlist":     len(opts.ImportLists) > 0,
	} {
		if used {
			opts.schemas[kind] = FetchSchema(ctx, client, apiVersion, kind)
		}
	}
	return opts
}
