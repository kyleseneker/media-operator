package mediaservers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

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
	Recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=jellyfinconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=jellyfinconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=media-operator.dev,resources=jellyfinconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *JellyfinConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var config mediaserversv1alpha1.JellyfinConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if done, after := ctrlcommon.HandleLifecycle(ctx, r.Client, r.Recorder, &config, ctrlcommon.Lifecycle{}); done {
		return ctrl.Result{RequeueAfter: after}, nil
	}

	// Resolve admin credentials
	username, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, config.Spec.AdminUser.UsernameSecretRef)
	if err != nil {
		logger.Error(err, "failed to resolve admin username secret")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Client, &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	password, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, config.Spec.AdminUser.PasswordSecretRef)
	if err != nil {
		logger.Error(err, "failed to resolve admin password secret")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Client, &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	tlsCfg, err := engine.ResolveTLSConfig(ctx, r.Client, config.Namespace, config.Spec.Connection.TLS)
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Client, &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	hc, err := engine.NewHTTPClient(config.Spec.Connection.URL, engine.AuthMediaBrowser, engine.WithTLSConfig(tlsCfg), engine.WithAppLabel("jellyfin"))
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Client, &config, engine.ReasonInvalidConfig, err.Error())
		return ctrl.Result{}, nil
	}
	jf := jellyfinclient.NewClient(hc)

	// Check if setup wizard is complete
	setupComplete, err := jf.IsSetupComplete(ctx)
	if err != nil {
		logger.Error(err, "Jellyfin unreachable")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Client, &config, engine.ReasonAppUnreachable, err.Error())
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	if !setupComplete && ctrlcommon.ObserveOnly(config.Spec.Reconcile) {
		config.Status.Initialized = &setupComplete
		observed := &engine.Writes{App: "jellyfin", Observe: true}
		err := observed.Compare("settings", "initialization", nil, map[string]any{"initialized": true})
		ctrlcommon.UpdateObservationStatus(ctx, r.Client, &config, observed.Drift(), err)
		return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
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
			ctrlcommon.UpdateStatus(ctx, r.Client, &config, false, engine.ReasonSyncFailed, fmt.Sprintf("setup wizard: %v", err))
			return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
		}
		logger.Info("Jellyfin setup wizard completed")
	}

	initialized := true
	config.Status.Initialized = &initialized

	// Authenticate
	if err := jf.Authenticate(ctx, username, password); err != nil {
		logger.Error(err, "Jellyfin authentication failed")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Client, &config, engine.ReasonAppUnreachable, fmt.Sprintf("authentication: %v", err))
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	if ctrlcommon.ObserveOnly(config.Spec.Reconcile) {
		observed := &engine.Writes{App: "jellyfin", Observe: true}
		err := observeJellyfinConfig(ctx, jf, config.Spec, observed)
		ctrlcommon.UpdateObservationStatus(ctx, r.Client, &config, observed.Drift(), err)
		return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
	}

	var syncErrors []string

	for _, section := range jellyfinSections(config.Spec) {
		if err := reconcileJellyfinSection(ctx, jf, section.path, section.settings); err != nil {
			logger.Error(err, "failed to reconcile settings", "section", section.name)
			syncErrors = append(syncErrors, fmt.Sprintf("%s: %v", section.name, err))
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
		ctrlcommon.UpdateStatus(ctx, r.Client, &config, false, engine.ReasonSyncFailed, fmt.Sprintf("partial sync failure: %v", syncErrors))
	} else {
		ctrlcommon.UpdateStatus(ctx, r.Client, &config, true, engine.ReasonSynced, "all configuration sections synced")
	}

	return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
}

// jellyfinSettings renders a spec section under Jellyfin's own PascalCase
// setting names. Jellyfin accepts camelCase on write but reads back PascalCase,
// so camelCase keys always looked missing and every pass rewrote the settings.
func jellyfinSettings(section any) (map[string]any, error) {
	raw, err := json.Marshal(section)
	if err != nil {
		return nil, err
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	return pascalKeys(fields), nil
}

func pascalKeys(fields map[string]any) map[string]any {
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		if nested, ok := v.(map[string]any); ok {
			v = pascalKeys(nested)
		}
		out[strings.ToUpper(k[:1])+k[1:]] = v
	}
	return out
}

type jellyfinSection struct {
	name     string
	path     string
	settings any
}

func jellyfinSections(spec mediaserversv1alpha1.JellyfinConfigSpec) []jellyfinSection {
	var sections []jellyfinSection
	if spec.Encoding != nil {
		sections = append(sections, jellyfinSection{"encoding", "/System/Configuration/encoding", spec.Encoding})
	}
	if spec.Server != nil {
		sections = append(sections, jellyfinSection{"server", "/System/Configuration", spec.Server})
	}
	if spec.Network != nil {
		sections = append(sections, jellyfinSection{"network", "/System/Configuration/network", spec.Network})
	}
	return sections
}

func reconcileJellyfinSection(ctx context.Context, jf *jellyfinclient.Client, path string, section any) error {
	current, err := jf.GetConfig(ctx, path)
	if err != nil {
		return fmt.Errorf("getting current config: %w", err)
	}
	desired, err := jellyfinSettings(section)
	if err != nil {
		return err
	}
	merged, changed, err := reconciler.MergeDesiredOverCurrent(current, desired)
	if err != nil {
		return fmt.Errorf("merging config: %w", err)
	}
	if !changed {
		return nil
	}
	return jf.PostConfig(ctx, path, merged)
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

	libraryOptions := map[string]any{
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

func buildPathInfos(paths []string) []map[string]any {
	infos := make([]map[string]any, len(paths))
	for i, p := range paths {
		infos[i] = map[string]any{"Path": p}
	}
	return infos
}

// SetupWithManager sets up the controller with the Manager.
func (r *JellyfinConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mediaserversv1alpha1.JellyfinConfig{}, builder.WithPredicates(ctrlcommon.ConfigChangedPredicate())).
		Watches(&corev1.Secret{}, ctrlcommon.EnqueueForSecret(r.Client, &mediaserversv1alpha1.JellyfinConfigList{})).
		Named("jellyfinconfig").
		Complete(r)
}

func observeJellyfinConfig(ctx context.Context, jf *jellyfinclient.Client, spec mediaserversv1alpha1.JellyfinConfigSpec, w *engine.Writes) error {
	for _, section := range jellyfinSections(spec) {
		desired, err := jellyfinSettings(section.settings)
		if err != nil {
			return err
		}
		current, err := jf.GetConfig(ctx, section.path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", section.path, err)
		}
		if err := w.Compare("settings", section.path, current, desired); err != nil {
			return err
		}
	}
	if len(spec.Libraries) == 0 {
		return nil
	}
	libraries, err := jf.ListLibraries(ctx)
	if err != nil {
		return fmt.Errorf("listing libraries: %w", err)
	}
	for _, library := range spec.Libraries {
		var current map[string]any
		for _, existing := range libraries {
			if existing["Name"] == library.Name {
				current = existing
				break
			}
		}
		// Libraries are create-only, so only existence matters.
		if err := w.Compare("libraries", library.Name, current, map[string]any{"Name": library.Name}); err != nil {
			return err
		}
	}
	return nil
}
