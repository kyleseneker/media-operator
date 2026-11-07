package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kyleseneker/media-operator/internal/metrics"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

// logAPIErrorDetail logs the full API response body at debug level if the error is an APIError.
func logAPIErrorDetail(ctx context.Context, err error) {
	if apiErr, ok := errors.AsType[*APIError](err); ok {
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
	Synced []string
	Errors []string
	// Drift lists what observe mode found differing and left alone.
	Drift   []string
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
		drift, err := reconcileSetting(ctx, client, s.Path, desired, policy.Observe)
		switch {
		case err != nil:
			logger.Error(err, "failed to reconcile setting", "section", s.Name)
			logAPIErrorDetail(ctx, err)
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", s.Name, err))
		case drift:
			result.Drift = append(result.Drift, s.Name)
		default:
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
			created, drift, err := reconcileResourceTracked(ctx, client, r, item, policy.Observe)
			if drift {
				result.Drift = append(result.Drift, fmt.Sprintf("%s(%v)", r.Name, r.resourceKey(item)))
				continue
			}
			if err != nil {
				failed = true
				matchVal := r.resourceKey(item)
				logger.Error(err, "failed to reconcile resource", "type", r.Name, "match", matchVal)
				logAPIErrorDetail(ctx, err)
				result.Errors = append(result.Errors, fmt.Sprintf("%s(%v): %v", r.Name, matchVal, err))
			} else {
				matchVal := r.resourceKey(item)
				result.Synced = append(result.Synced, fmt.Sprintf("%s(%v)", r.Name, matchVal))
				if created && matchVal != "" && !slices.Contains(result.Managed[r.Name], matchVal) {
					result.Managed[r.Name] = append(result.Managed[r.Name], matchVal)
				}
			}
		}

		// Prune previously owned resources that are absent from the desired list.
		// Only runs when prune is enabled, the resource type is prunable, and at least one
		// desired item was specified (so an omitted section doesn't wipe everything).
		// Any failed apply for this type blocks pruning until a later successful pass.
		if policy.Prune && policy.Observe && r.Prunable {
			candidates, err := pruneCandidates(ctx, client, r, items, previouslyManaged[r.Name])
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s(prune): %v", r.Name, err))
			}
			for _, c := range candidates {
				metrics.DriftObservedTotal.WithLabelValues(client.AppLabel(), r.Name, c.Name).Inc()
				result.Drift = append(result.Drift, fmt.Sprintf("%s(%s)", r.Name, c.Name))
			}
		}
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

// pruneResources deletes the resources pruneCandidates finds.
func pruneResources(ctx context.Context, client *HTTPClient, endpoint ResourceEndpoint, desired []map[string]any, previouslyManaged []string) ([]PrunedResource, error) {
	candidates, err := pruneCandidates(ctx, client, endpoint, desired, previouslyManaged)
	if err != nil {
		return nil, err
	}
	logger := log.FromContext(ctx)
	var pruned []PrunedResource
	for _, c := range candidates {
		logger.Info("pruning unmanaged resource", "type", endpoint.Name, "match", c.Name, "id", c.ID)
		if err := client.DeleteJSON(ctx, fmt.Sprintf("%s/%d", endpoint.Path, c.ID)); err != nil {
			if reason, refused := Refusal(err); refused {
				return pruned, fmt.Errorf("deleting %s %q (id=%d): the app refused: %s", endpoint.Name, c.Name, c.ID, reason)
			}
			return pruned, fmt.Errorf("deleting %s %q (id=%d): %w", endpoint.Name, c.Name, c.ID, err)
		}
		metrics.ResourcesPrunedTotal.WithLabelValues(client.AppLabel(), endpoint.Name).Inc()
		pruned = append(pruned, c)
	}
	return pruned, nil
}

// pruneCandidates lists the existing resources the operator created that the
// desired list no longer has. It refuses past DefaultMaxPruneCount.
func pruneCandidates(ctx context.Context, client *HTTPClient, endpoint ResourceEndpoint, desired []map[string]any, previouslyManaged []string) ([]PrunedResource, error) {
	if len(previouslyManaged) == 0 {
		return nil, nil
	}
	existing, err := client.GetJSONList(ctx, endpoint.Path)
	if err != nil {
		return nil, fmt.Errorf("listing for prune: %w", err)
	}
	desiredSet := make(map[string]bool, len(desired))
	for _, d := range desired {
		if v := endpoint.resourceKey(d); v != "" {
			desiredSet[v] = true
		}
	}
	var candidates []PrunedResource
	for _, e := range existing {
		matchVal := endpoint.resourceKey(e)
		if matchVal == "" || desiredSet[matchVal] || !slices.Contains(previouslyManaged, matchVal) {
			continue
		}
		id, ok := e["id"].(float64)
		if !ok {
			continue
		}
		candidates = append(candidates, PrunedResource{Type: endpoint.Name, Name: matchVal, ID: int(id)})
	}
	if len(candidates) > DefaultMaxPruneCount {
		return nil, fmt.Errorf(
			"refusing to prune %d %s resources (threshold is %d) — verify your desired state is complete",
			len(candidates), endpoint.Name, DefaultMaxPruneCount,
		)
	}
	return candidates, nil
}

// reconcileSetting handles a singleton config endpoint: GET, merge, PUT. In
// observe mode it reports a difference instead of writing it.
func reconcileSetting(ctx context.Context, client *HTTPClient, path string, desired any, observe bool) (bool, error) {
	current, err := client.GetJSON(ctx, path)
	if err != nil {
		return false, fmt.Errorf("getting current: %w", err)
	}

	id, ok := current["id"].(float64)
	if !ok {
		return false, fmt.Errorf("config at %s has no id", path)
	}

	out, err := reconciler.MergeDesired(current, desired)
	if err != nil {
		return false, fmt.Errorf("merging: %w", err)
	}
	key := client.secretKey("setting", path)
	if !out.Changed && !reconciler.SecretsDiffer(key, out.SecretDigest, observe) {
		return false, nil
	}

	if observe {
		metrics.DriftObservedTotal.WithLabelValues(client.AppLabel(), "setting", path).Inc()
		return true, nil
	}
	metrics.DriftCorrectedTotal.WithLabelValues(client.AppLabel(), "setting", path).Inc()
	if err := client.PutJSON(ctx, fmt.Sprintf("%s/%d", path, int(id)), out.Merged); err != nil {
		return false, err
	}
	reconciler.RecordSecrets(key, out.SecretDigest)
	return false, nil
}

// reconcileResource handles a list-based resource endpoint.
func reconcileResource(ctx context.Context, client *HTTPClient, endpoint ResourceEndpoint, desired map[string]any, observe bool) error {
	_, _, err := reconcileResourceTracked(ctx, client, endpoint, desired, observe)
	return err
}

// reconcileResourceTracked reports creation separately from updates and
// adoption, and in observe mode reports a difference instead of writing it.
func reconcileResourceTracked(
	ctx context.Context, client *HTTPClient, endpoint ResourceEndpoint, desired map[string]any, observe bool,
) (created, drift bool, err error) {
	existing, err := client.GetJSONList(ctx, endpoint.Path)
	if err != nil {
		return false, false, fmt.Errorf("listing: %w", err)
	}

	desiredMatch := endpoint.resourceKey(desired)
	if desiredMatch == "" {
		return false, false, fmt.Errorf("resource has no identity")
	}

	for _, e := range existing {
		existingMatch := endpoint.resourceKey(e)
		if existingMatch == desiredMatch {
			// Found existing resource
			switch endpoint.Policy {
			case CreateOnly:
				return false, false, nil // exists, don't update
			case CreateOrUpdate, UpdateAlways:
				id, ok := e["id"].(float64)
				if !ok {
					return false, false, fmt.Errorf("resource %q has no id", desiredMatch)
				}
				out, err := reconciler.MergeDesired(e, desired)
				if err != nil {
					return false, false, err
				}
				key := client.secretKey(endpoint.Name, desiredMatch)
				if !out.Changed && !reconciler.SecretsDiffer(key, out.SecretDigest, observe) {
					return false, false, nil
				}
				if observe {
					metrics.DriftObservedTotal.WithLabelValues(client.AppLabel(), endpoint.Name, desiredMatch).Inc()
					return false, true, nil
				}
				metrics.DriftCorrectedTotal.WithLabelValues(client.AppLabel(), endpoint.Name, desiredMatch).Inc()
				if err := client.PutJSON(ctx, fmt.Sprintf("%s/%d", endpoint.Path, int(id)), out.Merged); err != nil {
					return false, false, err
				}
				reconciler.RecordSecrets(key, out.SecretDigest)
				return false, false, nil
			}
		}
	}

	// Not found — create
	if observe {
		metrics.DriftObservedTotal.WithLabelValues(client.AppLabel(), endpoint.Name, desiredMatch).Inc()
		return false, true, nil
	}
	body, err := client.Do(ctx, http.MethodPost, endpoint.Path, desired)
	if err != nil {
		return false, false, err
	}
	// The created resource comes back with its secrets masked, like any later
	// read. Remember what was written now, or the next pass rewrites it once.
	var stored map[string]any
	if json.Unmarshal(body, &stored) == nil {
		if out, err := reconciler.MergeDesired(stored, desired); err == nil {
			reconciler.RecordSecrets(client.secretKey(endpoint.Name, desiredMatch), out.SecretDigest)
		}
	}
	return true, false, nil
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
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

// CorrectDifference is ObserveDifference for integrations that write through
// their own client: it reports whether the caller must write, and counts the
// correction so a write that never sticks shows up in drift_corrected_total.
func CorrectDifference(app, resourceType, name string, current map[string]any, desired any) (bool, error) {
	out, err := reconciler.MergeDesired(current, desired)
	if err != nil {
		return false, err
	}
	drift := current == nil || out.Changed
	if drift {
		metrics.DriftCorrectedTotal.WithLabelValues(app, resourceType, name).Inc()
	}
	return drift, nil
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

// KeptResource is an owned resource the app refused to delete.
type KeptResource struct {
	PrunedResource
	Reason string
}

// DeleteManagedResources removes only tracked, prunable resources. Reverse
// declaration order removes dependents (for example profiles) before resources
// they reference (custom formats). Settings, root folders and tags are retained.
// A resource the app refuses to delete, such as a quality profile a root folder
// still uses, is kept and returned; any other failure stops the cleanup so it
// can be retried.
func DeleteManagedResources(
	ctx context.Context, client *HTTPClient, def AppDefinition, managed map[string][]string,
) ([]PrunedResource, []KeptResource, error) {
	var deleted []PrunedResource
	var kept []KeptResource
	for _, endpoint := range slices.Backward(def.Resources) {
		if !endpoint.Prunable || len(managed[endpoint.Name]) == 0 {
			continue
		}
		candidates, err := pruneCandidates(ctx, client, endpoint, nil, managed[endpoint.Name])
		if err != nil {
			return deleted, kept, fmt.Errorf("cleaning up %s: %w", endpoint.Name, err)
		}
		for _, c := range candidates {
			err := client.DeleteJSON(ctx, fmt.Sprintf("%s/%d", endpoint.Path, c.ID))
			if reason, refused := Refusal(err); refused {
				kept = append(kept, KeptResource{PrunedResource: c, Reason: reason})
				continue
			}
			if err != nil && !isNotFound(err) {
				return deleted, kept, fmt.Errorf("cleaning up %s %q (id=%d): %w", endpoint.Name, c.Name, c.ID, err)
			}
			metrics.ResourcesPrunedTotal.WithLabelValues(client.AppLabel(), endpoint.Name).Inc()
			deleted = append(deleted, c)
		}
	}
	return deleted, kept, nil
}

// Refusal reports whether err is the app declining a request it understood,
// with the reason it gave. Authentication failures, rate limits and timeouts
// are not refusals; they may succeed on a retry.
func Refusal(err error) (string, bool) {
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode < 400 || apiErr.StatusCode >= 500 {
		return "", false
	}
	switch apiErr.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusRequestTimeout, http.StatusTooManyRequests:
		return "", false
	}
	var body struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(apiErr.Body, &body) == nil && body.Message != "" {
		return body.Message, true
	}
	return fmt.Sprintf("status %d", apiErr.StatusCode), true
}

func isNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}
