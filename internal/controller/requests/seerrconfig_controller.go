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
	seerrclient "github.com/kyleseneker/media-operator/internal/client/seerr"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

// SeerrConfigReconciler reconciles a SeerrConfig object.
type SeerrConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=seerrconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=seerrconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *SeerrConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var config requestsv1alpha1.SeerrConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
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

	// Authenticate via Jellyfin or Plex depending on which auth is configured
	authenticate := func() error {
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
			return sc.AuthenticateJellyfin(ctx, jfUsername, jfPassword, config.Spec.JellyfinAuth.Hostname, jfPort)
		}
		return fmt.Errorf("either jellyfinAuth or plexAuth must be configured")
	}

	if err := authenticate(); err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	apiKey, err := sc.GetAPIKey(ctx)
	if err != nil {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, false, engine.ReasonSyncFailed, fmt.Sprintf("get api key: %v", err))
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}
	sc.SetAPIKey(apiKey)

	if !isInitialized {
		var initErrors []string

		// Sync media server libraries
		if config.Spec.JellyfinAuth != nil {
			if _, err := sc.Post(ctx, "/api/v1/settings/jellyfin/library/sync", map[string]interface{}{}); err != nil {
				logger.Error(err, "failed to sync Jellyfin libraries")
				initErrors = append(initErrors, fmt.Sprintf("jellyfin library sync: %v", err))
			}
		}
		if config.Spec.PlexAuth != nil {
			if _, err := sc.Post(ctx, "/api/v1/settings/plex/library/sync", map[string]interface{}{}); err != nil {
				logger.Error(err, "failed to sync Plex libraries")
				initErrors = append(initErrors, fmt.Sprintf("plex library sync: %v", err))
			}
		}

		// Configure Sonarr if specified
		if config.Spec.Sonarr != nil {
			if err := r.reconcileSonarrConnection(ctx, sc, &config, true); err != nil {
				logger.Error(err, "failed to configure Sonarr during initialization")
				initErrors = append(initErrors, fmt.Sprintf("sonarr init: %v", err))
			}
		}

		// Configure Radarr if specified
		if config.Spec.Radarr != nil {
			if err := r.reconcileRadarrConnection(ctx, sc, &config, true); err != nil {
				logger.Error(err, "failed to configure Radarr during initialization")
				initErrors = append(initErrors, fmt.Sprintf("radarr init: %v", err))
			}
		}

		// Update main settings if specified
		if config.Spec.Main != nil {
			if err := reconcileSeerrMainSettings(ctx, sc, config.Spec.Main); err != nil {
				logger.Error(err, "failed to configure main settings during initialization")
				initErrors = append(initErrors, fmt.Sprintf("main settings init: %v", err))
			}
		}

		if len(initErrors) > 0 {
			ctrlcommon.UpdateStatus(ctx, r.Status(), &config, false, engine.ReasonSyncFailed, fmt.Sprintf("initialization failed: %v", initErrors))
			return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
		}

		// Complete initialization
		if _, err := sc.Post(ctx, "/api/v1/settings/initialize", map[string]interface{}{}); err != nil {
			logger.Error(err, "failed to initialize Seerr")
			ctrlcommon.UpdateStatus(ctx, r.Status(), &config, false, engine.ReasonSyncFailed, fmt.Sprintf("initialization: %v", err))
			return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
		}

		logger.Info("Seerr initialization completed")
	}

	initialized := true
	config.Status.Initialized = &initialized

	var syncErrors []string

	// Reconcile Sonarr connection
	if config.Spec.Sonarr != nil {
		if err := r.reconcileSonarrConnection(ctx, sc, &config, false); err != nil {
			logger.Error(err, "failed to reconcile Sonarr connection")
			syncErrors = append(syncErrors, fmt.Sprintf("sonarr: %v", err))
		}
	}

	// Reconcile Radarr connection
	if config.Spec.Radarr != nil {
		if err := r.reconcileRadarrConnection(ctx, sc, &config, false); err != nil {
			logger.Error(err, "failed to reconcile Radarr connection")
			syncErrors = append(syncErrors, fmt.Sprintf("radarr: %v", err))
		}
	}

	// Reconcile main settings
	if config.Spec.Main != nil {
		if err := reconcileSeerrMainSettings(ctx, sc, config.Spec.Main); err != nil {
			logger.Error(err, "failed to reconcile main settings")
			syncErrors = append(syncErrors, fmt.Sprintf("main: %v", err))
		}
	}

	// Reconcile notification agents
	for _, agent := range config.Spec.Notifications {
		if err := reconcileSeerrNotificationAgent(ctx, sc, agent); err != nil {
			logger.Error(err, "failed to reconcile notification agent", "agent", agent.Agent)
			syncErrors = append(syncErrors, fmt.Sprintf("notification(%s): %v", agent.Agent, err))
		}
	}

	if len(syncErrors) > 0 {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, false, engine.ReasonSyncFailed, fmt.Sprintf("partial sync failure: %v", syncErrors))
	} else {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, true, engine.ReasonSynced, "all configuration sections synced")
	}

	return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
}

func (r *SeerrConfigReconciler) reconcileSonarrConnection(ctx context.Context, sc *seerrclient.Client, config *requestsv1alpha1.SeerrConfig, createOnly bool) error {
	sonarrAPIKey, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, config.Spec.Sonarr.APIKeySecretRef)
	if err != nil {
		return fmt.Errorf("resolving sonarr API key: %w", err)
	}

	payload := buildServicePayload(config.Spec.Sonarr, sonarrAPIKey)

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
			return sc.Put(ctx, fmt.Sprintf("/api/v1/settings/sonarr/%d", int(id)), payload)
		}
	}

	_, err = sc.Post(ctx, "/api/v1/settings/sonarr", payload)
	return err
}

func (r *SeerrConfigReconciler) reconcileRadarrConnection(ctx context.Context, sc *seerrclient.Client, config *requestsv1alpha1.SeerrConfig, createOnly bool) error {
	radarrAPIKey, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, config.Spec.Radarr.APIKeySecretRef)
	if err != nil {
		return fmt.Errorf("resolving radarr API key: %w", err)
	}

	payload := buildServicePayload(config.Spec.Radarr, radarrAPIKey)

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
			return sc.Put(ctx, fmt.Sprintf("/api/v1/settings/radarr/%d", int(id)), payload)
		}
	}

	_, err = sc.Post(ctx, "/api/v1/settings/radarr", payload)
	return err
}

func buildServicePayload(svc *requestsv1alpha1.SeerrServiceConnection, apiKey string) map[string]interface{} {
	payload := map[string]interface{}{
		"name":     svc.Name,
		"hostname": svc.Hostname,
		"apiKey":   apiKey,
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

func reconcileSeerrMainSettings(ctx context.Context, sc *seerrclient.Client, main *requestsv1alpha1.SeerrMain) error {
	payload := map[string]interface{}{}
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

	_, err := sc.Post(ctx, "/api/v1/settings/main", payload)
	return err
}

func reconcileSeerrNotificationAgent(ctx context.Context, sc *seerrclient.Client, agent requestsv1alpha1.SeerrNotificationAgent) error {
	payload := map[string]interface{}{}
	if agent.Enabled != nil {
		payload["enabled"] = *agent.Enabled
	}
	if agent.Types != nil {
		payload["types"] = *agent.Types
	}
	if len(agent.Options) > 0 {
		opts := make(map[string]interface{}, len(agent.Options))
		for k, v := range agent.Options {
			opts[k] = v
		}
		payload["options"] = opts
	}

	_, err := sc.Post(ctx, fmt.Sprintf("/api/v1/settings/notifications/%s", agent.Agent), payload)
	return err
}

// seerrReferencesSecret returns true if the SeerrConfig references the named secret.
func seerrReferencesSecret(c *requestsv1alpha1.SeerrConfig, secretName string) bool {
	if c.Spec.JellyfinAuth != nil {
		if c.Spec.JellyfinAuth.UsernameSecretRef.Name == secretName || c.Spec.JellyfinAuth.PasswordSecretRef.Name == secretName {
			return true
		}
	}
	if c.Spec.PlexAuth != nil && c.Spec.PlexAuth.TokenSecretRef.Name == secretName {
		return true
	}
	if c.Spec.Sonarr != nil && c.Spec.Sonarr.APIKeySecretRef.Name == secretName {
		return true
	}
	if c.Spec.Radarr != nil && c.Spec.Radarr.APIKeySecretRef.Name == secretName {
		return true
	}
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *SeerrConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&requestsv1alpha1.SeerrConfig{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			return ctrlcommon.FindConfigsBySecret(ctx, r.Client, obj, &requestsv1alpha1.SeerrConfigList{}, func(list *requestsv1alpha1.SeerrConfigList) []reconcile.Request {
				var reqs []reconcile.Request
				for _, c := range list.Items {
					if seerrReferencesSecret(&c, obj.GetName()) {
						reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&c)})
					}
				}
				return reqs
			})
		})).
		Named("seerrconfig").
		Complete(r)
}
