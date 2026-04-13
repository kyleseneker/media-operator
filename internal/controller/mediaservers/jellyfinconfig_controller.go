package mediaservers

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

	mediaserversv1alpha1 "github.com/kyleseneker/media-operator/api/mediaservers/v1alpha1"
	jellyfinclient "github.com/kyleseneker/media-operator/internal/client/jellyfin"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

// JellyfinConfigReconciler reconciles a JellyfinConfig object.
type JellyfinConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=jellyfinconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=jellyfinconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *JellyfinConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var config mediaserversv1alpha1.JellyfinConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Resolve admin credentials
	username, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, config.Spec.AdminUser.UsernameSecretRef)
	if err != nil {
		logger.Error(err, "failed to resolve admin username secret")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	password, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, config.Spec.AdminUser.PasswordSecretRef)
	if err != nil {
		logger.Error(err, "failed to resolve admin password secret")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	tlsCfg, err := engine.ResolveTLSConfig(ctx, r.Client, config.Namespace, config.Spec.Connection.TLS)
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	hc, err := engine.NewHTTPClient(config.Spec.Connection.URL, engine.AuthMediaBrowser, engine.WithTLSConfig(tlsCfg), engine.WithAppLabel("jellyfin"))
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonInvalidConfig, err.Error())
		return ctrl.Result{}, nil
	}
	jf := jellyfinclient.NewClient(hc)

	// Check if setup wizard is complete
	setupComplete, err := jf.IsSetupComplete(ctx)
	if err != nil {
		logger.Error(err, "Jellyfin unreachable")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonAppUnreachable, err.Error())
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	// Run setup wizard if not complete
	if !setupComplete {
		serverName := ""
		metadataLang := "en"
		countryCode := "US"
		if config.Spec.Server != nil {
			if config.Spec.Server.ServerName != "" {
				serverName = config.Spec.Server.ServerName
			}
			if config.Spec.Server.PreferredMetadataLanguage != "" {
				metadataLang = config.Spec.Server.PreferredMetadataLanguage
			}
			if config.Spec.Server.MetadataCountryCode != "" {
				countryCode = config.Spec.Server.MetadataCountryCode
			}
		}

		if err := jf.RunSetupWizard(ctx, username, password, serverName, metadataLang, countryCode); err != nil {
			logger.Error(err, "failed to run setup wizard")
			ctrlcommon.UpdateStatus(ctx, r.Status(), &config, false, engine.ReasonSyncFailed, fmt.Sprintf("setup wizard: %v", err))
			return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
		}
		logger.Info("Jellyfin setup wizard completed")
	}

	initialized := true
	config.Status.Initialized = &initialized

	// Authenticate
	if err := jf.Authenticate(ctx, username, password); err != nil {
		logger.Error(err, "Jellyfin authentication failed")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonAppUnreachable, fmt.Sprintf("authentication: %v", err))
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	var syncErrors []string

	// Reconcile encoding settings
	if config.Spec.Encoding != nil {
		if err := reconcileJellyfinEncoding(ctx, jf, config.Spec.Encoding); err != nil {
			logger.Error(err, "failed to reconcile encoding settings")
			syncErrors = append(syncErrors, fmt.Sprintf("encoding: %v", err))
		}
	}

	// Reconcile server settings
	if config.Spec.Server != nil {
		if err := reconcileJellyfinServer(ctx, jf, config.Spec.Server); err != nil {
			logger.Error(err, "failed to reconcile server settings")
			syncErrors = append(syncErrors, fmt.Sprintf("server: %v", err))
		}
	}

	// Reconcile libraries (create-only)
	for _, lib := range config.Spec.Libraries {
		if err := reconcileJellyfinLibrary(ctx, jf, lib); err != nil {
			logger.Error(err, "failed to reconcile library", "name", lib.Name)
			syncErrors = append(syncErrors, fmt.Sprintf("library(%s): %v", lib.Name, err))
		}
	}

	if len(syncErrors) > 0 {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, false, engine.ReasonSyncFailed, fmt.Sprintf("partial sync failure: %v", syncErrors))
	} else {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, true, engine.ReasonSynced, "all configuration sections synced")
	}

	return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
}

func reconcileJellyfinEncoding(ctx context.Context, jf *jellyfinclient.Client, encoding *mediaserversv1alpha1.JellyfinEncoding) error {
	current, err := jf.GetConfig(ctx, "/System/Configuration/encoding")
	if err != nil {
		return fmt.Errorf("getting current encoding config: %w", err)
	}

	merged, changed, err := reconciler.MergeDesiredOverCurrent(current, encoding)
	if err != nil {
		return fmt.Errorf("merging encoding config: %w", err)
	}
	if !changed {
		return nil
	}

	return jf.PostConfig(ctx, "/System/Configuration/encoding", merged)
}

func reconcileJellyfinServer(ctx context.Context, jf *jellyfinclient.Client, server *mediaserversv1alpha1.JellyfinServer) error {
	current, err := jf.GetConfig(ctx, "/System/Configuration")
	if err != nil {
		return fmt.Errorf("getting current server config: %w", err)
	}

	merged, changed, err := reconciler.MergeDesiredOverCurrent(current, server)
	if err != nil {
		return fmt.Errorf("merging server config: %w", err)
	}
	if !changed {
		return nil
	}

	return jf.PostConfig(ctx, "/System/Configuration", merged)
}

func reconcileJellyfinLibrary(ctx context.Context, jf *jellyfinclient.Client, lib mediaserversv1alpha1.JellyfinLibrary) error {
	existing, err := jf.ListLibraries(ctx)
	if err != nil {
		return fmt.Errorf("listing libraries: %w", err)
	}

	for _, e := range existing {
		if name, ok := e["Name"].(string); ok && name == lib.Name {
			return nil // already exists, create-only
		}
	}

	libraryOptions := map[string]interface{}{
		"PathInfos": buildPathInfos(lib.Paths),
	}
	if lib.EnableRealtimeMonitor != nil {
		libraryOptions["EnableRealtimeMonitor"] = *lib.EnableRealtimeMonitor
	}
	if lib.EnableTrickplayImageExtraction != nil {
		libraryOptions["EnableTrickplayImageExtraction"] = *lib.EnableTrickplayImageExtraction
	}
	if lib.AutomaticRefreshIntervalDays != nil {
		libraryOptions["AutomaticRefreshIntervalDays"] = *lib.AutomaticRefreshIntervalDays
	}
	if lib.PreferredMetadataLanguage != "" {
		libraryOptions["PreferredMetadataLanguage"] = lib.PreferredMetadataLanguage
	}
	if lib.MetadataCountryCode != "" {
		libraryOptions["MetadataCountryCode"] = lib.MetadataCountryCode
	}

	return jf.CreateLibrary(ctx, lib.Name, lib.CollectionType, libraryOptions)
}

func buildPathInfos(paths []string) []map[string]interface{} {
	infos := make([]map[string]interface{}, len(paths))
	for i, p := range paths {
		infos[i] = map[string]interface{}{"Path": p}
	}
	return infos
}

// SetupWithManager sets up the controller with the Manager.
func (r *JellyfinConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mediaserversv1alpha1.JellyfinConfig{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			return ctrlcommon.FindConfigsBySecret(ctx, r.Client, obj, &mediaserversv1alpha1.JellyfinConfigList{}, func(list *mediaserversv1alpha1.JellyfinConfigList) []reconcile.Request {
				var reqs []reconcile.Request
				for _, c := range list.Items {
					if c.Spec.AdminUser.UsernameSecretRef.Name == obj.GetName() ||
						c.Spec.AdminUser.PasswordSecretRef.Name == obj.GetName() {
						reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&c)})
					}
				}
				return reqs
			})
		})).
		Named("jellyfinconfig").
		Complete(r)
}
