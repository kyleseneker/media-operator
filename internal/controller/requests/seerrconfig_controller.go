package requests

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	requestsv1alpha1 "github.com/kyleseneker/media-operator/api/requests/v1alpha1"
	seerrclient "github.com/kyleseneker/media-operator/internal/client/seerr"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/metrics"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

// SeerrConfigReconciler reconciles a SeerrConfig object.
type SeerrConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=seerrconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=seerrconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=media-operator.dev,resources=seerrconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *SeerrConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var config requestsv1alpha1.SeerrConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if done, after := ctrlcommon.HandleLifecycle(ctx, r.Client, r.Recorder, &config, nil); done {
		return ctrl.Result{RequeueAfter: after}, nil
	}

	tlsCfg, err := engine.ResolveTLSConfig(ctx, r.Client, config.Namespace, config.Spec.Connection.TLS)
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	hc, err := engine.NewHTTPClient(config.Spec.Connection.URL, engine.AuthSession, engine.WithTLSConfig(tlsCfg), engine.WithAppLabel("seerr"))
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonInvalidConfig, err.Error())
		return ctrl.Result{}, nil
	}
	sc := seerrclient.NewClient(hc)

	// Check initialization state
	isInitialized, err := sc.IsInitialized(ctx)
	if err != nil {
		logger.Error(err, "Seerr unreachable")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonAppUnreachable, err.Error())
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	writes := &seerrWrites{observe: ctrlcommon.ObserveOnly(config.Spec.Reconcile)}
	if !isInitialized && writes.observe {
		// Signing in to a Seerr that has not been set up configures its media
		// server, so observe mode reports the setup as missing and stops.
		config.Status.Initialized = &isInitialized
		drift, err := engine.ObserveDifference("seerr", "settings", "initialization", nil, map[string]any{"initialized": true})
		ctrlcommon.UpdateObservationStatus(ctx, r.Status(), &config, drift, err)
		return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
	}

	if err := r.establishSession(ctx, sc, &config); err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	if !isInitialized {
		if err := r.setup(ctx, sc, writes, &config); err != nil {
			ctrlcommon.UpdateStatus(ctx, r.Status(), &config, false, engine.ReasonSyncFailed, err.Error())
			return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
		}
	}

	initialized := true
	config.Status.Initialized = &initialized

	var syncErrors []string

	// Reconcile Sonarr connection
	if config.Spec.Sonarr != nil {
		if err := r.reconcileSonarrConnection(ctx, sc, writes, &config, false); err != nil {
			logger.Error(err, "failed to reconcile Sonarr connection")
			syncErrors = append(syncErrors, fmt.Sprintf("sonarr: %v", err))
		}
	}

	// Reconcile Radarr connection
	if config.Spec.Radarr != nil {
		if err := r.reconcileRadarrConnection(ctx, sc, writes, &config, false); err != nil {
			logger.Error(err, "failed to reconcile Radarr connection")
			syncErrors = append(syncErrors, fmt.Sprintf("radarr: %v", err))
		}
	}

	// Reconcile main settings
	if config.Spec.Main != nil {
		if err := reconcileSeerrMainSettings(ctx, sc, writes, config.Spec.Main); err != nil {
			logger.Error(err, "failed to reconcile main settings")
			syncErrors = append(syncErrors, fmt.Sprintf("main: %v", err))
		}
	}

	// Reconcile notification agents
	for _, agent := range config.Spec.Notifications {
		if err := reconcileSeerrNotificationAgent(ctx, sc, writes, agent); err != nil {
			logger.Error(err, "failed to reconcile notification agent", "agent", agent.Agent)
			syncErrors = append(syncErrors, fmt.Sprintf("notification(%s): %v", agent.Agent, err))
		}
	}

	if writes.observe {
		var observeErr error
		if len(syncErrors) > 0 {
			observeErr = fmt.Errorf("%v", syncErrors)
		}
		ctrlcommon.UpdateObservationStatus(ctx, r.Status(), &config, len(writes.drift) > 0, observeErr)
	} else if len(syncErrors) > 0 {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, false, engine.ReasonSyncFailed, fmt.Sprintf("partial sync failure: %v", syncErrors))
	} else {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, true, engine.ReasonSynced, "all configuration sections synced")
	}

	return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
}

// setup runs Seerr's first-time setup: the media server's libraries, the Sonarr
// and Radarr connections and the main settings, then marks Seerr initialized.
func (r *SeerrConfigReconciler) setup(
	ctx context.Context, sc *seerrclient.Client, writes *seerrWrites, config *requestsv1alpha1.SeerrConfig,
) error {
	logger := log.FromContext(ctx)
	var initErrors []string

	// Sync media server libraries
	if config.Spec.JellyfinAuth != nil {
		if err := syncJellyfinLibraries(ctx, sc, config.Spec.JellyfinAuth); err != nil {
			logger.Error(err, "failed to configure Jellyfin libraries")
			initErrors = append(initErrors, err.Error())
		}
	}
	if config.Spec.PlexAuth != nil {
		if _, err := sc.Post(ctx, "/api/v1/settings/plex/sync", map[string]any{}); err != nil {
			logger.Error(err, "failed to sync Plex libraries")
			initErrors = append(initErrors, fmt.Sprintf("plex library sync: %v", err))
		}
	}

	if config.Spec.Sonarr != nil {
		if err := r.reconcileSonarrConnection(ctx, sc, writes, config, true); err != nil {
			logger.Error(err, "failed to configure Sonarr during initialization")
			initErrors = append(initErrors, fmt.Sprintf("sonarr init: %v", err))
		}
	}
	if config.Spec.Radarr != nil {
		if err := r.reconcileRadarrConnection(ctx, sc, writes, config, true); err != nil {
			logger.Error(err, "failed to configure Radarr during initialization")
			initErrors = append(initErrors, fmt.Sprintf("radarr init: %v", err))
		}
	}
	if config.Spec.Main != nil {
		if err := reconcileSeerrMainSettings(ctx, sc, writes, config.Spec.Main); err != nil {
			logger.Error(err, "failed to configure main settings during initialization")
			initErrors = append(initErrors, fmt.Sprintf("main settings init: %v", err))
		}
	}
	if len(initErrors) > 0 {
		return fmt.Errorf("initialization failed: %v", initErrors)
	}

	if _, err := sc.Post(ctx, "/api/v1/settings/initialize", map[string]any{}); err != nil {
		logger.Error(err, "failed to initialize Seerr")
		return fmt.Errorf("initialization: %w", err)
	}
	logger.Info("Seerr initialization completed")
	return nil
}

func (r *SeerrConfigReconciler) reconcileSonarrConnection(
	ctx context.Context, sc *seerrclient.Client, w *seerrWrites, config *requestsv1alpha1.SeerrConfig, createOnly bool,
) error {
	sonarrAPIKey, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, config.Spec.Sonarr.APIKeySecretRef)
	if err != nil {
		return fmt.Errorf("resolving sonarr API key: %w", err)
	}

	payload, err := resolvedServicePayload(ctx, config.Spec.Sonarr, sonarrAPIKey)
	if err != nil {
		return fmt.Errorf("resolving sonarr profile: %w", err)
	}
	if _, ok := payload["enableSeasonFolders"]; !ok {
		payload["enableSeasonFolders"] = false
	}

	if createOnly {
		_, err := sc.Post(ctx, "/api/v1/settings/sonarr", payload)
		return err
	}

	existing, err := sc.GetList(ctx, "/api/v1/settings/sonarr")
	if err != nil {
		return fmt.Errorf("listing sonarr connections: %w", err)
	}

	for _, e := range existing {
		if name, ok := e["name"].(string); ok && name == config.Spec.Sonarr.Name {
			id, ok := e["id"].(float64)
			if !ok {
				continue
			}
			if !seerrDiffers(e, payload) {
				return nil
			}
			return w.apply("sonarr", name, func() error {
				return sc.Put(ctx, fmt.Sprintf("/api/v1/settings/sonarr/%d", int(id)), payload)
			})
		}
	}

	return w.apply("sonarr", config.Spec.Sonarr.Name, func() error {
		_, err := sc.Post(ctx, "/api/v1/settings/sonarr", payload)
		return err
	})
}

func (r *SeerrConfigReconciler) reconcileRadarrConnection(
	ctx context.Context, sc *seerrclient.Client, w *seerrWrites, config *requestsv1alpha1.SeerrConfig, createOnly bool,
) error {
	radarrAPIKey, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, config.Spec.Radarr.APIKeySecretRef)
	if err != nil {
		return fmt.Errorf("resolving radarr API key: %w", err)
	}

	payload, err := resolvedServicePayload(ctx, config.Spec.Radarr, radarrAPIKey)
	if err != nil {
		return fmt.Errorf("resolving radarr profile: %w", err)
	}
	if _, ok := payload["minimumAvailability"]; !ok {
		payload["minimumAvailability"] = "released"
	}

	if createOnly {
		_, err := sc.Post(ctx, "/api/v1/settings/radarr", payload)
		return err
	}

	existing, err := sc.GetList(ctx, "/api/v1/settings/radarr")
	if err != nil {
		return fmt.Errorf("listing radarr connections: %w", err)
	}

	for _, e := range existing {
		if name, ok := e["name"].(string); ok && name == config.Spec.Radarr.Name {
			id, ok := e["id"].(float64)
			if !ok {
				continue
			}
			if !seerrDiffers(e, payload) {
				return nil
			}
			return w.apply("radarr", name, func() error {
				return sc.Put(ctx, fmt.Sprintf("/api/v1/settings/radarr/%d", int(id)), payload)
			})
		}
	}

	return w.apply("radarr", config.Spec.Radarr.Name, func() error {
		_, err := sc.Post(ctx, "/api/v1/settings/radarr", payload)
		return err
	})
}

func buildServicePayload(svc *requestsv1alpha1.SeerrServiceConnection, apiKey string) map[string]any {
	payload := map[string]any{
		"name":              svc.Name,
		"hostname":          svc.Hostname,
		"apiKey":            apiKey,
		"port":              0,
		"useSsl":            false,
		"activeProfileId":   0,
		"activeProfileName": svc.ActiveProfileName,
		"activeDirectory":   svc.ActiveDirectory,
		"is4k":              false,
		"isDefault":         false,
	}
	if svc.Port != nil {
		payload["port"] = *svc.Port
	}
	if svc.UseSsl != nil {
		payload["useSsl"] = *svc.UseSsl
	}
	if svc.BaseUrl != "" {
		payload["baseUrl"] = svc.BaseUrl
	}
	if svc.ActiveProfileId != nil {
		payload["activeProfileId"] = *svc.ActiveProfileId
	}
	if svc.ActiveProfileName != "" {
		payload["activeProfileName"] = svc.ActiveProfileName
	}
	if svc.ActiveDirectory != "" {
		payload["activeDirectory"] = svc.ActiveDirectory
	}
	if svc.Is4k != nil {
		payload["is4k"] = *svc.Is4k
	}
	if svc.IsDefault != nil {
		payload["isDefault"] = *svc.IsDefault
	}
	if svc.SeriesType != "" {
		payload["seriesType"] = svc.SeriesType
	}
	if svc.MinimumAvailability != "" {
		payload["minimumAvailability"] = svc.MinimumAvailability
	}
	if svc.EnableSeasonFolders != nil {
		payload["enableSeasonFolders"] = *svc.EnableSeasonFolders
	}
	return payload
}

func reconcileSeerrMainSettings(ctx context.Context, sc *seerrclient.Client, w *seerrWrites, main *requestsv1alpha1.SeerrMain) error {
	payload := map[string]any{}
	if main.ApplicationTitle != "" {
		payload["applicationTitle"] = main.ApplicationTitle
	}
	if main.ApplicationUrl != "" {
		payload["applicationUrl"] = main.ApplicationUrl
	}
	if main.HideAvailable != nil {
		payload["hideAvailable"] = *main.HideAvailable
	}
	if main.LocalLogin != nil {
		payload["localLogin"] = *main.LocalLogin
	}
	if main.MediaServerLogin != nil {
		payload["mediaServerLogin"] = *main.MediaServerLogin
	}
	if main.DefaultPermissions != nil {
		payload["defaultPermissions"] = *main.DefaultPermissions
	}
	if main.PartialRequestsEnabled != nil {
		payload["partialRequestsEnabled"] = *main.PartialRequestsEnabled
	}
	if main.Locale != "" {
		payload["locale"] = main.Locale
	}

	if len(payload) == 0 {
		return nil
	}

	current, err := sc.Get(ctx, "/api/v1/settings/main")
	if err != nil {
		return fmt.Errorf("reading main settings: %w", err)
	}
	if !seerrDiffers(current, payload) {
		return nil
	}
	return w.apply("settings", "main", func() error {
		_, err := sc.Post(ctx, "/api/v1/settings/main", payload)
		return err
	})
}

func reconcileSeerrNotificationAgent(
	ctx context.Context, sc *seerrclient.Client, w *seerrWrites, agent requestsv1alpha1.SeerrNotificationAgent,
) error {
	payload := map[string]any{}
	if agent.Enabled != nil {
		payload["enabled"] = *agent.Enabled
	}
	if agent.Types != nil {
		payload["types"] = *agent.Types
	}
	if len(agent.Options) > 0 {
		opts := make(map[string]any, len(agent.Options))
		for k, v := range agent.Options {
			opts[k] = v
		}
		payload["options"] = opts
	}

	path := fmt.Sprintf("/api/v1/settings/notifications/%s", agent.Agent)
	current, err := sc.Get(ctx, path)
	if err != nil {
		return fmt.Errorf("reading %s notifications: %w", agent.Agent, err)
	}
	if !seerrDiffers(current, payload) {
		return nil
	}
	return w.apply("notifications", agent.Agent, func() error {
		_, err := sc.Post(ctx, path, payload)
		return err
	})
}

// seerrWrites sends Seerr's settings writes, or in observe mode records them
// instead.
type seerrWrites struct {
	observe bool
	drift   []string
}

func (w *seerrWrites) apply(kind, name string, write func() error) error {
	if w.observe {
		metrics.DriftObservedTotal.WithLabelValues("seerr", kind, name).Inc()
		w.drift = append(w.drift, kind+"/"+name)
		return nil
	}
	metrics.DriftCorrectedTotal.WithLabelValues("seerr", kind, name).Inc()
	return write()
}

// seerrDiffers reports whether Seerr's stored settings lack any declared value.
// Writing every section on every pass made each reconcile a burst of saves.
func seerrDiffers(current, payload map[string]any) bool {
	raw, err := json.Marshal(payload)
	if err != nil {
		return true
	}
	var desired any
	if err := json.Unmarshal(raw, &desired); err != nil {
		return true
	}
	return !reconciler.Covers(current, desired)
}

// SetupWithManager sets up the controller with the Manager.
func (r *SeerrConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&requestsv1alpha1.SeerrConfig{}, builder.WithPredicates(ctrlcommon.ConfigChangedPredicate())).
		Watches(&corev1.Secret{}, ctrlcommon.EnqueueForSecret(r.Client, &requestsv1alpha1.SeerrConfigList{})).
		Named("seerrconfig").
		Complete(r)
}

func syncJellyfinLibraries(ctx context.Context, sc *seerrclient.Client, auth *requestsv1alpha1.SeerrJellyfinAuth) error {
	port := 0
	if auth.Port != nil {
		port = *auth.Port
	}
	settings := map[string]any{
		"hostname": auth.Hostname,
		"port":     port,
		"useSsl":   auth.UseSsl != nil && *auth.UseSsl,
		"urlBase":  auth.UrlBase,
	}
	if _, err := sc.Post(ctx, "/api/v1/settings/jellyfin", settings); err != nil {
		return fmt.Errorf("jellyfin settings: %w", err)
	}
	return enableJellyfinLibraries(ctx, sc)
}

// enableJellyfinLibraries refreshes Seerr's library list from Jellyfin and
// enables each library. Seerr 3.5.0 replaced the old sync/enable query
// parameters on GET .../library with these endpoints, and its request validator
// rejects the old form. The sync answers 404 while Jellyfin has no libraries;
// setup then retries rather than finishing with none enabled.
func enableJellyfinLibraries(ctx context.Context, sc *seerrclient.Client) error {
	libraries, err := sc.PostList(ctx, "/api/v1/settings/jellyfin/library/sync", map[string]any{})
	if err != nil {
		return fmt.Errorf("jellyfin library sync: %w", err)
	}
	for _, lib := range libraries {
		id, ok := lib["id"].(string)
		if !ok || id == "" {
			continue
		}
		if err := sc.Put(ctx, fmt.Sprintf("/api/v1/settings/jellyfin/library/%s", id), map[string]any{"enabled": true}); err != nil {
			return fmt.Errorf("enabling jellyfin library %s: %w", id, err)
		}
	}
	return nil
}

func (r *SeerrConfigReconciler) establishSession(ctx context.Context, sc *seerrclient.Client, config *requestsv1alpha1.SeerrConfig) error {
	if ref := config.Spec.Connection.APIKeySecretRef; ref != nil {
		apiKey, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, *ref)
		if err != nil {
			return err
		}
		sc.SetAPIKey(apiKey)
		return nil
	}
	if err := r.authenticate(ctx, sc, config); err != nil {
		return err
	}
	apiKey, err := sc.GetAPIKey(ctx)
	if err != nil {
		return fmt.Errorf("get api key: %w", err)
	}
	sc.SetAPIKey(apiKey)
	return nil
}

func (r *SeerrConfigReconciler) authenticate(ctx context.Context, sc *seerrclient.Client, config *requestsv1alpha1.SeerrConfig) error {
	if config.Spec.PlexAuth != nil {
		plexToken, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, config.Spec.PlexAuth.TokenSecretRef)
		if err != nil {
			return fmt.Errorf("resolving plex token: %w", err)
		}
		return sc.AuthenticatePlex(ctx, plexToken)
	}
	if config.Spec.JellyfinAuth != nil {
		jfUsername, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, config.Spec.JellyfinAuth.UsernameSecretRef)
		if err != nil {
			return fmt.Errorf("resolving jellyfin username: %w", err)
		}
		jfPassword, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, config.Spec.JellyfinAuth.PasswordSecretRef)
		if err != nil {
			return fmt.Errorf("resolving jellyfin password: %w", err)
		}
		jfPort := 0
		if config.Spec.JellyfinAuth.Port != nil {
			jfPort = *config.Spec.JellyfinAuth.Port
		}
		useSsl := config.Spec.JellyfinAuth.UseSsl != nil && *config.Spec.JellyfinAuth.UseSsl
		configured, err := sc.MediaServerConfigured(ctx)
		if err != nil {
			return fmt.Errorf("reading media server state: %w", err)
		}
		return sc.AuthenticateJellyfin(ctx, jfUsername, jfPassword, config.Spec.JellyfinAuth.Hostname, jfPort,
			config.Spec.JellyfinAuth.UrlBase, useSsl, configured)
	}
	return fmt.Errorf("either jellyfinAuth or plexAuth must be configured")
}
