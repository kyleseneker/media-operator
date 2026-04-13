package servarr

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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	servarrv1alpha1 "github.com/kyleseneker/media-operator/api/servarr/v1alpha1"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	prowlarrclient "github.com/kyleseneker/media-operator/internal/client/prowlarr"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

type ProwlarrConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=prowlarrconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=prowlarrconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *ProwlarrConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var config servarrv1alpha1.ProwlarrConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	apiKey, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, config.Spec.Connection.APIKeySecretRef)
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	tlsCfg, err := engine.ResolveTLSConfig(ctx, r.Client, config.Namespace, config.Spec.Connection.TLS)
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	hc, err := engine.NewHTTPClient(config.Spec.Connection.URL, engine.AuthAPIKey, engine.WithAPIKey(apiKey), engine.WithTLSConfig(tlsCfg), engine.WithAppLabel("prowlarr"))
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonInvalidConfig, err.Error())
		return ctrl.Result{}, nil
	}

	def := prowlarrclient.ProwlarrDefinition()
	if err := hc.Ping(ctx, def.HealthPath); err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonAppUnreachable, err.Error())
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	// Build application payloads (requires per-app secret resolution).
	apps, err := r.buildApplicationPayloads(ctx, config.Namespace, config.Spec.Applications)
	if err != nil {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, false, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	resources := prowlarrclient.ProwlarrResources(prowlarrclient.ProwlarrOptions{
		Tags:            config.Spec.Tags,
		Applications:    apps,
		Indexers:        config.Spec.Indexers,
		Proxies:         config.Spec.Proxies,
		DownloadClients: config.Spec.DownloadClients,
		Notifications:   config.Spec.Notifications,
	})

	result := engine.ReconcileApp(ctx, hc, def, nil, resources, ctrlcommon.PruneEnabled(config.Spec.Reconcile))
	ctrlcommon.EmitPruneEvents(r.Recorder, &config, result.Pruned)
	ctrlcommon.UpdateStatus(ctx, r.Status(), &config, result.Success(), ctrlcommon.ResultReason(result), result.Message())

	return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
}

// buildApplicationPayloads resolves per-application API key secrets and builds payloads.
func (r *ProwlarrConfigReconciler) buildApplicationPayloads(ctx context.Context, namespace string, apps []servarrv1alpha1.ProwlarrApplication) ([]map[string]interface{}, error) {
	payloads := make([]map[string]interface{}, 0, len(apps))
	for _, app := range apps {
		appAPIKey, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, namespace, app.APIKeySecretRef)
		if err != nil {
			return nil, fmt.Errorf("application %q apiKeySecretRef: %w", app.Name, err)
		}
		payloads = append(payloads, prowlarrclient.BuildProwlarrApplicationPayload(app, appAPIKey))
	}
	return payloads, nil
}

func (r *ProwlarrConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&servarrv1alpha1.ProwlarrConfig{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			return ctrlcommon.FindConfigsBySecret(ctx, r.Client, obj, &servarrv1alpha1.ProwlarrConfigList{}, func(list *servarrv1alpha1.ProwlarrConfigList) []reconcile.Request {
				var reqs []reconcile.Request
				for _, c := range list.Items {
					if c.Spec.Connection.APIKeySecretRef.Name == obj.GetName() {
						reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&c)})
						continue
					}
					for _, app := range c.Spec.Applications {
						if app.APIKeySecretRef.Name == obj.GetName() {
							reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&c)})
							break
						}
					}
				}
				return reqs
			})
		})).
		Named("prowlarrconfig").
		Complete(r)
}
