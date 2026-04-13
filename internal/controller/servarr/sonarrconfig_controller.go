package servarr

import (
	"context"
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
	servarrclient "github.com/kyleseneker/media-operator/internal/client/servarr"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

type SonarrConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=sonarrconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=sonarrconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *SonarrConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var config servarrv1alpha1.SonarrConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	apiKey, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, config.Spec.Connection.APIKeySecretRef)
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	dcSecrets, err := ctrlcommon.ResolveDownloadClientSecrets(ctx, r.Client, config.Namespace, config.Spec.DownloadClients)
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	tlsCfg, err := engine.ResolveTLSConfig(ctx, r.Client, config.Namespace, config.Spec.Connection.TLS)
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	hc, err := engine.NewHTTPClient(config.Spec.Connection.URL, engine.AuthAPIKey, engine.WithAPIKey(apiKey), engine.WithTLSConfig(tlsCfg), engine.WithAppLabel("sonarr"))
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonInvalidConfig, err.Error())
		return ctrl.Result{}, nil
	}
	if err := hc.Ping(ctx, "/api/v3/system/status"); err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonAppUnreachable, err.Error())
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	result, err := servarrclient.ReconcileServarr(ctx, hc, "v3", config.Spec, servarrclient.ServarrOptions{
		RootFolders:     config.Spec.RootFolders,
		DownloadClients: config.Spec.DownloadClients,
		DCSecrets:       dcSecrets,
		CategoryField:   "tvCategory",
		QualityProfiles: config.Spec.QualityProfiles,
		CustomFormats:   config.Spec.CustomFormats,
		Tags:            config.Spec.Tags,
		Indexers:        config.Spec.Indexers,
		Notifications:   config.Spec.Notifications,
		ImportLists:     config.Spec.ImportLists,
	}, ctrlcommon.PruneEnabled(config.Spec.Reconcile))
	if err != nil {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, false, engine.ReasonSyncFailed, err.Error())
		return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
	}
	ctrlcommon.EmitPruneEvents(r.Recorder, &config, result.Pruned)
	ctrlcommon.UpdateStatus(ctx, r.Status(), &config, result.Success(), ctrlcommon.ResultReason(result), result.Message())

	return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
}

func (r *SonarrConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&servarrv1alpha1.SonarrConfig{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			return ctrlcommon.FindConfigsBySecret(ctx, r.Client, obj, &servarrv1alpha1.SonarrConfigList{}, func(list *servarrv1alpha1.SonarrConfigList) []reconcile.Request {
				var reqs []reconcile.Request
				for _, c := range list.Items {
					if c.Spec.Connection.APIKeySecretRef.Name == obj.GetName() || ctrlcommon.DownloadClientReferencesSecret(c.Spec.DownloadClients, obj.GetName()) {
						reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&c)})
					}
				}
				return reqs
			})
		})).
		Named("sonarrconfig").
		Complete(r)
}
