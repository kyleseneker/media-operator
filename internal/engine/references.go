package engine

import (
	"context"
	"fmt"
	"math"
	"slices"
)

// NamedIDs maps a name to every ID an app assigned it. Duplicates are retained
// so a reference can never silently choose one of multiple resources.
// Unreferenced duplicates do not block partial management.
type NamedIDs map[string][]int

// FetchNamedIDs lists path and indexes each item's ID by the given name field.
func FetchNamedIDs(ctx context.Context, client *HTTPClient, path, field string) (NamedIDs, error) {
	items, err := client.GetJSONList(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("reading references from %s: %w", path, err)
	}
	ids := NamedIDs{}
	for _, item := range items {
		name, _ := item[field].(string)
		id, _ := item["id"].(float64)
		if id <= 0 || id != math.Trunc(id) || id > math.MaxInt32 {
			id = 0
		}
		ids[name] = append(ids[name], int(id))
	}
	return ids, nil
}

// Resolve returns the ID of the single resource with the given name.
func (ids NamedIDs) Resolve(name string) (int, error) {
	values := ids[name]
	if len(values) == 0 {
		return 0, fmt.Errorf("reference %q does not exist", name)
	}
	if len(values) != 1 {
		return 0, fmt.Errorf("reference %q is ambiguous (%d matches)", name, len(values))
	}
	if values[0] <= 0 {
		return 0, fmt.Errorf("reference %q has an invalid ID", name)
	}
	return values[0], nil
}

// ResolveTags resolves tag labels to IDs. Without labels, the legacy numeric
// IDs are returned unchanged.
func (ids NamedIDs) ResolveTags(labels []string, legacy []int) ([]int, error) {
	if len(labels) == 0 {
		return legacy, nil
	}
	out := make([]int, 0, len(labels))
	for _, label := range labels {
		id, err := ids.Resolve(label)
		if err != nil {
			return nil, fmt.Errorf("tag: %w", err)
		}
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out, nil
}
