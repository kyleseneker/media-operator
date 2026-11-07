package mediaservers

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	mediaserversv1alpha1 "github.com/kyleseneker/media-operator/api/mediaservers/v1alpha1"
	plexclient "github.com/kyleseneker/media-operator/internal/client/plex"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

type PlexConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=plexconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=plexconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=media-operator.dev,resources=plexconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *PlexConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var config mediaserversv1alpha1.PlexConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if done, after := ctrlcommon.HandleLifecycle(ctx, r.Client, r.Recorder, &config, nil); done {
		return ctrl.Result{RequeueAfter: after}, nil
	}

	token, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, config.Spec.Connection.TokenSecretRef)
	if err != nil {
		logger.Error(err, "failed to resolve Plex token")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Client, &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	tlsCfg, err := engine.ResolveTLSConfig(ctx, r.Client, config.Namespace, config.Spec.Connection.TLS)
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Client, &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	hc, err := engine.NewHTTPClient(config.Spec.Connection.URL, engine.AuthPlexToken, engine.WithPlexToken(token), engine.WithTLSConfig(tlsCfg), engine.WithAppLabel("plex"))
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Client, &config, engine.ReasonInvalidConfig, err.Error())
		return ctrl.Result{}, nil
	}
	pc := plexclient.NewClient(hc)

	if err := pc.Ping(ctx); err != nil {
		logger.Error(err, "Plex unreachable")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Client, &config, engine.ReasonAppUnreachable, err.Error())
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	writes := &engine.Writes{App: "plex", Observe: ctrlcommon.ObserveOnly(config.Spec.Reconcile)}
	syncErrors := reconcilePlexPreferences(ctx, pc, writes, string(config.UID), buildPlexPreferences(config.Spec))
	syncErrors = append(syncErrors, reconcilePlexLibraries(ctx, pc, writes, config.Spec.Libraries)...)

	ctrlcommon.UpdateSyncStatus(ctx, r.Client, &config, writes.Observe, len(writes.Drift()) > 0, syncErrors)
	return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
}

// reconcilePlexLibraries creates the declared libraries Plex does not have.
// Libraries are create-only: an existing one is never changed.
func reconcilePlexLibraries(
	ctx context.Context, pc *plexclient.Client, w *engine.Writes, libraries []mediaserversv1alpha1.PlexLibrary,
) []string {
	if len(libraries) == 0 {
		return nil
	}
	existing, err := pc.ListLibraries(ctx)
	if err != nil {
		return []string{fmt.Sprintf("listLibraries: %v", err)}
	}
	existingNames := make(map[string]bool)
	for _, lib := range existing {
		if name, ok := lib["title"].(string); ok {
			existingNames[name] = true
		}
	}
	var syncErrors []string
	for _, lib := range libraries {
		if existingNames[lib.Name] {
			continue
		}
		agent, scanner, lang := plexLibraryDefaults(lib)
		err := w.Apply("libraries", lib.Name, func() error {
			return pc.CreateLibrary(ctx, lib.Name, lib.Type, agent, scanner, lang, lib.Paths)
		})
		if err != nil {
			syncErrors = append(syncErrors, fmt.Sprintf("createLibrary(%s): %v", lib.Name, err))
		}
	}
	return syncErrors
}

// plexLibraryDefaults fills in the agent, scanner and language Plex needs for a
// library type when the spec leaves them out.
func plexLibraryDefaults(lib mediaserversv1alpha1.PlexLibrary) (agent, scanner, lang string) {
	agent, scanner, lang = lib.Agent, lib.Scanner, lib.Language
	if agent == "" {
		switch lib.Type {
		case "movie":
			agent = "tv.plex.agents.movie"
		case "show":
			agent = "tv.plex.agents.series"
		case "artist":
			agent = "tv.plex.agents.music"
		}
	}
	if scanner == "" {
		switch lib.Type {
		case "movie":
			scanner = "Plex Movie"
		case "show":
			scanner = "Plex TV Series"
		case "artist":
			scanner = "Plex Music"
		}
	}
	if lang == "" {
		// Plex takes a locale; a bare "en" is rejected.
		lang = "en-US"
	}
	return agent, scanner, lang
}

func (r *PlexConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mediaserversv1alpha1.PlexConfig{}, builder.WithPredicates(ctrlcommon.ConfigChangedPredicate())).
		Watches(&corev1.Secret{}, ctrlcommon.EnqueueForSecret(r.Client, &mediaserversv1alpha1.PlexConfigList{})).
		Named("plexconfig").
		Complete(r)
}

func buildPlexPreferences(spec mediaserversv1alpha1.PlexConfigSpec) map[string]string {
	desiredPrefs := make(map[string]string)
	if s := spec.Server; s != nil {
		if s.FriendlyName != "" {
			desiredPrefs["FriendlyName"] = s.FriendlyName
		}
		if s.Language != "" {
			desiredPrefs["language"] = s.Language
		}
		if s.AutoEmptyTrash != nil {
			desiredPrefs["autoEmptyTrash"] = ctrlcommon.BoolTo01(*s.AutoEmptyTrash)
		}
		if s.ScanMyLibraryAutomatically != nil {
			desiredPrefs["FSEventLibraryUpdatesEnabled"] = ctrlcommon.BoolTo01(*s.ScanMyLibraryAutomatically)
		}
		if s.ScanMyLibraryPeriodically != nil {
			desiredPrefs["ScheduledLibraryUpdatesEnabled"] = ctrlcommon.BoolTo01(*s.ScanMyLibraryPeriodically)
		}
		if s.LogDebug != nil {
			desiredPrefs["logDebug"] = ctrlcommon.BoolTo01(*s.LogDebug)
		}
		if s.EnableRemoteAccess != nil {
			desiredPrefs["PublishServerOnPlexOnlineKey"] = ctrlcommon.BoolTo01(*s.EnableRemoteAccess)
		}
	}
	if t := spec.Transcoder; t != nil {
		if t.TranscodeHwRequested != nil {
			desiredPrefs["HardwareAcceleratedCodecs"] = ctrlcommon.BoolTo01(*t.TranscodeHwRequested)
		}
		if t.HardwareAccelerationType != "" {
			desiredPrefs["HardwareAccelerationType"] = t.HardwareAccelerationType
		}
		if t.MaxSimultaneousVideoTranscodes != nil {
			desiredPrefs["TranscodeCountLimit"] = strconv.Itoa(*t.MaxSimultaneousVideoTranscodes)
		}
		if t.TranscodeHwDecodingEnabled != nil {
			desiredPrefs["TranscodeHwDecodingEnabled"] = ctrlcommon.BoolTo01(*t.TranscodeHwDecodingEnabled)
		}
		if t.TranscodeHwEncodingEnabled != nil {
			desiredPrefs["HardwareAcceleratedEncoders"] = ctrlcommon.BoolTo01(*t.TranscodeHwEncodingEnabled)
		}
		if t.TranscoderTempDirectory != "" {
			desiredPrefs["TranscoderTempDirectory"] = t.TranscoderTempDirectory
		}
	}
	if n := spec.Network; n != nil {
		if n.SecureConnections != nil {
			desiredPrefs["secureConnections"] = strconv.Itoa(*n.SecureConnections)
		}
		if n.CustomServerAccessUrls != "" {
			desiredPrefs["customConnections"] = n.CustomServerAccessUrls
		}
		if n.AllowedNetworks != "" {
			desiredPrefs["allowedNetworks"] = n.AllowedNetworks
		}
		if n.EnableIPv6 != nil {
			desiredPrefs["EnableIPv6"] = ctrlcommon.BoolTo01(*n.EnableIPv6)
		}
	}
	return desiredPrefs
}

// plexPreferenceChanges returns the declared preferences whose value differs
// from Plex's, and those Plex does not list and so cannot be compared.
func plexPreferenceChanges(desired, current map[string]string) (map[string]string, []string) {
	changes := map[string]string{}
	var unknown []string
	for key, value := range desired {
		have, ok := current[key]
		switch {
		case !ok:
			unknown = append(unknown, key)
		case have != value:
			changes[key] = value
		}
	}
	slices.Sort(unknown)
	return changes, unknown
}

// reconcilePlexPreferences writes the declared preferences that differ, one
// per request, and returns what could not be read or written. Observe mode
// compares only the preferences Plex lists: the others cannot be read back.
func reconcilePlexPreferences(
	ctx context.Context, pc *plexclient.Client, w *engine.Writes, owner string, desiredPrefs map[string]string,
) []string {
	logger := log.FromContext(ctx)
	var syncErrors []string
	if len(desiredPrefs) > 0 {
		current, err := pc.GetSettings(ctx)
		if err != nil {
			syncErrors = append(syncErrors, fmt.Sprintf("reading preferences: %v", err))
		} else {
			changes, unlisted := plexPreferenceChanges(desiredPrefs, current)
			// Some settings (secureConnections, the hardware switches) are accepted
			// but never listed, so what was last written is remembered instead.
			remember := func(key string) string { return fmt.Sprintf("plex/%s/pref/%s", owner, key) }
			for _, key := range unlisted {
				if !w.Observe && reconciler.SecretsChangedSince(remember(key), desiredPrefs[key]) {
					changes[key] = desiredPrefs[key]
				}
			}
			// One key per request: Plex rejects a whole update over one setting it
			// does not have, which would block every other setting.
			for _, key := range slices.Sorted(maps.Keys(changes)) {
				err := w.Apply("settings", key, func() error {
					if err := pc.SetPreferences(ctx, map[string]string{key: changes[key]}); err != nil {
						return err
					}
					if slices.Contains(unlisted, key) {
						reconciler.RecordSecrets(remember(key), changes[key])
					}
					return nil
				})
				if err != nil {
					logger.Error(err, "Plex rejected a preference", "setting", key)
					syncErrors = append(syncErrors, fmt.Sprintf("preference %s: Plex rejected it (%v); it may need Plex Pass or not exist in this version", key, err))
				}
			}
		}
	}

	return syncErrors
}
