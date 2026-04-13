package automation

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

	automationv1alpha1 "github.com/kyleseneker/media-operator/api/automation/v1alpha1"
	autobrrclient "github.com/kyleseneker/media-operator/internal/client/autobrr"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

// AutobrrConfigReconciler reconciles an AutobrrConfig object.
type AutobrrConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=autobrrconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=autobrrconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *AutobrrConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var config automationv1alpha1.AutobrrConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Resolve API key
	apiKey, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, config.Spec.Connection.APIKeySecretRef)
	if err != nil {
		logger.Error(err, "failed to resolve API key secret")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	tlsCfg, err := engine.ResolveTLSConfig(ctx, r.Client, config.Namespace, config.Spec.Connection.TLS)
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	hc, err := engine.NewHTTPClient(config.Spec.Connection.URL, engine.AuthAPIKey, engine.WithAPIKey(apiKey), engine.WithTLSConfig(tlsCfg), engine.WithAppLabel("autobrr"))
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonInvalidConfig, err.Error())
		return ctrl.Result{}, nil
	}
	ac := autobrrclient.NewClient(hc)

	// Health check
	if err := ac.Ping(ctx); err != nil {
		logger.Error(err, "Autobrr unreachable")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonAppUnreachable, err.Error())
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	var syncErrors []string

	// Reconcile download clients
	for _, dc := range config.Spec.DownloadClients {
		if err := reconcileAutobrrDownloadClient(ctx, r.Client, ac, config.Namespace, dc); err != nil {
			logger.Error(err, "failed to reconcile download client", "name", dc.Name)
			syncErrors = append(syncErrors, fmt.Sprintf("downloadClient(%s): %v", dc.Name, err))
		}
	}

	// Reconcile indexers
	for _, idx := range config.Spec.Indexers {
		if err := reconcileAutobrrIndexer(ctx, r.Client, ac, config.Namespace, idx); err != nil {
			logger.Error(err, "failed to reconcile indexer", "name", idx.Name)
			syncErrors = append(syncErrors, fmt.Sprintf("indexer(%s): %v", idx.Name, err))
		}
	}

	// Reconcile IRC networks
	for _, irc := range config.Spec.IRCNetworks {
		if err := reconcileAutobrrIRCNetwork(ctx, r.Client, ac, config.Namespace, irc); err != nil {
			logger.Error(err, "failed to reconcile IRC network", "name", irc.Name)
			syncErrors = append(syncErrors, fmt.Sprintf("ircNetwork(%s): %v", irc.Name, err))
		}
	}

	// Reconcile feeds
	for _, feed := range config.Spec.Feeds {
		if err := reconcileAutobrrFeed(ctx, r.Client, ac, config.Namespace, feed); err != nil {
			logger.Error(err, "failed to reconcile feed", "name", feed.Name)
			syncErrors = append(syncErrors, fmt.Sprintf("feed(%s): %v", feed.Name, err))
		}
	}

	// Reconcile filters
	for _, filter := range config.Spec.Filters {
		if err := reconcileAutobrrFilter(ctx, ac, filter); err != nil {
			logger.Error(err, "failed to reconcile filter", "name", filter.Name)
			syncErrors = append(syncErrors, fmt.Sprintf("filter(%s): %v", filter.Name, err))
		}
	}

	if len(syncErrors) > 0 {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, false, engine.ReasonSyncFailed, fmt.Sprintf("partial sync failure: %v", syncErrors))
	} else {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, true, engine.ReasonSynced, "all configuration sections synced")
	}

	return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
}

func reconcileAutobrrDownloadClient(ctx context.Context, c client.Reader, ac *autobrrclient.Client, namespace string, dc automationv1alpha1.AutobrrDownloadClient) error {
	existing, err := ac.ListDownloadClients(ctx)
	if err != nil {
		return fmt.Errorf("listing download clients: %w", err)
	}

	obj := map[string]interface{}{
		"name":    dc.Name,
		"type":    dc.Type,
		"host":    dc.Host,
		"enabled": dc.Enable == nil || *dc.Enable,
	}
	if dc.Port != nil {
		obj["port"] = *dc.Port
	}
	if dc.TLS != nil {
		obj["tls"] = *dc.TLS
	}

	// Resolve secrets
	if dc.UsernameSecretRef != nil {
		val, err := reconciler.ResolveSecretKeyRef(ctx, c, namespace, *dc.UsernameSecretRef)
		if err != nil {
			return fmt.Errorf("resolving username: %w", err)
		}
		obj["username"] = val
	}
	if dc.PasswordSecretRef != nil {
		val, err := reconciler.ResolveSecretKeyRef(ctx, c, namespace, *dc.PasswordSecretRef)
		if err != nil {
			return fmt.Errorf("resolving password: %w", err)
		}
		obj["password"] = val
	}
	if dc.APIKeySecretRef != nil {
		val, err := reconciler.ResolveSecretKeyRef(ctx, c, namespace, *dc.APIKeySecretRef)
		if err != nil {
			return fmt.Errorf("resolving API key: %w", err)
		}
		obj["api_key"] = val
	}

	if dc.Settings != nil {
		settings := make(map[string]interface{})
		if dc.Settings.Category != "" {
			settings["category"] = dc.Settings.Category
		}
		if dc.Settings.SavePath != "" {
			settings["savePath"] = dc.Settings.SavePath
		}
		if dc.Settings.ContentLayout != "" {
			settings["contentLayout"] = dc.Settings.ContentLayout
		}
		if dc.Settings.PrioritizeProperRepacks != nil {
			settings["prioritizeProperRepacks"] = *dc.Settings.PrioritizeProperRepacks
		}
		if len(settings) > 0 {
			obj["settings"] = settings
		}
	}

	// Find existing by name
	for _, e := range existing {
		if name, _ := e["name"].(string); name == dc.Name {
			id, ok := e["id"].(float64)
			if !ok {
				return fmt.Errorf("download client %q has no valid id", dc.Name)
			}
			obj["id"] = int(id)
			return ac.UpdateDownloadClient(ctx, int(id), obj)
		}
	}

	return ac.CreateDownloadClient(ctx, obj)
}

func reconcileAutobrrIndexer(ctx context.Context, c client.Reader, ac *autobrrclient.Client, namespace string, idx automationv1alpha1.AutobrrIndexer) error {
	existing, err := ac.ListIndexers(ctx)
	if err != nil {
		return fmt.Errorf("listing indexers: %w", err)
	}

	obj := map[string]interface{}{
		"name":           idx.Name,
		"enabled":        idx.Enable == nil || *idx.Enable,
		"implementation": idx.Implementation,
	}
	if idx.BaseURL != "" {
		obj["base_url"] = idx.BaseURL
	}
	if idx.FeedURL != "" {
		obj["feed_url"] = idx.FeedURL
	}
	if idx.APIKeySecretRef != nil {
		val, err := reconciler.ResolveSecretKeyRef(ctx, c, namespace, *idx.APIKeySecretRef)
		if err != nil {
			return fmt.Errorf("resolving API key: %w", err)
		}
		obj["api_key"] = val
	}

	for _, e := range existing {
		if name, _ := e["name"].(string); name == idx.Name {
			id, ok := e["id"].(float64)
			if !ok {
				return fmt.Errorf("indexer %q has no valid id", idx.Name)
			}
			obj["id"] = int(id)
			return ac.UpdateIndexer(ctx, int(id), obj)
		}
	}

	return ac.CreateIndexer(ctx, obj)
}

func reconcileAutobrrIRCNetwork(ctx context.Context, c client.Reader, ac *autobrrclient.Client, namespace string, irc automationv1alpha1.AutobrrIRCNetwork) error {
	existing, err := ac.ListIRCNetworks(ctx)
	if err != nil {
		return fmt.Errorf("listing IRC networks: %w", err)
	}

	obj := map[string]interface{}{
		"name":    irc.Name,
		"enabled": irc.Enable == nil || *irc.Enable,
		"server":  irc.Server,
		"port":    irc.Port,
		"nick":    irc.Nick,
	}
	if irc.TLS != nil {
		obj["tls"] = *irc.TLS
	}
	if irc.AuthMechanism != "" {
		obj["auth_mechanism"] = irc.AuthMechanism
	}
	if irc.InviteCommand != "" {
		obj["invite_command"] = irc.InviteCommand
	}

	if irc.AuthAccountSecretRef != nil {
		val, err := reconciler.ResolveSecretKeyRef(ctx, c, namespace, *irc.AuthAccountSecretRef)
		if err != nil {
			return fmt.Errorf("resolving auth account: %w", err)
		}
		obj["auth_account"] = val
	}
	if irc.AuthPasswordSecretRef != nil {
		val, err := reconciler.ResolveSecretKeyRef(ctx, c, namespace, *irc.AuthPasswordSecretRef)
		if err != nil {
			return fmt.Errorf("resolving auth password: %w", err)
		}
		obj["auth_password"] = val
	}

	// Build channels list
	if len(irc.Channels) > 0 {
		channels := make([]map[string]interface{}, 0, len(irc.Channels))
		for _, ch := range irc.Channels {
			chObj := map[string]interface{}{"name": ch.Name}
			if ch.PasswordSecretRef != nil {
				val, err := reconciler.ResolveSecretKeyRef(ctx, c, namespace, *ch.PasswordSecretRef)
				if err != nil {
					return fmt.Errorf("resolving channel %q password: %w", ch.Name, err)
				}
				chObj["password"] = val
			}
			channels = append(channels, chObj)
		}
		obj["channels"] = channels
	}

	for _, e := range existing {
		if name, _ := e["name"].(string); name == irc.Name {
			id, ok := e["id"].(float64)
			if !ok {
				return fmt.Errorf("IRC network %q has no valid id", irc.Name)
			}
			obj["id"] = int(id)
			return ac.UpdateIRCNetwork(ctx, int(id), obj)
		}
	}

	return ac.CreateIRCNetwork(ctx, obj)
}

func reconcileAutobrrFeed(ctx context.Context, c client.Reader, ac *autobrrclient.Client, namespace string, feed automationv1alpha1.AutobrrFeed) error {
	existing, err := ac.ListFeeds(ctx)
	if err != nil {
		return fmt.Errorf("listing feeds: %w", err)
	}

	obj := map[string]interface{}{
		"name":    feed.Name,
		"enabled": feed.Enable == nil || *feed.Enable,
		"type":    feed.Type,
		"url":     feed.URL,
	}
	if feed.Interval != nil {
		obj["interval"] = *feed.Interval
	}
	if feed.IndexerRef != "" {
		obj["indexer"] = feed.IndexerRef
	}
	if feed.APIKeySecretRef != nil {
		val, err := reconciler.ResolveSecretKeyRef(ctx, c, namespace, *feed.APIKeySecretRef)
		if err != nil {
			return fmt.Errorf("resolving API key: %w", err)
		}
		obj["api_key"] = val
	}

	for _, e := range existing {
		if name, _ := e["name"].(string); name == feed.Name {
			id, ok := e["id"].(float64)
			if !ok {
				return fmt.Errorf("feed %q has no valid id", feed.Name)
			}
			obj["id"] = int(id)
			return ac.UpdateFeed(ctx, int(id), obj)
		}
	}

	return ac.CreateFeed(ctx, obj)
}

func reconcileAutobrrFilter(ctx context.Context, ac *autobrrclient.Client, filter automationv1alpha1.AutobrrFilter) error {
	existing, err := ac.ListFilters(ctx)
	if err != nil {
		return fmt.Errorf("listing filters: %w", err)
	}

	obj := map[string]interface{}{
		"name":    filter.Name,
		"enabled": filter.Enable == nil || *filter.Enable,
	}
	if filter.Priority != nil {
		obj["priority"] = *filter.Priority
	}
	if filter.MatchReleases != "" {
		obj["match_releases"] = filter.MatchReleases
	}
	if filter.ExceptReleases != "" {
		obj["except_releases"] = filter.ExceptReleases
	}
	if filter.MatchCategories != "" {
		obj["match_categories"] = filter.MatchCategories
	}
	if len(filter.Resolutions) > 0 {
		obj["resolutions"] = filter.Resolutions
	}
	if len(filter.Sources) > 0 {
		obj["sources"] = filter.Sources
	}
	if len(filter.Codecs) > 0 {
		obj["codecs"] = filter.Codecs
	}
	if filter.MinSize != "" {
		obj["min_size"] = filter.MinSize
	}
	if filter.MaxSize != "" {
		obj["max_size"] = filter.MaxSize
	}
	if filter.Delay != nil {
		obj["delay"] = *filter.Delay
	}
	if filter.UseRegex != nil {
		obj["use_regex"] = *filter.UseRegex
	}
	if len(filter.Tags) > 0 {
		obj["tags"] = filter.Tags
	}
	if len(filter.Indexers) > 0 {
		obj["indexers"] = filter.Indexers
	}

	// Build actions
	if len(filter.Actions) > 0 {
		actions := make([]map[string]interface{}, 0, len(filter.Actions))
		for _, a := range filter.Actions {
			aObj := map[string]interface{}{
				"name":    a.Name,
				"type":    a.Type,
				"enabled": a.Enable == nil || *a.Enable,
			}
			if a.ClientRef != "" {
				aObj["client_id"] = a.ClientRef
			}
			if a.Category != "" {
				aObj["category"] = a.Category
			}
			if a.SavePath != "" {
				aObj["save_path"] = a.SavePath
			}
			if a.WebhookURL != "" {
				aObj["webhook_url"] = a.WebhookURL
			}
			if a.ExecCommand != "" {
				aObj["exec_cmd"] = a.ExecCommand
			}
			if a.ExecArgs != "" {
				aObj["exec_args"] = a.ExecArgs
			}
			if a.WatchFolder != "" {
				aObj["watch_folder"] = a.WatchFolder
			}
			actions = append(actions, aObj)
		}
		obj["actions"] = actions
	}

	for _, e := range existing {
		if name, _ := e["name"].(string); name == filter.Name {
			id, ok := e["id"].(float64)
			if !ok {
				return fmt.Errorf("filter %q has no valid id", filter.Name)
			}
			obj["id"] = int(id)
			return ac.UpdateFilter(ctx, int(id), obj)
		}
	}

	_, err = ac.CreateFilter(ctx, obj)
	return err
}

// referencesSecret checks if the config references the given secret name.
func referencesSecret(config *automationv1alpha1.AutobrrConfig, secretName string) bool {
	if config.Spec.Connection.APIKeySecretRef.Name == secretName {
		return true
	}
	for _, dc := range config.Spec.DownloadClients {
		if dc.UsernameSecretRef != nil && dc.UsernameSecretRef.Name == secretName {
			return true
		}
		if dc.PasswordSecretRef != nil && dc.PasswordSecretRef.Name == secretName {
			return true
		}
		if dc.APIKeySecretRef != nil && dc.APIKeySecretRef.Name == secretName {
			return true
		}
	}
	for _, idx := range config.Spec.Indexers {
		if idx.APIKeySecretRef != nil && idx.APIKeySecretRef.Name == secretName {
			return true
		}
	}
	for _, irc := range config.Spec.IRCNetworks {
		if irc.AuthAccountSecretRef != nil && irc.AuthAccountSecretRef.Name == secretName {
			return true
		}
		if irc.AuthPasswordSecretRef != nil && irc.AuthPasswordSecretRef.Name == secretName {
			return true
		}
		for _, ch := range irc.Channels {
			if ch.PasswordSecretRef != nil && ch.PasswordSecretRef.Name == secretName {
				return true
			}
		}
	}
	for _, feed := range config.Spec.Feeds {
		if feed.APIKeySecretRef != nil && feed.APIKeySecretRef.Name == secretName {
			return true
		}
	}
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *AutobrrConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&automationv1alpha1.AutobrrConfig{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			return ctrlcommon.FindConfigsBySecret(ctx, r.Client, obj, &automationv1alpha1.AutobrrConfigList{}, func(list *automationv1alpha1.AutobrrConfigList) []reconcile.Request {
				var reqs []reconcile.Request
				for _, c := range list.Items {
					if referencesSecret(&c, obj.GetName()) {
						reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&c)})
					}
				}
				return reqs
			})
		})).
		Named("autobrrconfig").
		Complete(r)
}
