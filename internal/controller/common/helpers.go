package common

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	indexersv1alpha1 "github.com/kyleseneker/media-operator/api/indexers/v1alpha1"
	pvrclient "github.com/kyleseneker/media-operator/internal/client/pvr"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/metrics"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

const DefaultReconcileInterval = engine.DefaultReconcileInterval

const DriftPolicyObserve = "observe"

// ReconcileInterval returns the configured interval or the default.
func ReconcileInterval(rc *commonv1alpha1.ReconcileConfig) time.Duration {
	if rc != nil && rc.Interval != nil {
		return rc.Interval.Duration
	}
	return DefaultReconcileInterval
}

// PruneEnabled returns whether resource pruning is enabled.
func PruneEnabled(rc *commonv1alpha1.ReconcileConfig) bool {
	return rc != nil && rc.Prune != nil && *rc.Prune
}

// ObserveOnly reports whether drift should be recorded without writing.
func ObserveOnly(rc *commonv1alpha1.ReconcileConfig) bool {
	return rc != nil && rc.DriftPolicy != nil && *rc.DriftPolicy == DriftPolicyObserve
}

// Policy renders the reconcile policy for a CR.
func Policy(rc *commonv1alpha1.ReconcileConfig) engine.SyncPolicy {
	return engine.SyncPolicy{Prune: PruneEnabled(rc), Observe: ObserveOnly(rc)}
}

// UpdateObservationStatus reports comparison results without claiming drift was
// corrected. Read failures take precedence over any differences already found.
func UpdateObservationStatus(ctx context.Context, sw client.SubResourceWriter, obj ConfigResource, drift bool, err error) {
	if err != nil {
		UpdateStatus(ctx, sw, obj, false, engine.ReasonSyncFailed, "observation failed: "+err.Error())
	} else if drift {
		UpdateStatus(ctx, sw, obj, false, "DriftDetected", "configuration differs; observe policy prevents application writes")
	} else {
		UpdateStatus(ctx, sw, obj, true, "Observed", "observable configuration matches; no application writes performed")
	}
}

// UpdateResultStatus reports an engine reconcile: what was applied or, under
// the observe policy, whether anything differs.
func UpdateResultStatus(ctx context.Context, sw client.SubResourceWriter, obj ConfigResource, result engine.ReconcileResult) {
	if ObserveOnly(obj.GetReconcileConfig()) {
		UpdateObservationStatus(ctx, sw, obj, len(result.Drift) > 0, syncError(result.Errors))
		return
	}
	UpdateStatus(ctx, sw, obj, result.Success(), ResultReason(result), result.Message())
}

// UpdateSyncStatus reports a pass of an integration that writes through its
// own client: under the observe policy, whether anything differs; otherwise
// what failed.
func UpdateSyncStatus(ctx context.Context, sw client.SubResourceWriter, obj ConfigResource, observe, drift bool, syncErrors []string) {
	switch {
	case observe:
		UpdateObservationStatus(ctx, sw, obj, drift, syncError(syncErrors))
	case len(syncErrors) > 0:
		UpdateStatus(ctx, sw, obj, false, engine.ReasonSyncFailed, fmt.Sprintf("partial sync failure: %v", syncErrors))
	default:
		UpdateStatus(ctx, sw, obj, true, engine.ReasonSynced, "all configuration sections synced")
	}
}

func syncError(syncErrors []string) error {
	if len(syncErrors) == 0 {
		return nil
	}
	return fmt.Errorf("%v", syncErrors)
}

// ResultReason returns the appropriate status reason for a reconcile result.
func ResultReason(r engine.ReconcileResult) string {
	if r.Success() {
		return engine.ReasonSynced
	}
	return engine.ReasonSyncFailed
}

// UpdateStatus sets the Synced and Ready conditions on a config resource and persists the status.
// Ready=True indicates the app is reachable; Synced reflects whether config was applied.
// UpdateStatusManaged records the resources the operator now manages, so prune
// can distinguish them from resources created outside the operator.
func UpdateStatusManaged(obj ConfigResource, managed map[string][]string) {
	if managed == nil {
		return
	}
	*obj.GetManagedResources() = managed
}

func UpdateStatus(ctx context.Context, sw client.SubResourceWriter, obj ConfigResource, synced bool, reason, message string) {
	syncedStatus := string(metav1.ConditionFalse)
	if synced {
		syncedStatus = string(metav1.ConditionTrue)
		now := metav1.Now()
		*obj.GetLastSyncTime() = &now
	}
	generation := obj.GetGeneration()
	conditions := obj.GetConditions()
	reconciler.SetCondition(conditions, generation, engine.ConditionSynced, syncedStatus, reason, message)
	// If we got far enough to reconcile, the app is reachable.
	reconciler.SetCondition(conditions, generation, engine.ConditionReady, string(metav1.ConditionTrue), engine.ReasonSynced, "app is reachable")
	*obj.GetObservedGeneration() = generation
	recordSynced(obj, synced)
	if err := sw.Update(ctx, obj); err != nil {
		log.FromContext(ctx).Error(err, "failed to update status")
	}
}

func recordSynced(obj ConfigResource, synced bool) {
	v := 0.0
	if synced {
		v = 1.0
	}
	// TypeMeta is cleared on typed objects after a client round-trip, so the
	// concrete Go type is the reliable source for the kind label.
	kind := reflect.TypeOf(obj).Elem().Name()
	metrics.ConfigSynced.WithLabelValues(kind, obj.GetNamespace(), obj.GetName()).Set(v)
}

// UpdateStatusUnreachable sets Ready=False and Synced=False when the app cannot be reached.
func UpdateStatusUnreachable(ctx context.Context, sw client.SubResourceWriter, obj ConfigResource, reason, message string) {
	generation := obj.GetGeneration()
	conditions := obj.GetConditions()
	reconciler.SetCondition(conditions, generation, engine.ConditionReady, string(metav1.ConditionFalse), reason, message)
	reconciler.SetCondition(conditions, generation, engine.ConditionSynced, string(metav1.ConditionFalse), reason, message)
	*obj.GetObservedGeneration() = generation
	recordSynced(obj, false)
	if err := sw.Update(ctx, obj); err != nil {
		log.FromContext(ctx).Error(err, "failed to update status")
	}
}

// BoolTo01 converts a bool to "1" or "0" for APIs that use numeric strings (Plex, SABnzbd).
func BoolTo01(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// EnqueueForSecret re-reconciles every config of the listed kind, in a changed
// Secret's namespace, that refers to that Secret.
func EnqueueForSecret(c client.Client, list client.ObjectList) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(configsForSecret(c, list))
}

func configsForSecret(c client.Client, list client.ObjectList) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		if _, ok := obj.(*corev1.Secret); !ok {
			return nil
		}
		configs := list.DeepCopyObject().(client.ObjectList)
		if err := c.List(ctx, configs, client.InNamespace(obj.GetNamespace())); err != nil {
			log.FromContext(ctx).Error(err, "listing configs for a changed Secret", "secret", obj.GetName())
			return nil
		}
		items, err := meta.ExtractList(configs)
		if err != nil {
			return nil
		}
		var reqs []reconcile.Request
		for _, item := range items {
			config, ok := item.(client.Object)
			if ok && ReferencesSecret(config, obj.GetName()) {
				reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(config)})
			}
		}
		return reqs
	}
}

// ResolveDownloadClientSecrets resolves all secret references for a list of download clients.
// Returns a map keyed by download client name.
func ResolveDownloadClientSecrets(ctx context.Context, c client.Reader, namespace string, downloadClients []commonv1alpha1.DownloadClient) (map[string]pvrclient.DownloadClientResolvedSecrets, error) {
	resolved := make(map[string]pvrclient.DownloadClientResolvedSecrets, len(downloadClients))
	for _, dc := range downloadClients {
		var s pvrclient.DownloadClientResolvedSecrets
		if dc.UsernameSecretRef != nil {
			val, err := reconciler.ResolveSecretKeyRef(ctx, c, namespace, *dc.UsernameSecretRef)
			if err != nil {
				return nil, fmt.Errorf("download client %q username: %w", dc.Name, err)
			}
			s.Username = val
		}
		if dc.PasswordSecretRef != nil {
			val, err := reconciler.ResolveSecretKeyRef(ctx, c, namespace, *dc.PasswordSecretRef)
			if err != nil {
				return nil, fmt.Errorf("download client %q password: %w", dc.Name, err)
			}
			s.Password = val
		}
		if dc.APIKeySecretRef != nil {
			val, err := reconciler.ResolveSecretKeyRef(ctx, c, namespace, *dc.APIKeySecretRef)
			if err != nil {
				return nil, fmt.Errorf("download client %q apiKey: %w", dc.Name, err)
			}
			s.APIKey = val
		}
		resolved[dc.Name] = s
	}
	return resolved, nil
}

// ResolveConfigFields returns a copy of fields with any valueFrom reference
// resolved into a literal value, so secrets stay out of the CR.
func ResolveConfigFields(ctx context.Context, c client.Reader, namespace, owner string, fields []commonv1alpha1.ConfigField) ([]commonv1alpha1.ConfigField, error) {
	if len(fields) == 0 {
		return fields, nil
	}
	out := make([]commonv1alpha1.ConfigField, len(fields))
	copy(out, fields)
	for i := range out {
		if out[i].ValueFrom == nil {
			continue
		}
		if out[i].Value != nil {
			return nil, fmt.Errorf("%s field %q: value and valueFrom are mutually exclusive", owner, out[i].Name)
		}
		val, err := reconciler.ResolveSecretKeyRef(ctx, c, namespace, *out[i].ValueFrom)
		if err != nil {
			return nil, fmt.Errorf("%s field %q: %w", owner, out[i].Name, err)
		}
		raw, err := json.Marshal(val)
		if err != nil {
			return nil, fmt.Errorf("%s field %q: encoding secret value: %w", owner, out[i].Name, err)
		}
		out[i].Value = &commonv1alpha1.FieldValue{Raw: raw}
		out[i].ValueFrom = nil
	}
	return out, nil
}

// ResolveIndexerSecrets returns indexers with their field secret references resolved.
func ResolveIndexerSecrets(ctx context.Context, c client.Reader, namespace string, in []commonv1alpha1.Indexer) ([]commonv1alpha1.Indexer, error) {
	out := make([]commonv1alpha1.Indexer, len(in))
	copy(out, in)
	for i := range out {
		f, err := ResolveConfigFields(ctx, c, namespace, "indexer "+out[i].Name, out[i].Fields)
		if err != nil {
			return nil, err
		}
		out[i].Fields = f
	}
	return out, nil
}

// ResolveNotificationSecrets returns notifications with their field secret references resolved.
func ResolveNotificationSecrets(ctx context.Context, c client.Reader, namespace string, in []commonv1alpha1.Notification) ([]commonv1alpha1.Notification, error) {
	out := make([]commonv1alpha1.Notification, len(in))
	copy(out, in)
	for i := range out {
		f, err := ResolveConfigFields(ctx, c, namespace, "notification "+out[i].Name, out[i].Fields)
		if err != nil {
			return nil, err
		}
		out[i].Fields = f
	}
	return out, nil
}

// ResolveImportListSecrets returns import lists with their field secret references resolved.
func ResolveImportListSecrets(ctx context.Context, c client.Reader, namespace string, in []commonv1alpha1.ImportList) ([]commonv1alpha1.ImportList, error) {
	out := make([]commonv1alpha1.ImportList, len(in))
	copy(out, in)
	for i := range out {
		f, err := ResolveConfigFields(ctx, c, namespace, "import list "+out[i].Name, out[i].Fields)
		if err != nil {
			return nil, err
		}
		out[i].Fields = f
	}
	return out, nil
}

// ResolveProwlarrFields returns a copy of fields with any valueFrom reference resolved.
func ResolveProwlarrFields(ctx context.Context, c client.Reader, namespace, owner string, fields []indexersv1alpha1.ProwlarrField) ([]indexersv1alpha1.ProwlarrField, error) {
	if len(fields) == 0 {
		return fields, nil
	}
	out := make([]indexersv1alpha1.ProwlarrField, len(fields))
	copy(out, fields)
	for i := range out {
		if out[i].ValueFrom == nil {
			continue
		}
		if out[i].Value != nil {
			return nil, fmt.Errorf("%s field %q: value and valueFrom are mutually exclusive", owner, out[i].Name)
		}
		val, err := reconciler.ResolveSecretKeyRef(ctx, c, namespace, *out[i].ValueFrom)
		if err != nil {
			return nil, fmt.Errorf("%s field %q: %w", owner, out[i].Name, err)
		}
		out[i].Value = &val
		out[i].ValueFrom = nil
	}
	return out, nil
}

var secretKeyRefType = reflect.TypeFor[commonv1alpha1.SecretKeyRef]()

// ReferencesSecret reports whether a config refers to the named Secret
// anywhere. Every Secret reference in the API is a SecretKeyRef, so walking the
// config finds new ones as fields are added rather than relying on a list kept
// by hand.
func ReferencesSecret(config any, name string) bool {
	return referencesSecret(reflect.ValueOf(config), name)
}

func referencesSecret(v reflect.Value, name string) bool {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		return !v.IsNil() && referencesSecret(v.Elem(), name)
	case reflect.Struct:
		if v.Type() == secretKeyRefType {
			return v.FieldByName("Name").String() == name
		}
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() && referencesSecret(v.Field(i), name) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return false // raw JSON
		}
		for i := range v.Len() {
			if referencesSecret(v.Index(i), name) {
				return true
			}
		}
	case reflect.Map:
		for it := v.MapRange(); it.Next(); {
			if referencesSecret(it.Value(), name) {
				return true
			}
		}
	}
	return false
}

// EmitPruneEvents emits a Kubernetes Warning event for each pruned resource.
func EmitPruneEvents(recorder events.EventRecorder, obj client.Object, pruned []engine.PrunedResource) {
	for _, p := range pruned {
		recorder.Eventf(obj, nil, corev1.EventTypeWarning, "ResourcePruned", "Prune",
			"Pruned unmanaged %s %q (id=%d)", p.Type, p.Name, p.ID)
	}
}

// ConfigChangedPredicate ignores status-only updates generated by reconciliation.
// Deletion and finalizer changes must still enqueue even without a generation
// change. Apply this to the CR watch only, never to Secret watches.
func ConfigChangedPredicate() predicate.Predicate {
	return predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
		if e.ObjectOld == nil || e.ObjectNew == nil {
			return false
		}
		return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() ||
			!e.ObjectOld.GetDeletionTimestamp().Equal(e.ObjectNew.GetDeletionTimestamp()) ||
			!slices.Equal(e.ObjectOld.GetFinalizers(), e.ObjectNew.GetFinalizers()) ||
			!reflect.DeepEqual(e.ObjectOld.GetAnnotations(), e.ObjectNew.GetAnnotations())
	}}
}
