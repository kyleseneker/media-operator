package curation

import (
	"context"
	"errors"
	"fmt"

	curationv1alpha1 "github.com/kyleseneker/media-operator/api/curation/v1alpha1"
	maintainerrclient "github.com/kyleseneker/media-operator/internal/client/maintainerr"
)

const ruleGroupsKind = "ruleGroups"

type maintainerrList struct {
	kind      string
	nameField string
	list      func(ctx context.Context) ([]map[string]any, error)
	remove    func(ctx context.Context, id int) error
}

// lists returns the kinds in the order they are deleted: Maintainerr refuses to
// delete a server while a rule group's collection uses it, and rule groups
// refer to notifications.
func (s *maintainerrSync) lists() []maintainerrList {
	return []maintainerrList{
		{ruleGroupsKind, "name", s.mc.ListRuleGroups, s.mc.DeleteRuleGroup},
		{notificationsKind, "name", s.mc.ListNotifications, s.mc.DeleteNotification},
		s.servers(maintainerrclient.Sonarr),
		s.servers(maintainerrclient.Radarr),
	}
}

func (s *maintainerrSync) servers(kind maintainerrclient.Arr) maintainerrList {
	return maintainerrList{
		kind:      string(kind),
		nameField: "serverName",
		list:      func(ctx context.Context) ([]map[string]any, error) { return s.mc.ListArr(ctx, kind) },
		remove:    func(ctx context.Context, id int) error { return s.mc.DeleteArr(ctx, kind, id) },
	}
}

func maintainerrDeclared(spec *curationv1alpha1.MaintainerrConfigSpec) map[string][]string {
	declared := map[string][]string{}
	for _, g := range spec.RuleGroups {
		declared[ruleGroupsKind] = append(declared[ruleGroupsKind], g.Name)
	}
	for _, n := range spec.Notifications {
		declared[notificationsKind] = append(declared[notificationsKind], n.Name)
	}
	for _, srv := range spec.Sonarr {
		declared[string(maintainerrclient.Sonarr)] = append(declared[string(maintainerrclient.Sonarr)], srv.Name)
	}
	for _, srv := range spec.Radarr {
		declared[string(maintainerrclient.Radarr)] = append(declared[string(maintainerrclient.Radarr)], srv.Name)
	}
	return declared
}

// pruneUndeclared deletes what the operator created that the spec no longer
// declares, skipping any kind a write failed for this pass.
func (s *maintainerrSync) pruneUndeclared(
	ctx context.Context, spec *curationv1alpha1.MaintainerrConfigSpec, failed map[string]bool,
) []string {
	declared := maintainerrDeclared(spec)
	var errs []string
	for _, l := range s.lists() {
		if failed[l.kind] || len(declared[l.kind]) == 0 {
			continue
		}
		names, ids, err := l.listed(ctx)
		if err == nil {
			err = s.Prune(s.owned, l.kind, declared[l.kind], names, func(name string) error { return l.remove(ctx, ids[name]) })
		}
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s(prune): %v", l.kind, err))
		}
	}
	return errs
}

// cleanup deletes everything the operator created, for deletionPolicy delete.
func (s *maintainerrSync) cleanup(ctx context.Context) error {
	lists := s.lists()
	errs := make([]error, 0, len(lists))
	for _, l := range lists {
		names, ids, err := l.listed(ctx)
		if err == nil {
			err = s.owned.Cleanup(l.kind, names, func(name string) error { return l.remove(ctx, ids[name]) })
		}
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (l maintainerrList) listed(ctx context.Context) ([]string, map[string]int, error) {
	items, err := l.list(ctx)
	if err != nil {
		return nil, nil, err
	}
	names := make([]string, 0, len(items))
	ids := map[string]int{}
	for _, item := range items {
		name, _ := item[l.nameField].(string)
		id, ok := item["id"].(float64)
		if name == "" || !ok {
			continue
		}
		names = append(names, name)
		ids[name] = int(id)
	}
	return names, ids, nil
}
