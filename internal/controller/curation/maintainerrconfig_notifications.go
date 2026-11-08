package curation

import (
	"context"
	"fmt"
	"maps"
	"slices"

	curationv1alpha1 "github.com/kyleseneker/media-operator/api/curation/v1alpha1"
	maintainerrclient "github.com/kyleseneker/media-operator/internal/client/maintainerr"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

const notificationsKind = "notifications"

// defaultAboutScale is the lead time Maintainerr's UI gives a new notification.
const defaultAboutScale = 3

// notifications saves each declared notification and returns every
// notification Maintainerr has by name, for the rule groups that name them.
func (s *maintainerrSync) notifications(ctx context.Context, declared []curationv1alpha1.MaintainerrNotification) (map[string]int, []string) {
	stored, err := s.mc.ListNotifications(ctx)
	if err != nil {
		return nil, []string{fmt.Sprintf("notifications: listing: %v", err)}
	}
	var errs []string
	for i := range declared {
		if err := s.notification(ctx, &declared[i], stored); err != nil {
			errs = append(errs, fmt.Sprintf("notification(%s): %v", declared[i].Name, err))
		}
	}
	if len(declared) > 0 && !s.Observe {
		if stored, err = s.mc.ListNotifications(ctx); err != nil {
			return nil, append(errs, fmt.Sprintf("notifications: listing: %v", err))
		}
	}
	ids := make(map[string]int, len(stored))
	for _, n := range stored {
		name, _ := n["name"].(string)
		if id, ok := n["id"].(float64); ok && name != "" {
			ids[name] = int(id)
		}
	}
	return ids, errs
}

func (s *maintainerrSync) notification(ctx context.Context, n *curationv1alpha1.MaintainerrNotification, stored []map[string]any) error {
	secrets := make(map[string]string, len(n.SecretOptions))
	for name, ref := range n.SecretOptions {
		value, err := reconciler.ResolveSecretKeyRef(ctx, s.c, s.namespace, ref)
		if err != nil {
			return fmt.Errorf("resolving %s: %w", name, err)
		}
		secrets[name] = value
	}

	var current map[string]any
	for _, e := range stored {
		if e["name"] != n.Name {
			continue
		}
		if current != nil {
			return fmt.Errorf("maintainerr has more than one notification named %q", n.Name)
		}
		current = e
	}
	body, err := notificationBody(n, secrets, current)
	if err != nil {
		return err
	}
	if current == nil {
		return s.Apply("notification", n.Name, s.created(notificationsKind, n.Name, func() error { return s.mc.SaveNotification(ctx, body) }))
	}
	if reconciler.Covers(current, mustJSON(body)) {
		return nil
	}
	body["id"] = current["id"]
	return s.Apply("notification", n.Name, func() error { return s.mc.SaveNotification(ctx, body) })
}

// notificationBody is the whole notification Maintainerr saves, since a save
// replaces every field: what the spec leaves out comes from Maintainerr's copy,
// or a new notification's defaults.
func notificationBody(n *curationv1alpha1.MaintainerrNotification, secrets map[string]string, current map[string]any) (map[string]any, error) {
	body := map[string]any{
		"name": n.Name, "agent": n.Agent, "enabled": true, "types": []any{}, "aboutScale": defaultAboutScale,
	}
	options := map[string]any{}
	if current != nil {
		for _, key := range []string{"enabled", "types", "aboutScale"} {
			if v, ok := current[key]; ok && v != nil {
				body[key] = v
			}
		}
		if stored, ok := current["options"].(map[string]any); ok {
			maps.Copy(options, stored)
		}
	}
	if n.Enabled != nil {
		body["enabled"] = *n.Enabled
	}
	if n.AboutScale != nil {
		body["aboutScale"] = *n.AboutScale
	}
	if n.Types != nil {
		types := make([]int, 0, len(n.Types))
		for _, name := range n.Types {
			t, ok := maintainerrclient.NotificationTypes[name]
			if !ok {
				return nil, fmt.Errorf("unknown notification type %q", name)
			}
			types = append(types, t)
		}
		slices.Sort(types)
		body["types"] = types
	}
	for key, value := range n.Options {
		options[key] = value.ToInterface()
	}
	for key, value := range secrets {
		options[key] = value
	}
	body["options"] = options
	return body, nil
}

// notificationIDs resolves the notifications a rule group names.
func notificationIDs(names []string, ids map[string]int) ([]any, error) {
	resolved := make([]int, 0, len(names))
	for _, name := range names {
		id, ok := ids[name]
		if !ok {
			return nil, fmt.Errorf("maintainerr has no notification named %q", name)
		}
		resolved = append(resolved, id)
	}
	slices.Sort(resolved)
	out := make([]any, 0, len(resolved))
	for _, id := range slices.Compact(resolved) {
		out = append(out, map[string]any{"id": id})
	}
	return out, nil
}
