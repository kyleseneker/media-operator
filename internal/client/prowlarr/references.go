package prowlarr

import (
	"context"
	"fmt"

	indexersv1alpha1 "github.com/kyleseneker/media-operator/api/indexers/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/engine"
)

// ResolveReferences operates on a copy: IDs belong to the current application,
// not to the Kubernetes spec. Missing references must never become empty tags.
func ResolveReferences(ctx context.Context, client *engine.HTTPClient, spec indexersv1alpha1.ProwlarrConfigSpec) (indexersv1alpha1.ProwlarrConfigSpec, map[string]int, error) {
	resolved := spec.DeepCopy()
	tags, err := engine.FetchNamedIDs(ctx, client, "/api/v1/tag", "label")
	if err != nil {
		return *resolved, nil, err
	}
	tagIDs := map[string]int{}
	check := func(resource, name string, labels []string) error {
		for _, label := range labels {
			id, err := tags.Resolve(label)
			if err != nil {
				return fmt.Errorf("%s(%s): tag: %w", resource, name, err)
			}
			tagIDs[label] = id
		}
		return nil
	}
	for _, app := range resolved.Applications {
		if err := check("applications", app.Name, app.Tags); err != nil {
			return *resolved, nil, err
		}
	}
	for _, idx := range resolved.Indexers {
		if err := check("indexers", idx.Name, idx.Tags); err != nil {
			return *resolved, nil, err
		}
	}
	for _, proxy := range resolved.Proxies {
		if err := check("proxies", proxy.Name, proxy.Tags); err != nil {
			return *resolved, nil, err
		}
	}
	for _, dc := range resolved.DownloadClients {
		if err := check("downloadClients", dc.Name, dc.Tags); err != nil {
			return *resolved, nil, err
		}
	}
	for i := range resolved.Notifications {
		n := &resolved.Notifications[i]
		n.Tags, err = tags.ResolveTags(n.TagLabels, n.Tags)
		if err != nil {
			return *resolved, nil, fmt.Errorf("notifications(%s): %w", n.Name, err)
		}
	}
	var profiles engine.NamedIDs
	for i := range resolved.Indexers {
		idx := &resolved.Indexers[i]
		if idx.AppProfileName == "" {
			continue
		}
		if profiles == nil {
			profiles, err = engine.FetchNamedIDs(ctx, client, "/api/v1/appprofile", "name")
			if err != nil {
				return *resolved, nil, err
			}
		}
		id, err := profiles.Resolve(idx.AppProfileName)
		if err != nil {
			return *resolved, nil, fmt.Errorf("indexers(%s): appProfileName: %w", idx.Name, err)
		}
		idx.AppProfileId = &id
	}
	return *resolved, tagIDs, nil
}
