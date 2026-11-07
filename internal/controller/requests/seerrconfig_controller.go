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

	lifecycle := ctrlcommon.Lifecycle{Remove: func(ctx context.Context) error { return r.cleanup(ctx, &config) }, Prune: true}
	if done, after := ctrlcommon.HandleLifecycle(ctx, r.Client, r.Recorder, &config, lifecycle); done {
		return ctrl.Result{RequeueAfter: after}, nil
	}

	sc, reason, err := r.seerrClient(ctx, &config)
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Client, &config, reason, err.Error())
		if reason == engine.ReasonInvalidConfig {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Check initialization state
	isInitialized, err := sc.IsInitialized(ctx)
	if err != nil {
		logger.Error(err, "Seerr unreachable")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Client, &config, engine.ReasonAppUnreachable, err.Error())
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	writes := &engine.Writes{App: "seerr", Observe: ctrlcommon.ObserveOnly(config.Spec.Reconcile)}
	owned := engine.NewOwnership("seerr", config.Status.ManagedResources)
	if !isInitialized && writes.Observe {
		// Signing in to a Seerr that has not been set up configures its media
		// server, so observe mode reports the setup as missing and stops.
		config.Status.Initialized = &isInitialized
		err := writes.Compare("settings", "initialization", nil, map[string]any{"initialized": true})
		ctrlcommon.UpdateObservationStatus(ctx, r.Client, &config, writes.Drift(), err)
		return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
	}

	if err := r.establishSession(ctx, sc, &config); err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Client, &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	if !isInitialized {
		if err := r.setup(ctx, sc, writes, owned, &config); err != nil {
			ctrlcommon.UpdateStatus(ctx, r.Client, &config, false, engine.ReasonSyncFailed, err.Error())
			return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
		}
	}

	initialized := true
	config.Status.Initialized = &initialized

	var syncErrors []string

	if config.Spec.JellyfinAuth != nil {
		if err := reconcileSeerrJellyfin(ctx, sc, writes, config.Spec.JellyfinAuth); err != nil {
			logger.Error(err, "failed to reconcile Jellyfin server")
			syncErrors = append(syncErrors, fmt.Sprintf("jellyfin: %v", err))
		}
	}
	if config.Spec.PlexAuth != nil {
		if err := reconcileSeerrPlex(ctx, sc, writes, config.Spec.PlexAuth); err != nil {
			logger.Error(err, "failed to reconcile Plex server")
			syncErrors = append(syncErrors, fmt.Sprintf("plex: %v", err))
		}
	}

	for _, svc := range []struct {
		kind      string
		conn      *requestsv1alpha1.SeerrServiceConnection
		reconcile func(context.Context, *seerrclient.Client, *engine.Writes, *engine.Ownership, *requestsv1alpha1.SeerrConfig) error
	}{
		{"sonarr", config.Spec.Sonarr, r.reconcileSonarrConnection},
		{"radarr", config.Spec.Radarr, r.reconcileRadarrConnection},
	} {
		if svc.conn == nil {
			continue
		}
		err := svc.reconcile(ctx, sc, writes, owned, &config)
		if err == nil && ctrlcommon.PruneEnabled(config.Spec.Reconcile) {
			err = pruneSeerrService(ctx, sc, writes, owned, svc.kind, svc.conn.Name)
		}
		if err != nil {
			logger.Error(err, "failed to reconcile connection", "service", svc.kind)
			syncErrors = append(syncErrors, fmt.Sprintf("%s: %v", svc.kind, err))
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

	ctrlcommon.UpdateStatusManaged(&config, owned.Managed())
	ctrlcommon.EmitPruneEvents(r.Recorder, &config, owned.Pruned())
	ctrlcommon.UpdateSyncStatus(ctx, r.Client, &config, writes.Observe, writes.Drift(), syncErrors)

	return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
}

// seerrClient builds the client for a config, or returns the status reason it
// can't.
func (r *SeerrConfigReconciler) seerrClient(ctx context.Context, config *requestsv1alpha1.SeerrConfig) (*seerrclient.Client, string, error) {
	tlsCfg, err := engine.ResolveTLSConfig(ctx, r.Client, config.Namespace, config.Spec.Connection.TLS)
	if err != nil {
		return nil, engine.ReasonSecretNotFound, err
	}
	hc, err := engine.NewHTTPClient(config.Spec.Connection.URL, engine.AuthSession, engine.WithTLSConfig(tlsCfg), engine.WithAppLabel("seerr"))
	if err != nil {
		return nil, engine.ReasonInvalidConfig, err
	}
	return seerrclient.NewClient(hc), "", nil
}

// pruneSeerrService deletes the connections of a kind the operator created other
// than the declared one.
func pruneSeerrService(ctx context.Context, sc *seerrclient.Client, w *engine.Writes, owned *engine.Ownership, kind, declared string) error {
	names, ids, err := seerrServices(ctx, sc, kind)
	if err != nil {
		return err
	}
	return w.Prune(owned, kind, []string{declared}, names, func(name string) error {
		return sc.Delete(ctx, fmt.Sprintf("/api/v1/settings/%s/%d", kind, ids[name]))
	})
}

// cleanup deletes the connections the operator created, for deletionPolicy delete.
func (r *SeerrConfigReconciler) cleanup(ctx context.Context, config *requestsv1alpha1.SeerrConfig) error {
	sc, _, err := r.seerrClient(ctx, config)
	if err != nil {
		return err
	}
	if err := r.establishSession(ctx, sc, config); err != nil {
		return err
	}
	owned := engine.NewOwnership("seerr", config.Status.ManagedResources)
	for _, kind := range []string{"sonarr", "radarr"} {
		names, ids, err := seerrServices(ctx, sc, kind)
		if err != nil {
			return err
		}
		err = owned.Cleanup(kind, names, func(name string) error {
			return sc.Delete(ctx, fmt.Sprintf("/api/v1/settings/%s/%d", kind, ids[name]))
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func seerrServices(ctx context.Context, sc *seerrclient.Client, kind string) ([]string, map[string]int, error) {
	servers, err := sc.GetList(ctx, "/api/v1/settings/"+kind)
	if err != nil {
		return nil, nil, fmt.Errorf("listing %s connections: %w", kind, err)
	}
	var names []string
	ids := map[string]int{}
	for _, s := range servers {
		name, _ := s["name"].(string)
		if id, ok := s["id"].(float64); ok && name != "" {
			names = append(names, name)
			ids[name] = int(id)
		}
	}
	return names, ids, nil
}

// setup runs Seerr's first-time setup: the media server's libraries, the Sonarr
// and Radarr connections and the main settings, then marks Seerr initialized.
func (r *SeerrConfigReconciler) setup(
	ctx context.Context, sc *seerrclient.Client, writes *engine.Writes, owned *engine.Ownership, config *requestsv1alpha1.SeerrConfig,
) error {
	logger := log.FromContext(ctx)
	var initErrors []string

	// Sync media server libraries
	if config.Spec.JellyfinAuth != nil {
		if err := syncJellyfinLibraries(ctx, sc, writes, config.Spec.JellyfinAuth); err != nil {
			logger.Error(err, "failed to configure Jellyfin libraries")
			initErrors = append(initErrors, err.Error())
		}
	}
	if config.Spec.PlexAuth != nil {
		if err := syncPlexLibraries(ctx, sc, writes, config.Spec.PlexAuth); err != nil {
			logger.Error(err, "failed to configure Plex libraries")
			initErrors = append(initErrors, err.Error())
		}
	}

	if config.Spec.Sonarr != nil {
		if err := r.reconcileSonarrConnection(ctx, sc, writes, owned, config); err != nil {
			logger.Error(err, "failed to configure Sonarr during initialization")
			initErrors = append(initErrors, fmt.Sprintf("sonarr init: %v", err))
		}
	}
	if config.Spec.Radarr != nil {
		if err := r.reconcileRadarrConnection(ctx, sc, writes, owned, config); err != nil {
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

// reconcileSonarrConnection updates the Sonarr connection with the declared
// name, or creates it. Setup uses it too: a setup that failed partway runs again
// from the start, and Seerr appends a connection on every POST.
func (r *SeerrConfigReconciler) reconcileSonarrConnection(
	ctx context.Context, sc *seerrclient.Client, w *engine.Writes, owned *engine.Ownership, config *requestsv1alpha1.SeerrConfig,
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
			return w.Apply("sonarr", name, func() error {
				return sc.Put(ctx, fmt.Sprintf("/api/v1/settings/sonarr/%d", int(id)), payload)
			})
		}
	}

	return w.Apply("sonarr", config.Spec.Sonarr.Name, func() error {
		if _, err := sc.Post(ctx, "/api/v1/settings/sonarr", payload); err != nil {
			return err
		}
		owned.Created("sonarr", config.Spec.Sonarr.Name)
		return nil
	})
}

func (r *SeerrConfigReconciler) reconcileRadarrConnection(
	ctx context.Context, sc *seerrclient.Client, w *engine.Writes, owned *engine.Ownership, config *requestsv1alpha1.SeerrConfig,
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
			return w.Apply("radarr", name, func() error {
				return sc.Put(ctx, fmt.Sprintf("/api/v1/settings/radarr/%d", int(id)), payload)
			})
		}
	}

	return w.Apply("radarr", config.Spec.Radarr.Name, func() error {
		if _, err := sc.Post(ctx, "/api/v1/settings/radarr", payload); err != nil {
			return err
		}
		owned.Created("radarr", config.Spec.Radarr.Name)
		return nil
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

func reconcileSeerrMainSettings(ctx context.Context, sc *seerrclient.Client, w *engine.Writes, main *requestsv1alpha1.SeerrMain) error {
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
	return w.Apply("settings", "main", func() error {
		_, err := sc.Post(ctx, "/api/v1/settings/main", payload)
		return err
	})
}

func reconcileSeerrNotificationAgent(
	ctx context.Context, sc *seerrclient.Client, w *engine.Writes, agent requestsv1alpha1.SeerrNotificationAgent,
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
	return w.Apply("notifications", agent.Agent, func() error {
		_, err := sc.Post(ctx, path, payload)
		return err
	})
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

func syncJellyfinLibraries(ctx context.Context, sc *seerrclient.Client, w *engine.Writes, auth *requestsv1alpha1.SeerrJellyfinAuth) error {
	if err := reconcileSeerrJellyfin(ctx, sc, w, auth); err != nil {
		return err
	}
	return enableLibraries(ctx, sc, "jellyfin")
}

// reconcileSeerrJellyfin keeps Seerr's Jellyfin server at the declared address.
// Seerr stores the host as "ip". A "hostname" key is kept verbatim and, on the
// next start, Seerr's settings migration parses it as a URL and replaces the
// port with 80 or 443.
func reconcileSeerrJellyfin(ctx context.Context, sc *seerrclient.Client, w *engine.Writes, auth *requestsv1alpha1.SeerrJellyfinAuth) error {
	port := 0
	if auth.Port != nil {
		port = *auth.Port
	}
	payload := map[string]any{
		"ip":      auth.Hostname,
		"port":    port,
		"useSsl":  auth.UseSsl != nil && *auth.UseSsl,
		"urlBase": auth.UrlBase,
	}
	current, err := sc.Get(ctx, "/api/v1/settings/jellyfin")
	if err != nil {
		return fmt.Errorf("reading jellyfin settings: %w", err)
	}
	if !seerrDiffers(current, payload) {
		return nil
	}
	return w.Apply("settings", "jellyfin", func() error {
		_, err := sc.Post(ctx, "/api/v1/settings/jellyfin", payload)
		return err
	})
}

func syncPlexLibraries(ctx context.Context, sc *seerrclient.Client, w *engine.Writes, auth *requestsv1alpha1.SeerrPlexAuth) error {
	if err := reconcileSeerrPlex(ctx, sc, w, auth); err != nil {
		return err
	}
	return enableLibraries(ctx, sc, "plex")
}

// reconcileSeerrPlex keeps Seerr's Plex server at the declared address. Seerr
// connects to it to check the address before saving.
func reconcileSeerrPlex(ctx context.Context, sc *seerrclient.Client, w *engine.Writes, auth *requestsv1alpha1.SeerrPlexAuth) error {
	port := 32400
	if auth.Port != nil {
		port = *auth.Port
	}
	payload := map[string]any{
		"ip":     auth.Hostname,
		"port":   port,
		"useSsl": auth.UseSsl != nil && *auth.UseSsl,
	}
	current, err := sc.Get(ctx, "/api/v1/settings/plex")
	if err != nil {
		return fmt.Errorf("reading plex settings: %w", err)
	}
	if !seerrDiffers(current, payload) {
		return nil
	}
	return w.Apply("settings", "plex", func() error {
		_, err := sc.Post(ctx, "/api/v1/settings/plex", payload)
		return err
	})
}

// enableLibraries refreshes Seerr's library list from its media server and
// enables each library. Seerr 3.5.0 replaced the old sync/enable query
// parameters on GET .../library with these endpoints, and its request validator
// rejects the old form. The sync answers 404 while the server has no libraries;
// setup then retries rather than finishing with none enabled.
func enableLibraries(ctx context.Context, sc *seerrclient.Client, server string) error {
	libraries, err := sc.PostList(ctx, fmt.Sprintf("/api/v1/settings/%s/library/sync", server), map[string]any{})
	if err != nil {
		return fmt.Errorf("%s library sync: %w", server, err)
	}
	for _, lib := range libraries {
		id, ok := lib["id"].(string)
		if !ok || id == "" {
			continue
		}
		if err := sc.Put(ctx, fmt.Sprintf("/api/v1/settings/%s/library/%s", server, id), map[string]any{"enabled": true}); err != nil {
			return fmt.Errorf("enabling %s library %s: %w", server, id, err)
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
