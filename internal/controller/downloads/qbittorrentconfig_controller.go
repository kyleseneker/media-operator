package downloads

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
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
	Recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=qbittorrentconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=qbittorrentconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=media-operator.dev,resources=qbittorrentconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *QBittorrentConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var config downloadsv1alpha1.QBittorrentConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if done, after := ctrlcommon.HandleLifecycle(ctx, r.Client, r.Recorder, &config, nil); done {
		return ctrl.Result{RequeueAfter: after}, nil
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

var maxRatioActions = map[string]int{
	"stop":               0,
	"remove":             1,
	"enableSuperSeeding": 2,
	"removeWithContent":  3,
}

// qbPreferencePayload maps spec fields to the keys qBittorrent's API expects.
func qbPreferencePayload(prefs *downloadsv1alpha1.QBittorrentPreferences) (map[string]any, error) {
	out := map[string]any{}
	set := func(key string, v any) {
		if !reflect.ValueOf(v).IsNil() {
			out[key] = reflect.ValueOf(v).Elem().Interface()
		}
	}

	if prefs.SavePath != "" {
		out["save_path"] = prefs.SavePath
	}
	if prefs.Locale != "" {
		out["locale"] = prefs.Locale
	}
	set("temp_path_enabled", prefs.TempPathEnabled)
	set("dht", prefs.DHT)
	set("pex", prefs.PEX)
	set("lsd", prefs.LSD)
	set("encryption", prefs.Encryption)
	set("max_connec", prefs.MaxConnec)
	set("max_connec_per_torrent", prefs.MaxConnecPerTorrent)
	set("max_uploads", prefs.MaxUploads)
	set("max_uploads_per_torrent", prefs.MaxUploadsPerTorrent)
	set("max_ratio_enabled", prefs.MaxRatioEnabled)
	set("max_seeding_time_enabled", prefs.MaxSeedingTimeEnabled)
	set("max_seeding_time", prefs.MaxSeedingTime)
	set("preallocate_all", prefs.PreallocateAll)
	set("web_ui_port", prefs.WebUIPort)
	set("bypass_auth_subnet_whitelist_enabled", prefs.BypassAuthSubnetWhitelistEnabled)
	if prefs.BypassAuthSubnetWhitelist != "" {
		out["bypass_auth_subnet_whitelist"] = prefs.BypassAuthSubnetWhitelist
	}

	if prefs.MaxRatio != nil {
		ratio, err := strconv.ParseFloat(*prefs.MaxRatio, 64)
		if err != nil {
			return nil, fmt.Errorf("parsing maxRatio %q: %w", *prefs.MaxRatio, err)
		}
		out["max_ratio"] = ratio
	}
	if prefs.MaxRatioAction != nil {
		act, ok := maxRatioActions[*prefs.MaxRatioAction]
		if !ok {
			return nil, fmt.Errorf("unknown maxRatioAction %q", *prefs.MaxRatioAction)
		}
		out["max_ratio_act"] = act
	}

	return out, nil
}

func reconcileQBPreferences(ctx context.Context, qb *qbclient.Client, prefs *downloadsv1alpha1.QBittorrentPreferences) error {
	payload, err := qbPreferencePayload(prefs)
	if err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	return qb.SetPreferences(ctx, payload)
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
