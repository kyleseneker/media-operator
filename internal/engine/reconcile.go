package engine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kyleseneker/media-operator/internal/metrics"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

// logAPIErrorDetail logs the full API response body at debug level if the error is an APIError.
func logAPIErrorDetail(ctx context.Context, err error) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		log.FromContext(ctx).V(1).Info("API error detail", "response", apiErr.DetailedMessage())
	}
}

// DefaultMaxPruneCount is the maximum number of resources that can be pruned
// in a single reconciliation pass per resource type. If more resources would be
// pruned, the operation is refused as a safety measure.
const DefaultMaxPruneCount = 25

// PrunedResource describes a resource that was deleted during pruning.
type PrunedResource struct {
	Type string
	Name string
	ID   int
}

// ReconcileResult holds the outcome of a reconciliation pass.
type ReconcileResult struct {
	Synced  []string
	Errors  []string
	Pruned  []PrunedResource
	Managed map[string][]string
}

// Success returns true if no errors occurred.
func (r *ReconcileResult) Success() bool {
	return len(r.Errors) == 0
}

// Message returns a human-readable summary including what synced and what failed.
func (r *ReconcileResult) Message() string {
	if r.Success() {
		if len(r.Synced) > 0 {
			return fmt.Sprintf("synced: %v", r.Synced)
		}
		return "all configuration sections synced"
	}
	msg := fmt.Sprintf("errors: %v", r.Errors)
	if len(r.Synced) > 0 {
		msg = fmt.Sprintf("synced: %v; %s", r.Synced, msg)
	}
	return msg
}

// ReconcileApp runs the full reconciliation loop for an app.
// `sections` maps setting endpoint names to their desired state (Go structs).
// `resources` maps resource endpoint names to slices of desired resource objects.
// `policy` controls pruning and whether drift is written or only recorded.
func ReconcileApp(ctx context.Context, client *HTTPClient, def AppDefinition, sections map[string]any, resources map[string][]map[string]any, policy SyncPolicy, previouslyManaged map[string][]string) ReconcileResult {
	logger := log.FromContext(ctx)
	result := ReconcileResult{Managed: map[string][]string{}}
	// Retain ownership until a remote deletion succeeds, including omitted
	// sections and failed writes. Never lose the ability to retry cleanup.
	for name, managed := range previouslyManaged {
		result.Managed[name] = slices.Clone(managed)
	}

	// Reconcile settings (singleton config endpoints)
	for _, s := range def.Settings {
		desired, ok := sections[s.Name]
		if !ok || desired == nil || isNilInterface(desired) {
			continue
		}
		if err := reconcileSetting(ctx, client, s.Path, desired, policy.Observe); err != nil {
			logger.Error(err, "failed to reconcile setting", "section", s.Name)
			logAPIErrorDetail(ctx, err)
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", s.Name, err))
		} else {
			result.Synced = append(result.Synced, s.Name)
		}
	}

	// Reconcile resources (list-based endpoints)
	for _, r := range def.Resources {
		items, ok := resources[r.Name]
		if !ok || len(items) == 0 {
			continue
		}
		failed := false
		for _, item := range items {
			created, err := reconcileResourceTracked(ctx, client, r, item, policy.Observe)
			if err != nil {
				failed = true
				matchVal := item[r.MatchField]
				logger.Error(err, "failed to reconcile resource", "type", r.Name, "match", matchVal)
				logAPIErrorDetail(ctx, err)
				result.Errors = append(result.Errors, fmt.Sprintf("%s(%v): %v", r.Name, matchVal, err))
			} else {
				matchVal := item[r.MatchField]
				result.Synced = append(result.Synced, fmt.Sprintf("%s(%v)", r.Name, matchVal))
				if s, ok := matchVal.(string); ok && created && !slices.Contains(result.Managed[r.Name], s) {
					result.Managed[r.Name] = append(result.Managed[r.Name], s)
				}
			}
		}

		// Prune previously owned resources that are absent from the desired list.
		// Only runs when prune is enabled, the resource type is prunable, and at least one
		// desired item was specified (so an omitted section doesn't wipe everything).
		// Any failed apply for this type blocks pruning until a later successful pass.
		if policy.Prune && !policy.Observe && r.Prunable && !failed {
			pruned, err := pruneResources(ctx, client, r, items, previouslyManaged[r.Name])
			result.Pruned = append(result.Pruned, pruned...)
			for _, resource := range pruned {
				result.Managed[r.Name] = slices.DeleteFunc(result.Managed[r.Name], func(name string) bool { return name == resource.Name })
			}
			if err != nil {
				logger.Error(err, "failed to prune resources", "type", r.Name)
				logAPIErrorDetail(ctx, err)
				result.Errors = append(result.Errors, fmt.Sprintf("%s(prune): %v", r.Name, err))
			}
		}
	}

	return result
}

// pruneResources deletes any existing resources not present in the desired list.
// Returns the list of successfully pruned resources for event emission.
// Refuses to prune if the number of candidates exceeds DefaultMaxPruneCount.
func pruneResources(ctx context.Context, client *HTTPClient, endpoint ResourceEndpoint, desired []map[string]any, previouslyManaged []string) ([]PrunedResource, error) {
	if len(previouslyManaged) == 0 {
		return nil, nil
	}
	managedSet := make(map[string]bool, len(previouslyManaged))
	for _, m := range previouslyManaged {
		managedSet[m] = true
	}
	existing, err := client.GetJSONList(ctx, endpoint.Path)
	if err != nil {
		return nil, fmt.Errorf("listing for prune: %w", err)
	}

	// Build set of desired match values
	desiredSet := make(map[string]bool, len(desired))
	for _, d := range desired {
		if v, ok := d[endpoint.MatchField].(string); ok {
			desiredSet[v] = true
		}
	}

	// Collect candidates before deleting anything
	type pruneCandidate struct {
		name string
		id   int
	}
	var candidates []pruneCandidate
	for _, e := range existing {
		matchVal, _ := e[endpoint.MatchField].(string)
		if matchVal == "" {
			continue
		}
		if desiredSet[matchVal] {
			continue
		}
		if !managedSet[matchVal] {
			continue
		}
		id, ok := e["id"].(float64)
		if !ok {
			continue
		}
		candidates = append(candidates, pruneCandidate{name: matchVal, id: int(id)})
	}

	// Safety threshold: refuse to prune if too many resources would be deleted
	if len(candidates) > DefaultMaxPruneCount {
		return nil, fmt.Errorf(
			"refusing to prune %d %s resources (threshold is %d) — verify your desired state is complete",
			len(candidates), endpoint.Name, DefaultMaxPruneCount,
		)
	}

	logger := log.FromContext(ctx)
	var pruned []PrunedResource
	for _, c := range candidates {
		logger.Info("pruning unmanaged resource", "type", endpoint.Name, "match", c.name, "id", c.id)
		if err := client.DeleteJSON(ctx, fmt.Sprintf("%s/%d", endpoint.Path, c.id)); err != nil {
			return pruned, fmt.Errorf("deleting %s %q (id=%d): %w", endpoint.Name, c.name, c.id, err)
		}
		metrics.ResourcesPrunedTotal.WithLabelValues(client.AppLabel(), endpoint.Name).Inc()
		pruned = append(pruned, PrunedResource{Type: endpoint.Name, Name: c.name, ID: c.id})
	}
	return pruned, nil
}

// reconcileSetting handles a singleton config endpoint: GET, merge, PUT.
func reconcileSetting(ctx context.Context, client *HTTPClient, path string, desired any, observe bool) error {
	current, err := client.GetJSON(ctx, path)
	if err != nil {
		return fmt.Errorf("getting current: %w", err)
	}

	id, ok := current["id"].(float64)
	if !ok {
		return fmt.Errorf("config at %s has no id", path)
	}

	out, err := reconciler.MergeDesired(current, desired)
	if err != nil {
		return fmt.Errorf("merging: %w", err)
	}
	key := client.secretKey("setting", path)
	if !out.Changed && !reconciler.SecretsChangedSince(key, out.SecretDigest) {
		return nil
	}

	if observe {
		metrics.DriftObservedTotal.WithLabelValues(client.AppLabel(), "setting", path).Inc()
		return nil
	}
	metrics.DriftCorrectedTotal.WithLabelValues(client.AppLabel(), "setting", path).Inc()
	if err := client.PutJSON(ctx, fmt.Sprintf("%s/%d", path, int(id)), out.Merged); err != nil {
		return err
	}
	reconciler.RecordSecrets(key, out.SecretDigest)
	return nil
}

// reconcileResource handles a list-based resource endpoint.
func reconcileResource(ctx context.Context, client *HTTPClient, endpoint ResourceEndpoint, desired map[string]any, observe bool) error {
	_, err := reconcileResourceTracked(ctx, client, endpoint, desired, observe)
	return err
}

// reconcileResourceTracked reports creation separately from updates and adoption.
func reconcileResourceTracked(ctx context.Context, client *HTTPClient, endpoint ResourceEndpoint, desired map[string]any, observe bool) (bool, error) {
	existing, err := client.GetJSONList(ctx, endpoint.Path)
	if err != nil {
		return false, fmt.Errorf("listing: %w", err)
	}

	desiredMatch, _ := desired[endpoint.MatchField].(string)

	for _, e := range existing {
		existingMatch, _ := e[endpoint.MatchField].(string)
		if existingMatch == desiredMatch {
			// Found existing resource
			switch endpoint.Policy {
			case CreateOnly:
				return false, nil // exists, don't update
			case CreateOrUpdate, UpdateAlways:
				id, ok := e["id"].(float64)
				if !ok {
					return false, fmt.Errorf("resource %q has no id", desiredMatch)
				}
				out, err := reconciler.MergeDesired(e, desired)
				if err != nil {
					return false, err
				}
				key := client.secretKey(endpoint.Name, desiredMatch)
				if !out.Changed && !reconciler.SecretsChangedSince(key, out.SecretDigest) {
					return false, nil
				}
				if observe {
					metrics.DriftObservedTotal.WithLabelValues(client.AppLabel(), endpoint.Name, desiredMatch).Inc()
					return false, nil
				}
				metrics.DriftCorrectedTotal.WithLabelValues(client.AppLabel(), endpoint.Name, desiredMatch).Inc()
				if err := client.PutJSON(ctx, fmt.Sprintf("%s/%d", endpoint.Path, int(id)), out.Merged); err != nil {
					return false, err
				}
				reconciler.RecordSecrets(key, out.SecretDigest)
				return false, nil
			}
		}
	}

	// Not found — create
	if observe {
		metrics.DriftObservedTotal.WithLabelValues(client.AppLabel(), endpoint.Name, desiredMatch).Inc()
		return false, nil
	}
	err = client.PostJSON(ctx, endpoint.Path, desired)
	return err == nil, err
}

// secretKey isolates last-written secrets by target and Kubernetes owner. Quoted
// components keep arbitrary resource names from colliding with separators.
func (c *HTTPClient) secretKey(resourceType, name string) string {
	return fmt.Sprintf("%q/%q/%q/%q/%q", c.baseURL, c.owner, c.AppLabel(), resourceType, name)
}

func isNilInterface(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Ptr && rv.IsNil()
}

// ObserveDifference compares only desired fields, preserving the same partial
// ownership and masked-value semantics as enforcement. A nil current object
// means the resource does not exist and therefore requires creation.
func ObserveDifference(app, resourceType, name string, current map[string]any, desired any) (bool, error) {
	out, err := reconciler.MergeDesired(current, desired)
	if err != nil {
		return false, err
	}
	drift := current == nil || out.Changed
	if drift {
		metrics.DriftObservedTotal.WithLabelValues(app, resourceType, name).Inc()
	}
	return drift, nil
}
