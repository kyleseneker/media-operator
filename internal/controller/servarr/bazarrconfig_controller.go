package servarr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	servarrv1alpha1 "github.com/kyleseneker/media-operator/api/servarr/v1alpha1"
	bazarrclient "github.com/kyleseneker/media-operator/internal/client/bazarr"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

// BazarrConfigReconciler reconciles a BazarrConfig object.
type BazarrConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=bazarrconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=bazarrconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *BazarrConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var config servarrv1alpha1.BazarrConfig
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

	hc, err := engine.NewHTTPClient(config.Spec.Connection.URL, engine.AuthFormEncoded, engine.WithAPIKey(apiKey), engine.WithAPIKeyHeader("X-API-KEY"), engine.WithTLSConfig(tlsCfg), engine.WithAppLabel("bazarr"))
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonInvalidConfig, err.Error())
		return ctrl.Result{}, nil
	}
	bc := bazarrclient.NewClient(hc)

	// Health check
	if err := bc.Ping(ctx); err != nil {
		logger.Error(err, "Bazarr unreachable")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonAppUnreachable, err.Error())
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	var syncErrors []string

	// General settings
	if config.Spec.General != nil {
		if err := bc.PostSettings(ctx, "general", config.Spec.General); err != nil {
			logger.Error(err, "failed to reconcile general settings")
			syncErrors = append(syncErrors, fmt.Sprintf("general: %v", err))
		}
	}

	// SubSync settings
	if config.Spec.SubSync != nil {
		if err := bc.PostSettings(ctx, "subsync", config.Spec.SubSync); err != nil {
			logger.Error(err, "failed to reconcile subsync settings")
			syncErrors = append(syncErrors, fmt.Sprintf("subsync: %v", err))
		}
	}

	// Sonarr connection
	if config.Spec.SonarrConnection != nil {
		if err := r.reconcileBazarrConnection(ctx, bc, config.Namespace, "sonarr", config.Spec.SonarrConnection); err != nil {
			logger.Error(err, "failed to reconcile sonarr connection")
			syncErrors = append(syncErrors, fmt.Sprintf("sonarrConnection: %v", err))
		}
	}

	// Radarr connection
	if config.Spec.RadarrConnection != nil {
		if err := r.reconcileBazarrConnection(ctx, bc, config.Namespace, "radarr", config.Spec.RadarrConnection); err != nil {
			logger.Error(err, "failed to reconcile radarr connection")
			syncErrors = append(syncErrors, fmt.Sprintf("radarrConnection: %v", err))
		}
	}

	// Providers
	if len(config.Spec.Providers) > 0 {
		if err := r.reconcileBazarrProviders(ctx, bc, config.Namespace, config.Spec.Providers); err != nil {
			logger.Error(err, "failed to reconcile providers")
			syncErrors = append(syncErrors, fmt.Sprintf("providers: %v", err))
		}
	}

	// Languages
	if config.Spec.Languages != nil {
		if err := bc.ReconcileLanguages(ctx, config.Spec.Languages); err != nil {
			logger.Error(err, "failed to reconcile languages")
			syncErrors = append(syncErrors, fmt.Sprintf("languages: %v", err))
		}
	}

	if len(syncErrors) > 0 {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, false, engine.ReasonSyncFailed, fmt.Sprintf("partial sync failure: %v", syncErrors))
	} else {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, true, engine.ReasonSynced, "all configuration sections synced")
	}

	return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
}

func (r *BazarrConfigReconciler) reconcileBazarrConnection(ctx context.Context, bc *bazarrclient.Client, namespace, section string, conn *servarrv1alpha1.BazarrAppConnection) error {
	connAPIKey, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, namespace, conn.APIKeySecretRef)
	if err != nil {
		return fmt.Errorf("resolving apiKeySecretRef for %s: %w", section, err)
	}

	form := bazarrclient.StructToFormData(section, conn)
	// Override the apiKeySecretRef field with the resolved key
	delete(form, fmt.Sprintf("settings-%s-apiKeySecretRef", section))
	form.Set(fmt.Sprintf("settings-%s-apikey", section), connAPIKey)

	return bc.PostForm(ctx, "/api/system/settings", form)
}

func (r *BazarrConfigReconciler) reconcileBazarrProviders(ctx context.Context, bc *bazarrclient.Client, namespace string, providers []servarrv1alpha1.BazarrProvider) error {
	enabledProviders := []string{}
	form := url.Values{}

	for _, p := range providers {
		enabled := p.Enabled == nil || *p.Enabled
		if enabled {
			enabledProviders = append(enabledProviders, p.Name)
		}

		for k, v := range p.Settings {
			// Resolve secret references for values starting with "ENV:"
			resolvedVal := v
			if strings.HasPrefix(v, "ENV:") {
				// Parse "ENV:secretName:secretKey" format
				parts := strings.SplitN(v[4:], ":", 2)
				if len(parts) == 2 {
					ref := commonv1alpha1.SecretKeyRef{Name: parts[0], Key: parts[1]}
					secret, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, namespace, ref)
					if err != nil {
						return fmt.Errorf("resolving secret for provider %s field %s: %w", p.Name, k, err)
					}
					resolvedVal = secret
				}
			}
			form.Set(fmt.Sprintf("settings-%s-%s", p.Name, k), resolvedVal)
		}
	}

	providersJSON, err := json.Marshal(enabledProviders)
	if err != nil {
		return fmt.Errorf("marshaling enabled providers: %w", err)
	}
	form.Set("settings-general-enabled_providers", string(providersJSON))

	return bc.PostForm(ctx, "/api/system/settings", form)
}

// SetupWithManager sets up the controller with the Manager.
func (r *BazarrConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&servarrv1alpha1.BazarrConfig{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			return ctrlcommon.FindConfigsBySecret(ctx, r.Client, obj, &servarrv1alpha1.BazarrConfigList{}, func(list *servarrv1alpha1.BazarrConfigList) []reconcile.Request {
				var reqs []reconcile.Request
				for _, c := range list.Items {
					if c.Spec.Connection.APIKeySecretRef.Name == obj.GetName() {
						reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&c)})
						continue
					}
					if c.Spec.SonarrConnection != nil && c.Spec.SonarrConnection.APIKeySecretRef.Name == obj.GetName() {
						reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&c)})
						continue
					}
					if c.Spec.RadarrConnection != nil && c.Spec.RadarrConnection.APIKeySecretRef.Name == obj.GetName() {
						reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&c)})
						continue
					}
				}
				return reqs
			})
		})).
		Named("bazarrconfig").
		Complete(r)
}
