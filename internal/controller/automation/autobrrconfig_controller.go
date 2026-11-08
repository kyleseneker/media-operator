package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

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

	cleanup := func(ctx context.Context) error {
		ac, _, err := r.autobrrClient(ctx, &config)
		if err != nil {
			return err
		}
		return (&autobrrSync{ac: ac, owned: engine.NewOwnership("autobrr", config.Status.ManagedResources)}).cleanup(ctx)
	}
	lifecycle := ctrlcommon.Lifecycle{Remove: cleanup, Prune: true}
	if done, after := ctrlcommon.HandleLifecycle(ctx, r.Client, r.Recorder, &config, lifecycle); done {
		return ctrl.Result{RequeueAfter: after}, nil
	}

	ac, reason, err := r.autobrrClient(ctx, &config)
	if err != nil {
		logger.Error(err, "failed to build the Autobrr client")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Client, &config, reason, err.Error())
		if reason == engine.ReasonInvalidConfig {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Health check
	if err := ac.Ping(ctx); err != nil {
		logger.Error(err, "Autobrr unreachable")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Client, &config, engine.ReasonAppUnreachable, err.Error())
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	sync := &autobrrSync{
		App: "autobrr", Observe: ctrlcommon.ObserveOnly(config.Spec.Reconcile),
		c: r.Client, ac: ac, namespace: config.Namespace,
		prune: ctrlcommon.PruneEnabled(config.Spec.Reconcile), owned: engine.NewOwnership("autobrr", config.Status.ManagedResources),
	}
	syncErrors := sync.run(ctx, &config.Spec)
	ctrlcommon.UpdateStatusManaged(&config, sync.owned.Managed())
	ctrlcommon.EmitPruneEvents(r.Recorder, &config, sync.owned.Pruned())
	ctrlcommon.UpdateSyncStatus(ctx, r.Client, &config, sync.Observe, sync.Drift(), syncErrors)

	return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
}

// autobrrClient builds the client for a config, or returns the status reason it
// can't.
func (r *AutobrrConfigReconciler) autobrrClient(ctx context.Context, config *automationv1alpha1.AutobrrConfig) (*autobrrclient.Client, string, error) {
	apiKey, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, config.Spec.Connection.APIKeySecretRef)
	if err != nil {
		return nil, engine.ReasonSecretNotFound, err
	}
	tlsCfg, err := engine.ResolveTLSConfig(ctx, r.Client, config.Namespace, config.Spec.Connection.TLS)
	if err != nil {
		return nil, engine.ReasonSecretNotFound, err
	}
	hc, err := engine.NewHTTPClient(config.Spec.Connection.URL, engine.AuthAPIKey, engine.WithAPIKey(apiKey),
		// Autobrr reads only X-API-Token; any other header is answered with 403.
		engine.WithAPIKeyHeader("X-API-Token"), engine.WithTLSConfig(tlsCfg), engine.WithAppLabel("autobrr"))
	if err != nil {
		return nil, engine.ReasonInvalidConfig, err
	}
	return autobrrclient.NewClient(hc), "", nil
}

// autobrrSync applies one AutobrrConfig. With observe set it only reads, and
// records what it would have written.
type autobrrSync struct {
	engine.Writes
	c         client.Reader
	ac        *autobrrclient.Client
	namespace string
	prune     bool
	owned     *engine.Ownership

	definitions []map[string]any
}

// created records an item once create succeeds.
func (s *autobrrSync) created(kind, name string, create func() error) func() error {
	return func() error {
		if err := create(); err != nil {
			return err
		}
		s.owned.Created(kind, name)
		return nil
	}
}

// run applies the sections in dependency order: filter actions refer to
// download clients by name.
func (s *autobrrSync) run(ctx context.Context, spec *automationv1alpha1.AutobrrConfigSpec) []string {
	logger := log.FromContext(ctx)
	var syncErrors []string
	failed := map[string]bool{}
	fail := func(section, name string, err error) {
		logger.Error(err, "failed to reconcile", "section", section, "name", name)
		syncErrors = append(syncErrors, fmt.Sprintf("%s(%s): %v", section, name, err))
		failed[section+"s"] = true
	}
	for _, dc := range spec.DownloadClients {
		if err := s.downloadClient(ctx, dc); err != nil {
			fail("downloadClient", dc.Name, err)
		}
	}
	for _, idx := range spec.Indexers {
		if err := s.indexer(ctx, idx); err != nil {
			fail("indexer", idx.Name, err)
		}
	}
	for _, irc := range spec.IRCNetworks {
		if err := s.ircNetwork(ctx, irc); err != nil {
			fail("ircNetwork", irc.Name, err)
		}
	}
	for _, feed := range spec.Feeds {
		if err := s.feed(ctx, feed); err != nil {
			fail("feed", feed.Name, err)
		}
	}
	for _, filter := range spec.Filters {
		if err := s.filter(ctx, filter); err != nil {
			fail("filter", filter.Name, err)
		}
	}
	if s.prune {
		syncErrors = append(syncErrors, s.pruneUndeclared(ctx, spec, failed)...)
	}
	return syncErrors
}

func (s *autobrrSync) downloadClient(ctx context.Context, dc automationv1alpha1.AutobrrDownloadClient) error {
	existing, err := s.ac.ListDownloadClients(ctx)
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
		val, err := reconciler.ResolveSecretKeyRef(ctx, s.c, s.namespace, *dc.UsernameSecretRef)
		if err != nil {
			return fmt.Errorf("resolving username: %w", err)
		}
		obj["username"] = val
	}
	if dc.PasswordSecretRef != nil {
		val, err := reconciler.ResolveSecretKeyRef(ctx, s.c, s.namespace, *dc.PasswordSecretRef)
		if err != nil {
			return fmt.Errorf("resolving password: %w", err)
		}
		obj["password"] = val
	}
	if dc.APIKeySecretRef != nil {
		val, err := reconciler.ResolveSecretKeyRef(ctx, s.c, s.namespace, *dc.APIKeySecretRef)
		if err != nil {
			return fmt.Errorf("resolving API key: %w", err)
		}
		obj["settings"] = map[string]any{"apikey": val}
	}

	// Category, save path and content layout are set on filter actions in
	// Autobrr; a download client has no such fields and drops them.
	if dc.Settings != nil && (dc.Settings.Category != "" || dc.Settings.SavePath != "" ||
		dc.Settings.ContentLayout != "" || dc.Settings.PrioritizeProperRepacks != nil) {
		return fmt.Errorf("settings are not download client fields in Autobrr; set category, savePath and contentLayout on filter actions")
	}

	return s.upsert("download client", dc.Name, existing, obj,
		s.created("downloadClients", dc.Name, func() error { return s.ac.CreateDownloadClient(ctx, obj) }),
		func(id int, current map[string]any) error {
			body, err := mergedOver(current, obj)
			if err != nil {
				return err
			}
			return s.ac.UpdateDownloadClient(ctx, id, body)
		})
}

func (s *autobrrSync) indexer(ctx context.Context, idx automationv1alpha1.AutobrrIndexer) error {
	existing, err := s.ac.ListIndexers(ctx)
	if err != nil {
		return fmt.Errorf("listing indexers: %w", err)
	}
	def, err := s.definition(ctx, idx.Identifier)
	if err != nil {
		return err
	}
	implementation, _ := def["implementation"].(string)
	if implementation == "" {
		implementation = autobrrclient.ImplementationIRC
	}
	settings, err := s.indexerSettings(ctx, idx)
	if err != nil {
		return err
	}

	obj := map[string]any{
		"name":           idx.Name,
		"enabled":        idx.Enable == nil || *idx.Enable,
		"implementation": implementation,
	}
	if len(settings) > 0 {
		obj["settings"] = settings
	}
	// Autobrr names a feed indexer itself, as <implementation>-<name>, so only an
	// IRC indexer's identifier can be compared.
	if implementation == autobrrclient.ImplementationIRC {
		obj["identifier"] = idx.Identifier
		obj["base_url"] = idx.BaseURL
		if idx.BaseURL == "" {
			urls, _ := def["urls"].([]any)
			if len(urls) > 0 {
				obj["base_url"] = urls[0]
			}
		}
	}

	for _, e := range existing {
		if e["name"] != idx.Name {
			continue
		}
		id, ok := e["id"].(float64)
		if !ok {
			return fmt.Errorf("indexer %q has no valid id", idx.Name)
		}
		current, err := s.ac.GetIndexer(ctx, int(id))
		if err != nil {
			return fmt.Errorf("reading indexer %q: %w", idx.Name, err)
		}
		desired, err := jsonValue(obj)
		if err != nil {
			return err
		}
		if autobrrCovers(current, desired) {
			return nil
		}
		// An update must carry every stored secret setting, redacted or not.
		body, err := mergedOver(current, obj)
		if err != nil {
			return err
		}
		return s.Apply("indexer", idx.Name, func() error { return s.ac.UpdateIndexer(ctx, int(id), body) })
	}
	return s.Apply("indexer", idx.Name, s.created("indexers", idx.Name, func() error {
		body := maps.Clone(obj)
		body["identifier"] = idx.Identifier
		return s.ac.CreateIndexer(ctx, body)
	}))
}

// definition returns the Autobrr indexer definition with the given identifier.
func (s *autobrrSync) definition(ctx context.Context, identifier string) (map[string]any, error) {
	if s.definitions == nil {
		defs, err := s.ac.IndexerDefinitions(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing indexer definitions: %w", err)
		}
		s.definitions = defs
	}
	for _, d := range s.definitions {
		if d["identifier"] == identifier {
			return d, nil
		}
	}
	return nil, fmt.Errorf("autobrr has no indexer definition %q", identifier)
}

func (s *autobrrSync) indexerSettings(ctx context.Context, idx automationv1alpha1.AutobrrIndexer) (map[string]any, error) {
	fields, err := ctrlcommon.ResolveConfigFields(ctx, s.c, s.namespace, "indexer "+idx.Name, idx.Settings)
	if err != nil {
		return nil, err
	}
	settings := make(map[string]any, len(fields))
	for _, f := range fields {
		if f.Value == nil {
			continue
		}
		value := f.Value.ToInterface()
		if text, ok := value.(string); ok {
			settings[f.Name] = text
		} else {
			settings[f.Name] = fmt.Sprint(value)
		}
	}
	return settings, nil
}

func (s *autobrrSync) ircNetwork(ctx context.Context, irc automationv1alpha1.AutobrrIRCNetwork) error {
	existing, err := s.ac.ListIRCNetworks(ctx)
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
		val, err := reconciler.ResolveSecretKeyRef(ctx, s.c, s.namespace, *irc.AuthAccountSecretRef)
		if err != nil {
			return fmt.Errorf("resolving auth account: %w", err)
		}
		auth["account"] = val
	}
	if irc.AuthPasswordSecretRef != nil {
		val, err := reconciler.ResolveSecretKeyRef(ctx, s.c, s.namespace, *irc.AuthPasswordSecretRef)
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
				val, err := reconciler.ResolveSecretKeyRef(ctx, s.c, s.namespace, *ch.PasswordSecretRef)
				if err != nil {
					return fmt.Errorf("resolving channel %q password: %w", ch.Name, err)
				}
				chObj["password"] = val
			}
			channels = append(channels, chObj)
		}
		obj["channels"] = channels
	}

	return s.upsert("IRC network", irc.Name, existing, obj,
		s.created("ircNetworks", irc.Name, func() error { return s.ac.CreateIRCNetwork(ctx, obj) }),
		func(id int, _ map[string]any) error { return s.ac.UpdateIRCNetwork(ctx, id, obj) })
}

func (s *autobrrSync) feed(ctx context.Context, feed automationv1alpha1.AutobrrFeed) error {
	existing, err := s.ac.ListFeeds(ctx)
	if err != nil {
		return fmt.Errorf("listing feeds: %w", err)
	}
	indexerIDs, err := s.indexerIDs(ctx)
	if err != nil {
		return err
	}
	indexerID, ok := indexerIDs[feed.IndexerRef]
	switch {
	case !ok && s.Observe && len(s.Drift()) > 0:
		s.RecordDrift("feed", feed.Name)
		return nil
	case !ok:
		return fmt.Errorf("references indexer %q, which does not exist", feed.IndexerRef)
	}

	obj := map[string]any{
		"name":    feed.Name,
		"enabled": feed.Enable == nil || *feed.Enable,
		"type":    strings.ToUpper(feed.Type),
		"url":     feed.URL,
		"indexer": map[string]any{"id": indexerID},
	}
	if feed.Interval != nil {
		obj["interval"] = *feed.Interval
	}
	if feed.APIKeySecretRef != nil {
		val, err := reconciler.ResolveSecretKeyRef(ctx, s.c, s.namespace, *feed.APIKeySecretRef)
		if err != nil {
			return fmt.Errorf("resolving API key: %w", err)
		}
		obj["api_key"] = val
	}
	// Autobrr links a feed to its indexer by indexer_id and reads it back as an
	// indexer object.
	withIndexer := func(body map[string]any) map[string]any {
		delete(body, "indexer")
		body["indexer_id"] = indexerID
		return body
	}

	return s.upsert("feed", feed.Name, existing, obj,
		s.created("feeds", feed.Name, func() error {
			body := withIndexer(maps.Clone(obj))
			body["timeout"] = defaultFeedTimeout
			return s.ac.CreateFeed(ctx, body)
		}),
		func(id int, current map[string]any) error {
			body, err := mergedOver(current, obj)
			if err != nil {
				return err
			}
			return s.ac.UpdateFeed(ctx, id, withIndexer(body))
		})
}

// indexerIDs maps each Autobrr indexer's name to its id.
func (s *autobrrSync) indexerIDs(ctx context.Context) (map[string]int, error) {
	indexers, err := s.ac.ListIndexers(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing indexers: %w", err)
	}
	ids := map[string]int{}
	for _, i := range indexers {
		name, _ := i["name"].(string)
		if id, ok := i["id"].(float64); ok && name != "" {
			ids[name] = int(id)
		}
	}
	return ids, nil
}

func (s *autobrrSync) filter(ctx context.Context, filter automationv1alpha1.AutobrrFilter) error {
	existing, err := s.ac.ListFilters(ctx)
	if err != nil {
		return fmt.Errorf("listing filters: %w", err)
	}
	downloaders, err := s.ac.ListDownloadClients(ctx)
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

	obj, err := autobrrFields(filter, autobrrclient.FilterFields)
	if err != nil {
		return err
	}
	obj["name"] = filter.Name
	obj["enabled"] = filter.Enable == nil || *filter.Enable
	// Autobrr keeps tags as one comma-separated string.
	if len(filter.Tags) > 0 {
		obj["tags"] = strings.Join(filter.Tags, ",")
	}
	if len(filter.ExceptTags) > 0 {
		obj["except_tags"] = strings.Join(filter.ExceptTags, ",")
	}
	if len(filter.Indexers) > 0 {
		indexers, missing, err := s.filterIndexers(ctx, filter.Indexers)
		switch {
		case err != nil:
			return err
		case missing != "" && s.Observe && len(s.Drift()) > 0:
			s.RecordDrift("filter", filter.Name)
			return nil
		case missing != "":
			return fmt.Errorf("references indexer %q, which does not exist", missing)
		}
		obj["indexers"] = indexers
	}

	// A new filter must carry these lists; Autobrr's columns reject null.
	create := maps.Clone(obj)
	addRequiredFilterLists(create)
	filterID, err := s.upsertFilter(ctx, filter.Name, existing, obj, create)
	if err != nil {
		return err
	}
	if filterID == 0 || len(filter.Actions) == 0 {
		return nil
	}
	return s.actions(ctx, filterID, filter, clientIDs)
}

// filterIndexers resolves indexer names to the objects Autobrr links a filter
// to, ordered by id as Autobrr returns them. It returns the first name with no
// indexer.
func (s *autobrrSync) filterIndexers(ctx context.Context, names []string) ([]any, string, error) {
	ids, err := s.indexerIDs(ctx)
	if err != nil {
		return nil, "", err
	}
	resolved := make([]int, 0, len(names))
	for _, name := range names {
		id, ok := ids[name]
		if !ok {
			return nil, name, nil
		}
		resolved = append(resolved, id)
	}
	slices.Sort(resolved)
	out := make([]any, 0, len(resolved))
	for _, id := range resolved {
		out = append(out, map[string]any{"id": id})
	}
	return out, "", nil
}

// actions creates or updates a filter's actions by name.
func (s *autobrrSync) actions(
	ctx context.Context, filterID int, filter automationv1alpha1.AutobrrFilter, clientIDs map[string]int,
) error {
	existing, err := s.ac.ListActions(ctx, filterID)
	if err != nil {
		return fmt.Errorf("listing actions of filter %q: %w", filter.Name, err)
	}
	for _, a := range filter.Actions {
		aObj, err := autobrrFields(a, autobrrclient.ActionFields)
		if err != nil {
			return fmt.Errorf("action %q: %w", a.Name, err)
		}
		aObj["name"] = a.Name
		aObj["type"] = autobrrType(a.Type)
		aObj["enabled"] = a.Enable == nil || *a.Enable
		if a.ClientRef != "" {
			// Autobrr links an action to a downloader by its numeric ID.
			id, ok := clientIDs[a.ClientRef]
			switch {
			case !ok && s.Observe && len(s.Drift()) > 0:
				// The download client may be one observe mode found missing.
				s.RecordDrift("action", a.Name)
				continue
			case !ok:
				return fmt.Errorf("action %q references download client %q, which does not exist", a.Name, a.ClientRef)
			}
			aObj["client_id"] = id
		}
		// Actions read back without their filter_id, so it is sent but not compared.
		withFilter := func() map[string]any {
			body := maps.Clone(aObj)
			body["filter_id"] = filterID
			return body
		}
		if err := s.upsert("action", a.Name, existing, aObj,
			s.created(actionsKind, filter.Name+"/"+a.Name, func() error { return s.ac.CreateAction(ctx, withFilter()) }),
			func(id int, _ map[string]any) error { return s.ac.UpdateAction(ctx, id, withFilter()) }); err != nil {
			return fmt.Errorf("action %q: %w", a.Name, err)
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AutobrrConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&automationv1alpha1.AutobrrConfig{}, builder.WithPredicates(ctrlcommon.ConfigChangedPredicate())).
		Watches(&corev1.Secret{}, ctrlcommon.EnqueueForSecret(r.Client, &automationv1alpha1.AutobrrConfigList{})).
		Named("autobrrconfig").
		Complete(r)
}

// autobrrFields renders the spec fields a table names under Autobrr's keys,
// leaving out the ones the spec leaves unset.
func autobrrFields(spec any, table map[string]string) (map[string]any, error) {
	raw, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	out := map[string]any{}
	for name, key := range table {
		if v, ok := fields[name]; ok {
			out[key] = v
		}
	}
	return out, nil
}

func autobrrType(name string) string {
	if t, ok := autobrrclient.Types[name]; ok {
		return t
	}
	return name
}

// upsert updates an existing resource only when Autobrr does not already hold
// every declared value; writing on every pass rewrote each one each time.
func (s *autobrrSync) upsert(kind, name string, existing []map[string]any, obj map[string]any,
	create func() error, update func(id int, current map[string]any) error,
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
		if autobrrCovers(e, desired) {
			return nil
		}
		return s.Apply(kind, name, func() error { return update(int(id), e) })
	}
	return s.Apply(kind, name, create)
}

// autobrrCovers compares as reconciler.Covers does, except that a declared
// false, 0 or empty value matches a key Autobrr left out: it omits zero values
// from what it returns.
func autobrrCovers(current map[string]any, desired any) bool {
	fields, ok := desired.(map[string]any)
	if !ok {
		return reconciler.Covers(current, desired)
	}
	filled := maps.Clone(current)
	for key, value := range fields {
		if _, present := filled[key]; present {
			continue
		}
		switch v := value.(type) {
		case bool:
			if !v {
				filled[key] = v
			}
		case float64:
			if v == 0 {
				filled[key] = v
			}
		case string:
			if v == "" {
				filled[key] = v
			}
		case []any:
			if len(v) == 0 {
				filled[key] = v
			}
		}
	}
	return reconciler.Covers(filled, desired)
}

// upsertFilter compares against the full stored filter, since the list
// endpoint returns a summary. An update replaces the whole filter, so it sends
// the stored filter with the declared values over it. It returns the filter's
// id, or 0 when observe mode found it missing.
func (s *autobrrSync) upsertFilter(
	ctx context.Context, name string, existing []map[string]any, obj, create map[string]any,
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
		full, err := s.ac.GetFilter(ctx, id)
		if err != nil {
			return 0, fmt.Errorf("reading filter %q: %w", name, err)
		}
		desired, err := jsonValue(obj)
		if err != nil {
			return 0, err
		}
		sortByID(full["indexers"])
		if autobrrCovers(full, desired) {
			return id, nil
		}
		merged, _, err := reconciler.MergeDesiredOverCurrent(full, obj)
		if err != nil {
			return 0, err
		}
		delete(merged, "actions")
		addRequiredFilterLists(merged)
		return id, s.Apply("filter", name, func() error { return s.ac.UpdateFilter(ctx, id, merged) })
	}
	var id int
	err := s.Apply("filter", name, func() error {
		created, err := s.ac.CreateFilter(ctx, create)
		if err != nil {
			return err
		}
		raw, _ := created["id"].(float64)
		id = int(raw)
		s.owned.Created("filters", name)
		// Autobrr links a filter to its indexers only on update.
		if _, ok := obj["indexers"]; !ok {
			return nil
		}
		full, err := s.ac.GetFilter(ctx, id)
		if err != nil {
			return fmt.Errorf("reading the new filter: %w", err)
		}
		linked, err := mergedOver(full, obj)
		if err != nil {
			return err
		}
		delete(linked, "actions")
		addRequiredFilterLists(linked)
		return s.ac.UpdateFilter(ctx, id, linked)
	})
	return id, err
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

func autobrrIRCAuth(mechanism string) string {
	if m, ok := autobrrclient.IRCAuthMechanisms[mechanism]; ok {
		return m
	}
	return mechanism
}

// defaultFeedTimeout is the request timeout, in seconds, Autobrr's web UI gives a new feed.
const defaultFeedTimeout = 60

// mergedOver returns the stored object with the declared values over it, so an
// update keeps what the resource doesn't manage. Autobrr keeps a stored secret
// sent back redacted.
func mergedOver(current, obj map[string]any) (map[string]any, error) {
	merged, _, err := reconciler.MergeDesiredOverCurrent(current, obj)
	return merged, err
}

func sortByID(list any) {
	items, _ := list.([]any)
	slices.SortFunc(items, func(a, b any) int {
		ai, _ := a.(map[string]any)["id"].(float64)
		bi, _ := b.(map[string]any)["id"].(float64)
		return int(ai - bi)
	})
}
