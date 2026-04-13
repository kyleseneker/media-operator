package downloads

import (
	"context"
	"encoding/json"
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

	downloadsv1alpha1 "github.com/kyleseneker/media-operator/api/downloads/v1alpha1"
	qbclient "github.com/kyleseneker/media-operator/internal/client/qbittorrent"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

// QBittorrentConfigReconciler reconciles a QBittorrentConfig object.
type QBittorrentConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=qbittorrentconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=qbittorrentconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *QBittorrentConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var config downloadsv1alpha1.QBittorrentConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Resolve username
	username, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, config.Spec.Connection.UsernameSecretRef)
	if err != nil {
		logger.Error(err, "failed to resolve username secret")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Resolve password
	password, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, config.Spec.Connection.PasswordSecretRef)
	if err != nil {
		logger.Error(err, "failed to resolve password secret")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	tlsCfg, err := engine.ResolveTLSConfig(ctx, r.Client, config.Namespace, config.Spec.Connection.TLS)
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	hc, err := engine.NewHTTPClient(config.Spec.Connection.URL, engine.AuthCookie, engine.WithTLSConfig(tlsCfg), engine.WithDisableRedirect(), engine.WithAppLabel("qbittorrent"))
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonInvalidConfig, err.Error())
		return ctrl.Result{}, nil
	}
	qb := qbclient.NewClient(hc, username, password)

	// Login
	if err := qb.Login(ctx); err != nil {
		logger.Error(err, "qBittorrent login failed")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonAppUnreachable, err.Error())
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	// Health check
	if err := qb.Ping(ctx); err != nil {
		logger.Error(err, "qBittorrent unreachable")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonAppUnreachable, err.Error())
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	var syncErrors []string

	// Reconcile preferences
	if config.Spec.Preferences != nil {
		if err := reconcileQBPreferences(ctx, qb, config.Spec.Preferences); err != nil {
			logger.Error(err, "failed to reconcile preferences")
			syncErrors = append(syncErrors, fmt.Sprintf("preferences: %v", err))
		}
	}

	// Reconcile categories
	for _, cat := range config.Spec.Categories {
		if err := reconcileQBCategory(ctx, qb, cat); err != nil {
			logger.Error(err, "failed to reconcile category", "name", cat.Name)
			syncErrors = append(syncErrors, fmt.Sprintf("category(%s): %v", cat.Name, err))
		}
	}

	if len(syncErrors) > 0 {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, false, engine.ReasonSyncFailed, fmt.Sprintf("partial sync failure: %v", syncErrors))
	} else {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, true, engine.ReasonSynced, "all configuration sections synced")
	}

	return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
}

func reconcileQBPreferences(ctx context.Context, qb *qbclient.Client, prefs *downloadsv1alpha1.QBittorrentPreferences) error {
	data, err := json.Marshal(prefs)
	if err != nil {
		return fmt.Errorf("marshaling preferences: %w", err)
	}
	var prefsMap map[string]interface{}
	if err := json.Unmarshal(data, &prefsMap); err != nil {
		return fmt.Errorf("unmarshaling preferences to map: %w", err)
	}
	return qb.SetPreferences(ctx, prefsMap)
}

func reconcileQBCategory(ctx context.Context, qb *qbclient.Client, cat downloadsv1alpha1.QBittorrentCategory) error {
	existing, err := qb.ListCategories(ctx)
	if err != nil {
		return fmt.Errorf("listing categories: %w", err)
	}

	if _, exists := existing[cat.Name]; exists {
		return qb.EditCategory(ctx, cat.Name, cat.SavePath)
	}
	return qb.CreateCategory(ctx, cat.Name, cat.SavePath)
}

// SetupWithManager sets up the controller with the Manager.
func (r *QBittorrentConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&downloadsv1alpha1.QBittorrentConfig{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			return ctrlcommon.FindConfigsBySecret(ctx, r.Client, obj, &downloadsv1alpha1.QBittorrentConfigList{}, func(list *downloadsv1alpha1.QBittorrentConfigList) []reconcile.Request {
				var reqs []reconcile.Request
				for _, c := range list.Items {
					if c.Spec.Connection.UsernameSecretRef.Name == obj.GetName() ||
						c.Spec.Connection.PasswordSecretRef.Name == obj.GetName() {
						reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&c)})
					}
				}
				return reqs
			})
		})).
		Named("qbittorrentconfig").
		Complete(r)
}
