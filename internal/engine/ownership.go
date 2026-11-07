package engine

import (
	"fmt"
	"slices"

	"github.com/kyleseneker/media-operator/internal/metrics"
)

// Ownership is what an integration created, by resource type, as recorded in
// status.managedResources. Prune and cleanup only ever delete owned items.
type Ownership struct {
	app     string
	managed map[string][]string
	pruned  []PrunedResource
}

// NewOwnership starts from the ownership a resource's status recorded.
func NewOwnership(app string, managed map[string][]string) *Ownership {
	owned := make(map[string][]string, len(managed))
	for kind, names := range managed {
		owned[kind] = slices.Clone(names)
	}
	return &Ownership{app: app, managed: owned}
}

// Created records an item the operator created.
func (o *Ownership) Created(kind, name string) {
	if !slices.Contains(o.managed[kind], name) {
		o.managed[kind] = append(o.managed[kind], name)
	}
}

// Candidates lists the owned items of kind that exist but are no longer
// declared. A kind with nothing declared has none, so leaving a list out of a
// resource never empties it.
func (o *Ownership) Candidates(kind string, declared, existing []string) []string {
	if len(declared) == 0 {
		return nil
	}
	return o.owned(kind, declared, existing)
}

// Prune deletes the candidates of kind.
func (o *Ownership) Prune(kind string, declared, existing []string, remove func(name string) error) error {
	return o.delete(kind, o.Candidates(kind, declared, existing), remove)
}

// Cleanup deletes every owned item of kind that exists, for deletionPolicy delete.
func (o *Ownership) Cleanup(kind string, existing []string, remove func(name string) error) error {
	return o.delete(kind, o.owned(kind, nil, existing), remove)
}

// Managed is the ownership to record in status.
func (o *Ownership) Managed() map[string][]string {
	return o.managed
}

// Pruned lists the items deleted so far.
func (o *Ownership) Pruned() []PrunedResource {
	return o.pruned
}

func (o *Ownership) owned(kind string, declared, existing []string) []string {
	var out []string
	for _, name := range existing {
		if slices.Contains(o.managed[kind], name) && !slices.Contains(declared, name) && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// delete removes the given items, keeping ownership of any it fails to delete so
// a later pass retries them.
func (o *Ownership) delete(kind string, names []string, remove func(name string) error) error {
	if len(names) > DefaultMaxPruneCount {
		return fmt.Errorf("refusing to delete %d %s (threshold is %d); check the resource lists everything it should",
			len(names), kind, DefaultMaxPruneCount)
	}
	for _, name := range names {
		if err := remove(name); err != nil {
			return fmt.Errorf("deleting %s %q: %w", kind, name, err)
		}
		o.managed[kind] = slices.DeleteFunc(o.managed[kind], func(n string) bool { return n == name })
		if len(o.managed[kind]) == 0 {
			delete(o.managed, kind)
		}
		metrics.ResourcesPrunedTotal.WithLabelValues(o.app, kind).Inc()
		o.pruned = append(o.pruned, PrunedResource{Type: kind, Name: name})
	}
	return nil
}
