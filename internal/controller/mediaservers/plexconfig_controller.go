package mediaservers

import (
	"context"
	"fmt"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mediaserversv1alpha1 "github.com/kyleseneker/media-operator/api/mediaservers/v1alpha1"
	plexclient "github.com/kyleseneker/media-operator/internal/client/plex"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

type PlexConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=plexconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=plexconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *PlexConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var config mediaserversv1alpha1.PlexConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	token, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, config.Spec.Connection.TokenSecretRef)
	if err != nil {
		logger.Error(err, "failed to resolve Plex token")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	tlsCfg, err := engine.ResolveTLSConfig(ctx, r.Client, config.Namespace, config.Spec.Connection.TLS)
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	hc, err := engine.NewHTTPClient(config.Spec.Connection.URL, engine.AuthPlexToken, engine.WithPlexToken(token), engine.WithTLSConfig(tlsCfg), engine.WithAppLabel("plex"))
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonInvalidConfig, err.Error())
		return ctrl.Result{}, nil
	}
	pc := plexclient.NewClient(hc)

	if err := pc.Ping(ctx); err != nil {
		logger.Error(err, "Plex unreachable")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonAppUnreachable, err.Error())
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	var syncErrors []string

	// Reconcile preferences (server, transcoder, network are all Plex prefs)
	desiredPrefs := make(map[string]string)
	if s := config.Spec.Server; s != nil {
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
			desiredPrefs["ScanMyLibraryAutomatically"] = ctrlcommon.BoolTo01(*s.ScanMyLibraryAutomatically)
		}
		if s.ScanMyLibraryPeriodically != nil {
			desiredPrefs["ScanMyLibraryPeriodically"] = ctrlcommon.BoolTo01(*s.ScanMyLibraryPeriodically)
		}
		if s.LogDebug != nil {
			desiredPrefs["logDebug"] = ctrlcommon.BoolTo01(*s.LogDebug)
		}
	}
	if t := config.Spec.Transcoder; t != nil {
		if t.TranscodeHwRequested != nil {
			desiredPrefs["TranscoderHwRequested"] = ctrlcommon.BoolTo01(*t.TranscodeHwRequested)
		}
		if t.HardwareAccelerationType != "" {
			desiredPrefs["HardwareAccelerationType"] = t.HardwareAccelerationType
		}
		if t.MaxSimultaneousVideoTranscodes != nil {
			desiredPrefs["TranscoderMaxSimulTranscodes"] = strconv.Itoa(*t.MaxSimultaneousVideoTranscodes)
		}
		if t.TranscodeHwDecodingEnabled != nil {
			desiredPrefs["TranscodeHwDecodingEnabled"] = ctrlcommon.BoolTo01(*t.TranscodeHwDecodingEnabled)
		}
		if t.TranscodeHwEncodingEnabled != nil {
			desiredPrefs["TranscodeHwEncodingEnabled"] = ctrlcommon.BoolTo01(*t.TranscodeHwEncodingEnabled)
		}
		if t.TranscoderTempDirectory != "" {
			desiredPrefs["TranscoderTempDirectory"] = t.TranscoderTempDirectory
		}
	}
	if n := config.Spec.Network; n != nil {
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

	if len(desiredPrefs) > 0 {
		if err := pc.SetPreferences(ctx, desiredPrefs); err != nil {
			logger.Error(err, "failed to set preferences")
			syncErrors = append(syncErrors, fmt.Sprintf("preferences: %v", err))
		}
	}

	// Libraries (create-only)
	if len(config.Spec.Libraries) > 0 {
		existing, err := pc.ListLibraries(ctx)
		if err != nil {
			syncErrors = append(syncErrors, fmt.Sprintf("listLibraries: %v", err))
		} else {
			existingNames := make(map[string]bool)
			for _, lib := range existing {
				if name, ok := lib["title"].(string); ok {
					existingNames[name] = true
				}
			}
			for _, lib := range config.Spec.Libraries {
				if existingNames[lib.Name] {
					continue
				}
				agent := lib.Agent
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
				scanner := lib.Scanner
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
				lang := lib.Language
				if lang == "" {
					lang = "en"
				}
				if err := pc.CreateLibrary(ctx, lib.Name, lib.Type, agent, scanner, lang, lib.Paths); err != nil {
					syncErrors = append(syncErrors, fmt.Sprintf("createLibrary(%s): %v", lib.Name, err))
				}
			}
		}
	}

	if len(syncErrors) > 0 {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, false, engine.ReasonSyncFailed, fmt.Sprintf("partial sync: %v", syncErrors))
	} else {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, true, engine.ReasonSynced, "all configuration synced")
	}
	return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
}

func (r *PlexConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mediaserversv1alpha1.PlexConfig{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			return ctrlcommon.FindConfigsBySecret(ctx, r.Client, obj, &mediaserversv1alpha1.PlexConfigList{}, func(list *mediaserversv1alpha1.PlexConfigList) []reconcile.Request {
				var reqs []reconcile.Request
				for _, c := range list.Items {
					if c.Spec.Connection.TokenSecretRef.Name == obj.GetName() {
						reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&c)})
					}
				}
				return reqs
			})
		})).
		Named("plexconfig").
		Complete(r)
}
