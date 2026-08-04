package common

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	servarrv1alpha1 "github.com/kyleseneker/media-operator/api/servarr/v1alpha1"
	servarrclient "github.com/kyleseneker/media-operator/internal/client/servarr"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/metrics"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

const DefaultReconcileInterval = engine.DefaultReconcileInterval

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

// FindConfigsBySecret is a generic helper for watching Secrets and mapping them back to config CRs.
func FindConfigsBySecret[L client.ObjectList](ctx context.Context, c client.Client, obj client.Object, list L, extract func(L) []reconcile.Request) []reconcile.Request {
	if _, ok := obj.(*corev1.Secret); !ok {
		return nil
	}
	if err := c.List(ctx, list, client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}
	return extract(list)
}

// ResolveDownloadClientSecrets resolves all secret references for a list of download clients.
// Returns a map keyed by download client name.
func ResolveDownloadClientSecrets(ctx context.Context, c client.Reader, namespace string, downloadClients []commonv1alpha1.DownloadClient) (map[string]servarrclient.DownloadClientResolvedSecrets, error) {
	resolved := make(map[string]servarrclient.DownloadClientResolvedSecrets, len(downloadClients))
	for _, dc := range downloadClients {
		var s servarrclient.DownloadClientResolvedSecrets
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
func ResolveProwlarrFields(ctx context.Context, c client.Reader, namespace, owner string, fields []servarrv1alpha1.ProwlarrField) ([]servarrv1alpha1.ProwlarrField, error) {
	if len(fields) == 0 {
		return fields, nil
	}
	out := make([]servarrv1alpha1.ProwlarrField, len(fields))
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

// ProwlarrFieldsReferenceSecret returns true if any field sources from the named secret.
func ProwlarrFieldsReferenceSecret(fields []servarrv1alpha1.ProwlarrField, secretName string) bool {
	for _, f := range fields {
		if f.ValueFrom != nil && f.ValueFrom.Name == secretName {
			return true
		}
	}
	return false
}

// ListsReferenceSecret returns true if any indexer, notification or import list sources from the named secret.
func ListsReferenceSecret(indexers []commonv1alpha1.Indexer, notifications []commonv1alpha1.Notification, importLists []commonv1alpha1.ImportList, secretName string) bool {
	for _, x := range indexers {
		if ConfigFieldsReferenceSecret(x.Fields, secretName) {
			return true
		}
	}
	for _, x := range notifications {
		if ConfigFieldsReferenceSecret(x.Fields, secretName) {
			return true
		}
	}
	for _, x := range importLists {
		if ConfigFieldsReferenceSecret(x.Fields, secretName) {
			return true
		}
	}
	return false
}

// ConfigFieldsReferenceSecret returns true if any field sources from the named secret.
func ConfigFieldsReferenceSecret(fields []commonv1alpha1.ConfigField, secretName string) bool {
	for _, f := range fields {
		if f.ValueFrom != nil && f.ValueFrom.Name == secretName {
			return true
		}
	}
	return false
}

// DownloadClientReferencesSecret returns true if any download client references the named secret.
func DownloadClientReferencesSecret(downloadClients []commonv1alpha1.DownloadClient, secretName string) bool {
	for _, dc := range downloadClients {
		if dc.UsernameSecretRef != nil && dc.UsernameSecretRef.Name == secretName {
			return true
		}
		if dc.PasswordSecretRef != nil && dc.PasswordSecretRef.Name == secretName {
			return true
		}
		if dc.APIKeySecretRef != nil && dc.APIKeySecretRef.Name == secretName {
			return true
		}
	}
	return false
}

// EmitPruneEvents emits a Kubernetes Warning event for each pruned resource.
func EmitPruneEvents(recorder record.EventRecorder, obj client.Object, pruned []engine.PrunedResource) {
	for _, p := range pruned {
		recorder.Eventf(obj, corev1.EventTypeWarning, "ResourcePruned",
			"Pruned unmanaged %s %q (id=%d)", p.Type, p.Name, p.ID)
	}
}
