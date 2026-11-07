package automation

import (
	"context"
	"errors"
	"fmt"
	"slices"

	automationv1alpha1 "github.com/kyleseneker/media-operator/api/automation/v1alpha1"
)

const actionsKind = "actions"

// autobrrList is one kind the operator creates in Autobrr.
type autobrrList struct {
	kind   string
	list   func(ctx context.Context) ([]map[string]any, error)
	remove func(ctx context.Context, id int) error
}

// lists returns the kinds in the order they are deleted, each before what it
// refers to: filters before the download clients their actions use.
func (s *autobrrSync) lists() []autobrrList {
	return []autobrrList{
		{"filters", s.ac.ListFilters, s.ac.DeleteFilter},
		{"feeds", s.ac.ListFeeds, s.ac.DeleteFeed},
		{"ircNetworks", s.ac.ListIRCNetworks, s.ac.DeleteIRCNetwork},
		{"indexers", s.ac.ListIndexers, s.ac.DeleteIndexer},
		{"downloadClients", s.ac.ListDownloadClients, s.ac.DeleteDownloadClient},
	}
}

func declaredNames(spec *automationv1alpha1.AutobrrConfigSpec) map[string][]string {
	declared := map[string][]string{}
	for _, f := range spec.Filters {
		declared["filters"] = append(declared["filters"], f.Name)
	}
	for _, f := range spec.Feeds {
		declared["feeds"] = append(declared["feeds"], f.Name)
	}
	for _, n := range spec.IRCNetworks {
		declared["ircNetworks"] = append(declared["ircNetworks"], n.Name)
	}
	for _, i := range spec.Indexers {
		declared["indexers"] = append(declared["indexers"], i.Name)
	}
	for _, d := range spec.DownloadClients {
		declared["downloadClients"] = append(declared["downloadClients"], d.Name)
	}
	return declared
}

// pruneUndeclared deletes what the operator created that the spec no longer declares,
// skipping any kind a write failed for this pass.
func (s *autobrrSync) pruneUndeclared(ctx context.Context, spec *automationv1alpha1.AutobrrConfigSpec, failed map[string]bool) []string {
	var errs []string
	if !failed["filters"] {
		for _, f := range spec.Filters {
			if err := s.pruneActions(ctx, f); err != nil {
				errs = append(errs, fmt.Sprintf("actions(%s, prune): %v", f.Name, err))
			}
		}
	}
	declared := declaredNames(spec)
	for _, l := range s.lists() {
		if failed[l.kind] {
			continue
		}
		names, ids, err := listed(ctx, l.list)
		if err == nil {
			err = s.Prune(s.owned, l.kind, declared[l.kind], names, func(name string) error { return l.remove(ctx, ids[name]) })
		}
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s(prune): %v", l.kind, err))
		}
	}
	return errs
}

func (s *autobrrSync) pruneActions(ctx context.Context, filter automationv1alpha1.AutobrrFilter) error {
	filters, ids, err := listed(ctx, s.ac.ListFilters)
	if err != nil || !slices.Contains(filters, filter.Name) {
		return err
	}
	existing, actionIDs, err := s.filterActions(ctx, filter.Name, ids[filter.Name])
	if err != nil {
		return err
	}
	declared := make([]string, 0, len(filter.Actions))
	for _, a := range filter.Actions {
		declared = append(declared, filter.Name+"/"+a.Name)
	}
	return s.Prune(s.owned, actionsKind, declared, existing, func(name string) error {
		return s.ac.DeleteAction(ctx, actionIDs[name])
	})
}

// cleanup deletes everything the operator created, for deletionPolicy delete.
func (s *autobrrSync) cleanup(ctx context.Context) error {
	filters, ids, err := listed(ctx, s.ac.ListFilters)
	if err != nil {
		return err
	}
	var errs []error
	for _, name := range filters {
		existing, actionIDs, err := s.filterActions(ctx, name, ids[name])
		if err == nil {
			err = s.owned.Cleanup(actionsKind, existing, func(n string) error { return s.ac.DeleteAction(ctx, actionIDs[n]) })
		}
		errs = append(errs, err)
	}
	for _, l := range s.lists() {
		names, ids, err := listed(ctx, l.list)
		if err == nil {
			err = s.owned.Cleanup(l.kind, names, func(name string) error { return l.remove(ctx, ids[name]) })
		}
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// filterActions lists a filter's actions as <filter>/<action>.
func (s *autobrrSync) filterActions(ctx context.Context, filter string, filterID int) ([]string, map[string]int, error) {
	return listed(ctx, func(ctx context.Context) ([]map[string]any, error) {
		actions, err := s.ac.ListActions(ctx, filterID)
		for _, a := range actions {
			if name, ok := a["name"].(string); ok {
				a["name"] = filter + "/" + name
			}
		}
		return actions, err
	})
}

// listed returns the names of the items a list call returns and their ids.
func listed(ctx context.Context, list func(context.Context) ([]map[string]any, error)) ([]string, map[string]int, error) {
	items, err := list(ctx)
	if err != nil {
		return nil, nil, err
	}
	names := make([]string, 0, len(items))
	ids := map[string]int{}
	for _, item := range items {
		name, _ := item["name"].(string)
		id, ok := item["id"].(float64)
		if name == "" || !ok {
			continue
		}
		names = append(names, name)
		ids[name] = int(id)
	}
	return names, ids, nil
}
