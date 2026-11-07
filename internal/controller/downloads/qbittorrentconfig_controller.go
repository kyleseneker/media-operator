package downloads

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

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

	if ctrlcommon.ObserveOnly(config.Spec.Reconcile) {
		drift, err := observeQBConfig(ctx, qb, config.Spec)
		ctrlcommon.UpdateObservationStatus(ctx, r.Status(), &config, drift, err)
		return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
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
	if len(config.Spec.Categories) > 0 {
		existing, err := qb.ListCategories(ctx)
		if err != nil {
			syncErrors = append(syncErrors, fmt.Sprintf("listing categories: %v", err))
		}
		for _, cat := range config.Spec.Categories {
			if err != nil {
				break
			}
			if cerr := reconcileQBCategory(ctx, qb, existing, cat); cerr != nil {
				logger.Error(cerr, "failed to reconcile category", "name", cat.Name)
				syncErrors = append(syncErrors, fmt.Sprintf("category(%s): %v", cat.Name, cerr))
			}
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
		out["save_path"] = qbPath(prefs.SavePath)
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

// qbPath is a path as qBittorrent stores it: without a trailing separator.
// Declaring one would otherwise read back as a change on every pass.
func qbPath(p string) string {
	trimmed := strings.TrimRight(p, `/\`)
	if trimmed == "" {
		return p
	}
	return trimmed
}

// reconcileQBPreferences writes only when qBittorrent's preferences differ;
// setPreferences on every pass would re-apply listen and WebUI settings.
func reconcileQBPreferences(ctx context.Context, qb *qbclient.Client, prefs *downloadsv1alpha1.QBittorrentPreferences) error {
	payload, err := qbPreferencePayload(prefs)
	if err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	current, err := qb.GetPreferences(ctx)
	if err != nil {
		return fmt.Errorf("reading preferences: %w", err)
	}
	changed, err := engine.CorrectDifference("qbittorrent", "settings", "preferences", current, payload)
	if err != nil || !changed {
		return err
	}
	return qb.SetPreferences(ctx, payload)
}

func reconcileQBCategory(ctx context.Context, qb *qbclient.Client, existing map[string]any, cat downloadsv1alpha1.QBittorrentCategory) error {
	current, exists := existing[cat.Name].(map[string]any)
	if !exists {
		return qb.CreateCategory(ctx, cat.Name, qbPath(cat.SavePath))
	}
	changed, err := engine.CorrectDifference("qbittorrent", "categories", cat.Name, current, map[string]any{"savePath": qbPath(cat.SavePath)})
	if err != nil || !changed {
		return err
	}
	return qb.EditCategory(ctx, cat.Name, qbPath(cat.SavePath))
}

// SetupWithManager sets up the controller with the Manager.
func (r *QBittorrentConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&downloadsv1alpha1.QBittorrentConfig{}, builder.WithPredicates(ctrlcommon.ConfigChangedPredicate())).
		Watches(&corev1.Secret{}, ctrlcommon.EnqueueForSecret(r.Client, &downloadsv1alpha1.QBittorrentConfigList{})).
		Named("qbittorrentconfig").
		Complete(r)
}

func observeQBConfig(ctx context.Context, qb *qbclient.Client, spec downloadsv1alpha1.QBittorrentConfigSpec) (bool, error) {
	drift := false
	if spec.Preferences != nil {
		desired, err := qbPreferencePayload(spec.Preferences)
		if err != nil {
			return false, err
		}
		if len(desired) > 0 {
			current, err := qb.GetPreferences(ctx)
			if err != nil {
				return false, fmt.Errorf("reading preferences: %w", err)
			}
			drift, err = engine.ObserveDifference("qbittorrent", "settings", "preferences", current, desired)
			if err != nil {
				return false, err
			}
		}
	}
	if len(spec.Categories) == 0 {
		return drift, nil
	}
	categories, err := qb.ListCategories(ctx)
	if err != nil {
		return drift, fmt.Errorf("listing categories: %w", err)
	}
	for _, category := range spec.Categories {
		current, _ := categories[category.Name].(map[string]any)
		changed, err := engine.ObserveDifference("qbittorrent", "categories", category.Name, current, map[string]any{"savePath": qbPath(category.SavePath)})
		if err != nil {
			return drift, err
		}
		drift = drift || changed
	}
	return drift, nil
}
