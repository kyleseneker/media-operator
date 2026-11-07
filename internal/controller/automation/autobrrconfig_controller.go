package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	automationv1alpha1 "github.com/kyleseneker/media-operator/api/automation/v1alpha1"
	autobrrclient "github.com/kyleseneker/media-operator/internal/client/autobrr"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/metrics"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

// AutobrrConfigReconciler reconciles an AutobrrConfig object.
type AutobrrConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=autobrrconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=autobrrconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=media-operator.dev,resources=autobrrconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *AutobrrConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var config automationv1alpha1.AutobrrConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if done, after := ctrlcommon.HandleLifecycle(ctx, r.Client, r.Recorder, &config, nil); done {
		return ctrl.Result{RequeueAfter: after}, nil
	}

	if ctrlcommon.RejectUnsupportedObserve(ctx, r.Status(), &config) {
		return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
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

	hc, err := engine.NewHTTPClient(config.Spec.Connection.URL, engine.AuthAPIKey, engine.WithAPIKey(apiKey),
		// Autobrr reads only X-API-Token; any other header is answered with 403.
		engine.WithAPIKeyHeader("X-API-Token"), engine.WithTLSConfig(tlsCfg), engine.WithAppLabel("autobrr"))
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

	obj := map[string]any{
		"name":    dc.Name,
		"type":    autobrrType(dc.Type),
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

	// Category, save path and content layout are set on filter actions in
	// Autobrr; a download client has no such fields and drops them.
	if dc.Settings != nil && (dc.Settings.Category != "" || dc.Settings.SavePath != "" ||
		dc.Settings.ContentLayout != "" || dc.Settings.PrioritizeProperRepacks != nil) {
		return fmt.Errorf("settings are not download client fields in Autobrr; set category, savePath and contentLayout on filter actions")
	}

	return upsertAutobrr("download client", dc.Name, existing, obj,
		func() error { return ac.CreateDownloadClient(ctx, obj) },
		func(id int) error { return ac.UpdateDownloadClient(ctx, id, obj) })
}

func reconcileAutobrrIndexer(ctx context.Context, c client.Reader, ac *autobrrclient.Client, namespace string, idx automationv1alpha1.AutobrrIndexer) error {
	existing, err := ac.ListIndexers(ctx)
	if err != nil {
		return fmt.Errorf("listing indexers: %w", err)
	}

	obj := map[string]any{
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

	return upsertAutobrr("indexer", idx.Name, existing, obj,
		func() error { return ac.CreateIndexer(ctx, obj) },
		func(id int) error { return ac.UpdateIndexer(ctx, id, obj) })
}

func reconcileAutobrrIRCNetwork(ctx context.Context, c client.Reader, ac *autobrrclient.Client, namespace string, irc automationv1alpha1.AutobrrIRCNetwork) error {
	existing, err := ac.ListIRCNetworks(ctx)
	if err != nil {
		return fmt.Errorf("listing IRC networks: %w", err)
	}

	obj := map[string]any{
		"name":    irc.Name,
		"enabled": irc.Enable == nil || *irc.Enable,
		"server":  irc.Server,
		"port":    irc.Port,
		"nick":    irc.Nick,
	}
	if irc.TLS != nil {
		obj["tls"] = *irc.TLS
	}
	// Autobrr keeps IRC authentication in a nested object.
	auth := map[string]any{}
	if irc.AuthMechanism != "" {
		auth["mechanism"] = autobrrIRCAuth(irc.AuthMechanism)
	}
	if irc.InviteCommand != "" {
		obj["invite_command"] = irc.InviteCommand
	}

	if irc.AuthAccountSecretRef != nil {
		val, err := reconciler.ResolveSecretKeyRef(ctx, c, namespace, *irc.AuthAccountSecretRef)
		if err != nil {
			return fmt.Errorf("resolving auth account: %w", err)
		}
		auth["account"] = val
	}
	if irc.AuthPasswordSecretRef != nil {
		val, err := reconciler.ResolveSecretKeyRef(ctx, c, namespace, *irc.AuthPasswordSecretRef)
		if err != nil {
			return fmt.Errorf("resolving auth password: %w", err)
		}
		auth["password"] = val
	}
	if len(auth) > 0 {
		obj["auth"] = auth
	}

	// Build channels list
	if len(irc.Channels) > 0 {
		channels := make([]map[string]any, 0, len(irc.Channels))
		for _, ch := range irc.Channels {
			chObj := map[string]any{"name": ch.Name}
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

	return upsertAutobrr("IRC network", irc.Name, existing, obj,
		func() error { return ac.CreateIRCNetwork(ctx, obj) },
		func(id int) error { return ac.UpdateIRCNetwork(ctx, id, obj) })
}

func reconcileAutobrrFeed(ctx context.Context, c client.Reader, ac *autobrrclient.Client, namespace string, feed automationv1alpha1.AutobrrFeed) error {
	existing, err := ac.ListFeeds(ctx)
	if err != nil {
		return fmt.Errorf("listing feeds: %w", err)
	}

	obj := map[string]any{
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

	return upsertAutobrr("feed", feed.Name, existing, obj,
		func() error { return ac.CreateFeed(ctx, obj) },
		func(id int) error { return ac.UpdateFeed(ctx, id, obj) })
}

func reconcileAutobrrFilter(ctx context.Context, ac *autobrrclient.Client, filter automationv1alpha1.AutobrrFilter) error {
	existing, err := ac.ListFilters(ctx)
	if err != nil {
		return fmt.Errorf("listing filters: %w", err)
	}
	downloaders, err := ac.ListDownloadClients(ctx)
	if err != nil {
		return fmt.Errorf("listing download clients: %w", err)
	}
	clientIDs := map[string]int{}
	for _, d := range downloaders {
		if name, _ := d["name"].(string); name != "" {
			if id, ok := d["id"].(float64); ok {
				clientIDs[name] = int(id)
			}
		}
	}

	obj := map[string]any{
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

	// A new filter must carry these lists; Autobrr's columns reject null.
	create := maps.Clone(obj)
	addRequiredFilterLists(create)
	filterID, err := upsertAutobrrFilter(ctx, ac, filter.Name, existing, obj, create)
	if err != nil {
		return err
	}
	if filterID == 0 || len(filter.Actions) == 0 {
		return nil
	}
	return reconcileAutobrrActions(ctx, ac, filterID, filter, clientIDs)
}

// reconcileAutobrrActions creates or updates a filter's actions by name.
func reconcileAutobrrActions(
	ctx context.Context, ac *autobrrclient.Client, filterID int, filter automationv1alpha1.AutobrrFilter, clientIDs map[string]int,
) error {
	existing, err := ac.ListActions(ctx, filterID)
	if err != nil {
		return fmt.Errorf("listing actions of filter %q: %w", filter.Name, err)
	}
	for _, a := range filter.Actions {
		aObj := map[string]any{
			"name":    a.Name,
			"type":    autobrrType(a.Type),
			"enabled": a.Enable == nil || *a.Enable,
		}
		if a.ClientRef != "" {
			// Autobrr links an action to a downloader by its numeric ID.
			id, ok := clientIDs[a.ClientRef]
			if !ok {
				return fmt.Errorf("action %q references download client %q, which does not exist", a.Name, a.ClientRef)
			}
			aObj["client_id"] = id
		}
		for key, value := range map[string]string{
			"category": a.Category, "save_path": a.SavePath, "webhook_host": a.WebhookURL,
			"exec_cmd": a.ExecCommand, "exec_args": a.ExecArgs, "watch_folder": a.WatchFolder,
		} {
			if value != "" {
				aObj[key] = value
			}
		}
		// Actions read back without their filter_id, so it is sent but not compared.
		withFilter := func() map[string]any {
			body := maps.Clone(aObj)
			body["filter_id"] = filterID
			return body
		}
		if err := upsertAutobrr("action", a.Name, existing, aObj,
			func() error { return ac.CreateAction(ctx, withFilter()) },
			func(id int) error { return ac.UpdateAction(ctx, id, withFilter()) }); err != nil {
			return fmt.Errorf("action %q: %w", a.Name, err)
		}
	}
	return nil
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
		For(&automationv1alpha1.AutobrrConfig{}, builder.WithPredicates(ctrlcommon.ConfigChangedPredicate())).
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

// autobrrTypes maps the CRD's client and action names to Autobrr's constants,
// which are all Autobrr accepts ("invalid download client type" otherwise).
var autobrrTypes = map[string]string{
	"qBittorrent": "QBITTORRENT", "Deluge": "DELUGE_V2", "Transmission": "TRANSMISSION",
	"rTorrent": "RTORRENT", "SABnzbd": "SABNZBD", "Sonarr": "SONARR", "Radarr": "RADARR",
	"Lidarr": "LIDARR", "Readarr": "READARR", "Whisparr": "WHISPARR",
	"Webhook": "WEBHOOK", "Exec": "EXEC", "WatchFolder": "WATCH_FOLDER",
}

func autobrrType(name string) string {
	if t, ok := autobrrTypes[name]; ok {
		return t
	}
	return name
}

// upsertAutobrr updates an existing resource only when Autobrr does not already
// hold every declared value; writing on every pass rewrote each one each time.
func upsertAutobrr(kind, name string, existing []map[string]any, obj map[string]any,
	create func() error, update func(id int) error,
) error {
	for _, e := range existing {
		if n, _ := e["name"].(string); n != name {
			continue
		}
		id, ok := e["id"].(float64)
		if !ok {
			return fmt.Errorf("%s %q has no valid id", kind, name)
		}
		obj["id"] = int(id)
		desired, err := jsonValue(obj)
		if err != nil {
			return err
		}
		if reconciler.Covers(e, desired) {
			return nil
		}
		metrics.DriftCorrectedTotal.WithLabelValues("autobrr", kind, name).Inc()
		return update(int(id))
	}
	return create()
}

// upsertAutobrrFilter compares against the full stored filter, since the list
// endpoint returns a summary. An update replaces the whole filter, so it sends
// the stored filter with the declared values over it.
func upsertAutobrrFilter(
	ctx context.Context, ac *autobrrclient.Client, name string, existing []map[string]any, obj, create map[string]any,
) (int, error) {
	for _, e := range existing {
		if n, _ := e["name"].(string); n != name {
			continue
		}
		raw, ok := e["id"].(float64)
		if !ok {
			return 0, fmt.Errorf("filter %q has no valid id", name)
		}
		id := int(raw)
		full, err := ac.GetFilter(ctx, id)
		if err != nil {
			return 0, fmt.Errorf("reading filter %q: %w", name, err)
		}
		desired, err := jsonValue(obj)
		if err != nil {
			return 0, err
		}
		if reconciler.Covers(full, desired) {
			return id, nil
		}
		merged, _, err := reconciler.MergeDesiredOverCurrent(full, obj)
		if err != nil {
			return 0, err
		}
		delete(merged, "actions")
		addRequiredFilterLists(merged)
		metrics.DriftCorrectedTotal.WithLabelValues("autobrr", "filter", name).Inc()
		return id, ac.UpdateFilter(ctx, id, merged)
	}
	created, err := ac.CreateFilter(ctx, create)
	if err != nil {
		return 0, err
	}
	raw, _ := created["id"].(float64)
	return int(raw), nil
}

// addRequiredFilterLists sets the lists Autobrr's filter columns reject as null
// and its reads omit when empty.
func addRequiredFilterLists(filter map[string]any) {
	for _, list := range []string{"codecs", "containers"} {
		if filter[list] == nil {
			filter[list] = []string{}
		}
	}
}

func jsonValue(v any) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	return out, json.Unmarshal(raw, &out)
}

// autobrrIRCAuth maps the CRD's mechanism names to Autobrr's constants.
func autobrrIRCAuth(mechanism string) string {
	switch mechanism {
	case "None":
		return "NONE"
	case "NickServ":
		return "NICKSERV"
	}
	return mechanism
}
