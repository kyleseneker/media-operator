package requests

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	requestsv1alpha1 "github.com/kyleseneker/media-operator/api/requests/v1alpha1"
	maintainerrclient "github.com/kyleseneker/media-operator/internal/client/maintainerr"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

// MaintainerrConfigReconciler reconciles a MaintainerrConfig object.
type MaintainerrConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=maintainerrconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=maintainerrconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *MaintainerrConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var config requestsv1alpha1.MaintainerrConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Resolve API key
	apiKey, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, config.Spec.Connection.APIKeySecretRef)
	if err != nil {
		logger.Error(err, "failed to resolve API key secret")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	tlsCfg, err := engine.ResolveTLSConfig(ctx, r.Client, config.Namespace, config.Spec.Connection.TLS)
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	hc, err := engine.NewHTTPClient(config.Spec.Connection.URL, engine.AuthAPIKey, engine.WithAPIKey(apiKey), engine.WithTLSConfig(tlsCfg), engine.WithAppLabel("maintainerr"))
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonInvalidConfig, err.Error())
		return ctrl.Result{}, nil
	}
	mc := maintainerrclient.NewClient(hc)

	// Health check
	if err := mc.Ping(ctx); err != nil {
		logger.Error(err, "Maintainerr unreachable")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonAppUnreachable, err.Error())
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	var syncErrors []string

	// Reconcile Plex connection
	if config.Spec.PlexConnection != nil {
		if err := reconcileMaintainerrPlex(ctx, r.Client, mc, config.Namespace, config.Spec.PlexConnection); err != nil {
			logger.Error(err, "failed to reconcile Plex connection")
			syncErrors = append(syncErrors, fmt.Sprintf("plex: %v", err))
		}
	}

	// Reconcile Sonarr connection
	if config.Spec.SonarrConnection != nil {
		if err := reconcileMaintainerrArr(ctx, r.Client, mc, config.Namespace, "sonarr", config.Spec.SonarrConnection); err != nil {
			logger.Error(err, "failed to reconcile Sonarr connection")
			syncErrors = append(syncErrors, fmt.Sprintf("sonarr: %v", err))
		}
	}

	// Reconcile Radarr connection
	if config.Spec.RadarrConnection != nil {
		if err := reconcileMaintainerrArr(ctx, r.Client, mc, config.Namespace, "radarr", config.Spec.RadarrConnection); err != nil {
			logger.Error(err, "failed to reconcile Radarr connection")
			syncErrors = append(syncErrors, fmt.Sprintf("radarr: %v", err))
		}
	}

	// Reconcile Overseerr connection
	if config.Spec.OverseerrConnection != nil {
		if err := reconcileMaintainerrArr(ctx, r.Client, mc, config.Namespace, "overseerr", config.Spec.OverseerrConnection); err != nil {
			logger.Error(err, "failed to reconcile Overseerr connection")
			syncErrors = append(syncErrors, fmt.Sprintf("overseerr: %v", err))
		}
	}

	// Reconcile settings
	if config.Spec.Settings != nil {
		if err := reconcileMaintainerrSettings(ctx, mc, config.Spec.Settings); err != nil {
			logger.Error(err, "failed to reconcile settings")
			syncErrors = append(syncErrors, fmt.Sprintf("settings: %v", err))
		}
	}

	// Reconcile rules
	for _, rule := range config.Spec.Rules {
		if err := reconcileMaintainerrRule(ctx, mc, rule); err != nil {
			logger.Error(err, "failed to reconcile rule", "name", rule.Name)
			syncErrors = append(syncErrors, fmt.Sprintf("rule(%s): %v", rule.Name, err))
		}
	}

	if len(syncErrors) > 0 {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, false, engine.ReasonSyncFailed, fmt.Sprintf("partial sync failure: %v", syncErrors))
	} else {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, true, engine.ReasonSynced, "all configuration sections synced")
	}

	return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
}

func reconcileMaintainerrPlex(ctx context.Context, c client.Reader, mc *maintainerrclient.Client, namespace string, plex *requestsv1alpha1.MaintainerrPlexConnection) error {
	token, err := reconciler.ResolveSecretKeyRef(ctx, c, namespace, plex.TokenSecretRef)
	if err != nil {
		return fmt.Errorf("resolving Plex token: %w", err)
	}

	return mc.UpdatePlexSettings(ctx, map[string]interface{}{
		"url":   plex.URL,
		"token": token,
	})
}

func reconcileMaintainerrArr(ctx context.Context, c client.Reader, mc *maintainerrclient.Client, namespace, appType string, conn *requestsv1alpha1.MaintainerrArrConnection) error {
	apiKey, err := reconciler.ResolveSecretKeyRef(ctx, c, namespace, conn.APIKeySecretRef)
	if err != nil {
		return fmt.Errorf("resolving %s API key: %w", appType, err)
	}

	settings := map[string]interface{}{
		"url":    conn.URL,
		"apiKey": apiKey,
	}

	switch appType {
	case "sonarr":
		return mc.UpdateSonarrSettings(ctx, settings)
	case "radarr":
		return mc.UpdateRadarrSettings(ctx, settings)
	case "overseerr":
		return mc.UpdateOverseerrSettings(ctx, settings)
	default:
		return fmt.Errorf("unknown app type: %s", appType)
	}
}

func reconcileMaintainerrSettings(ctx context.Context, mc *maintainerrclient.Client, settings *requestsv1alpha1.MaintainerrSettings) error {
	obj := make(map[string]interface{})
	if settings.CollectionHandling != "" {
		obj["collectionHandling"] = settings.CollectionHandling
	}
	if settings.DryRun != nil {
		obj["dryRun"] = *settings.DryRun
	}
	if len(obj) == 0 {
		return nil
	}
	return mc.UpdateSettings(ctx, obj)
}

func reconcileMaintainerrRule(ctx context.Context, mc *maintainerrclient.Client, rule requestsv1alpha1.MaintainerrRule) error {
	existing, err := mc.ListRules(ctx)
	if err != nil {
		return fmt.Errorf("listing rules: %w", err)
	}

	obj := map[string]interface{}{
		"name":        rule.Name,
		"enabled":     rule.Enable == nil || *rule.Enable,
		"libraryName": rule.LibraryName,
		"mediaType":   rule.MediaType,
		"action":      rule.Action,
	}
	if rule.DeleteFromDisk != nil {
		obj["deleteFromDisk"] = *rule.DeleteFromDisk
	}

	// Build conditions
	if len(rule.Conditions) > 0 {
		conditions := make([]map[string]interface{}, 0, len(rule.Conditions))
		for _, cond := range rule.Conditions {
			conditions = append(conditions, map[string]interface{}{
				"field":    cond.Field,
				"operator": cond.Operator,
				"value":    cond.Value,
			})
		}
		obj["conditions"] = conditions
	}

	// Find existing by name
	for _, e := range existing {
		if name, _ := e["name"].(string); name == rule.Name {
			id, ok := e["id"].(float64)
			if !ok {
				return fmt.Errorf("rule %q has no valid id", rule.Name)
			}
			obj["id"] = int(id)
			return mc.UpdateRule(ctx, int(id), obj)
		}
	}

	return mc.CreateRule(ctx, obj)
}

// maintainerrReferencesSecret checks if the config references the given secret name.
func maintainerrReferencesSecret(config *requestsv1alpha1.MaintainerrConfig, secretName string) bool {
	if config.Spec.Connection.APIKeySecretRef.Name == secretName {
		return true
	}
	if config.Spec.PlexConnection != nil && config.Spec.PlexConnection.TokenSecretRef.Name == secretName {
		return true
	}
	if config.Spec.SonarrConnection != nil && config.Spec.SonarrConnection.APIKeySecretRef.Name == secretName {
		return true
	}
	if config.Spec.RadarrConnection != nil && config.Spec.RadarrConnection.APIKeySecretRef.Name == secretName {
		return true
	}
	if config.Spec.OverseerrConnection != nil && config.Spec.OverseerrConnection.APIKeySecretRef.Name == secretName {
		return true
	}
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *MaintainerrConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&requestsv1alpha1.MaintainerrConfig{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			return ctrlcommon.FindConfigsBySecret(ctx, r.Client, obj, &requestsv1alpha1.MaintainerrConfigList{}, func(list *requestsv1alpha1.MaintainerrConfigList) []reconcile.Request {
				var reqs []reconcile.Request
				for _, c := range list.Items {
					if maintainerrReferencesSecret(&c, obj.GetName()) {
						reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&c)})
					}
				}
				return reqs
			})
		})).
		Named("maintainerrconfig").
		Complete(r)
}
