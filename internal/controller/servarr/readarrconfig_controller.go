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
	servarrclient "github.com/kyleseneker/media-operator/internal/client/servarr"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

type ReadarrConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=readarrconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=readarrconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=media-operator.dev,resources=readarrconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *ReadarrConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var config servarrv1alpha1.ReadarrConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !config.GetDeletionTimestamp().IsZero() {
		handled, derr := ctrlcommon.HandleDeletion(ctx, r.Client, r.Recorder, &config, nil)
		if derr != nil {
			return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
		}
		if handled {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, nil
	}

	if err := ctrlcommon.EnsureFinalizer(ctx, r.Client, &config); err != nil {
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
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

	hc, err := engine.NewHTTPClient(config.Spec.Connection.URL, engine.AuthAPIKey, engine.WithAPIKey(apiKey), engine.WithTLSConfig(tlsCfg), engine.WithAppLabel("readarr"))
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonInvalidConfig, err.Error())
		return ctrl.Result{}, nil
	}
	if err := hc.Ping(ctx, "/api/v1/system/status"); err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonAppUnreachable, err.Error())
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	indexers, err := ctrlcommon.ResolveIndexerSecrets(ctx, r.Client, config.Namespace, config.Spec.Indexers)
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	notifications, err := ctrlcommon.ResolveNotificationSecrets(ctx, r.Client, config.Namespace, config.Spec.Notifications)
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	importLists, err := ctrlcommon.ResolveImportListSecrets(ctx, r.Client, config.Namespace, config.Spec.ImportLists)
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	result, err := servarrclient.ReconcileServarr(ctx, hc, "v1", config.Spec, servarrclient.ServarrOptions{
		RootFolders:     config.Spec.RootFolders,
		DownloadClients: config.Spec.DownloadClients,
		DCSecrets:       dcSecrets,
		CategoryField:   "bookCategory",
		QualityProfiles: config.Spec.QualityProfiles,
		CustomFormats:   config.Spec.CustomFormats,
		Tags:            config.Spec.Tags,
		Indexers:        indexers,
		Notifications:   notifications,
		ImportLists:     importLists,
	}, ctrlcommon.PruneEnabled(config.Spec.Reconcile), config.Status.ManagedResources)
	if err != nil {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, false, engine.ReasonSyncFailed, err.Error())
		return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
	}
	ctrlcommon.UpdateStatusManaged(&config, result.Managed)
	ctrlcommon.EmitPruneEvents(r.Recorder, &config, result.Pruned)
	ctrlcommon.UpdateStatus(ctx, r.Status(), &config, result.Success(), ctrlcommon.ResultReason(result), result.Message())

	return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
}

func (r *ReadarrConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&servarrv1alpha1.ReadarrConfig{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			return ctrlcommon.FindConfigsBySecret(ctx, r.Client, obj, &servarrv1alpha1.ReadarrConfigList{}, func(list *servarrv1alpha1.ReadarrConfigList) []reconcile.Request {
				var reqs []reconcile.Request
				for _, c := range list.Items {
					if c.Spec.Connection.APIKeySecretRef.Name == obj.GetName() || ctrlcommon.DownloadClientReferencesSecret(c.Spec.DownloadClients, obj.GetName()) || ctrlcommon.ListsReferenceSecret(c.Spec.Indexers, c.Spec.Notifications, c.Spec.ImportLists, obj.GetName()) {
						reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&c)})
					}
				}
				return reqs
			})
		})).
		Named("readarrconfig").
		Complete(r)
}
