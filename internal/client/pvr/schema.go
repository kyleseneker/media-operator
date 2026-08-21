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
		schema := FetchSchema(ctx, client, apiVersion, "downloadclient")
		for _, dc := range opts.DownloadClients {
			check("downloadClient", dc.Name, dc.Implementation, dc.Fields, schema)
		}
	}
	if len(opts.Indexers) > 0 {
		schema := FetchSchema(ctx, client, apiVersion, "indexer")
		for _, idx := range opts.Indexers {
			check("indexer", idx.Name, idx.Implementation, idx.Fields, schema)
		}
	}
	if len(opts.Notifications) > 0 {
		schema := FetchSchema(ctx, client, apiVersion, "notification")
		for _, n := range opts.Notifications {
			check("notification", n.Name, n.Implementation, n.Fields, schema)
		}
	}

	return problems
}
