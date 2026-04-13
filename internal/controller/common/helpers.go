package common

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	servarrclient "github.com/kyleseneker/media-operator/internal/client/servarr"
	"github.com/kyleseneker/media-operator/internal/engine"
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
	if err := sw.Update(ctx, obj); err != nil {
		log.FromContext(ctx).Error(err, "failed to update status")
	}
}

// UpdateStatusUnreachable sets Ready=False and Synced=False when the app cannot be reached.
func UpdateStatusUnreachable(ctx context.Context, sw client.SubResourceWriter, obj ConfigResource, reason, message string) {
	generation := obj.GetGeneration()
	conditions := obj.GetConditions()
	reconciler.SetCondition(conditions, generation, engine.ConditionReady, string(metav1.ConditionFalse), reason, message)
	reconciler.SetCondition(conditions, generation, engine.ConditionSynced, string(metav1.ConditionFalse), reason, message)
	*obj.GetObservedGeneration() = generation
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
