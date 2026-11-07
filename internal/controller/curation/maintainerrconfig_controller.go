package curation

import (
	"context"
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

	curationv1alpha1 "github.com/kyleseneker/media-operator/api/curation/v1alpha1"
	maintainerrclient "github.com/kyleseneker/media-operator/internal/client/maintainerr"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

const (
	maintainerrApp = "maintainerr"
	mediaPlex      = "plex"
	mediaJellyfin  = "jellyfin"
)

// MaintainerrConfigReconciler reconciles a MaintainerrConfig object.
type MaintainerrConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=maintainerrconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=maintainerrconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=media-operator.dev,resources=maintainerrconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *MaintainerrConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var config curationv1alpha1.MaintainerrConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if done, after := ctrlcommon.HandleLifecycle(ctx, r.Client, r.Recorder, &config, ctrlcommon.Lifecycle{}); done {
		return ctrl.Result{RequeueAfter: after}, nil
	}

	tlsCfg, err := engine.ResolveTLSConfig(ctx, r.Client, config.Namespace, config.Spec.Connection.TLS)
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Client, &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Maintainerr has no API authentication.
	hc, err := engine.NewHTTPClient(config.Spec.Connection.URL, engine.AuthNone,
		engine.WithTLSConfig(tlsCfg), engine.WithAppLabel(maintainerrApp))
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Client, &config, engine.ReasonInvalidConfig, err.Error())
		return ctrl.Result{}, nil
	}
	mc := maintainerrclient.NewClient(hc)

	if err := mc.Ping(ctx); err != nil {
		logger.Error(err, "Maintainerr unreachable")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Client, &config, engine.ReasonAppUnreachable, err.Error())
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	sync := &maintainerrSync{
		App: maintainerrApp, Observe: ctrlcommon.ObserveOnly(config.Spec.Reconcile),
		c: r.Client, mc: mc, namespace: config.Namespace,
	}
	syncErrors := sync.run(ctx, &config.Spec)
	ctrlcommon.UpdateSyncStatus(ctx, r.Client, &config, sync.Observe, len(sync.Drift()) > 0, syncErrors)

	return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
}

// maintainerrSync applies one MaintainerrConfig in dependency order: the media
// server and the Sonarr and Radarr servers before the rule groups that refer to
// them, since Maintainerr resolves rule properties only for configured apps.
// With observe set it only reads, and records what it would have written.
type maintainerrSync struct {
	engine.Writes
	c         client.Reader
	mc        *maintainerrclient.Client
	namespace string
}

func (s *maintainerrSync) run(ctx context.Context, spec *curationv1alpha1.MaintainerrConfigSpec) []string {
	logger := log.FromContext(ctx)
	var syncErrors []string
	fail := func(section string, err error) {
		logger.Error(err, "failed to reconcile", "section", section)
		syncErrors = append(syncErrors, fmt.Sprintf("%s: %v", section, err))
	}

	settings, err := s.mc.GetSettings(ctx)
	if err != nil {
		fail("settings", fmt.Errorf("reading settings: %w", err))
		return syncErrors
	}

	if spec.MediaServer != nil {
		if err := s.mediaServer(ctx, spec.MediaServer, settings); err != nil {
			fail("mediaServer", err)
		}
	}

	servers := map[maintainerrclient.Arr]map[string]int{}
	for _, arr := range []struct {
		kind     maintainerrclient.Arr
		declared []curationv1alpha1.MaintainerrArrServer
	}{{maintainerrclient.Sonarr, spec.Sonarr}, {maintainerrclient.Radarr, spec.Radarr}} {
		ids, err := s.arrServers(ctx, arr.kind, arr.declared)
		if err != nil {
			fail(string(arr.kind), err)
		}
		servers[arr.kind] = ids
	}

	for _, svc := range []struct {
		name maintainerrclient.Service
		conn *curationv1alpha1.MaintainerrServiceConnection
	}{{maintainerrclient.Seerr, spec.Seerr}, {maintainerrclient.Tautulli, spec.Tautulli}} {
		if svc.conn == nil {
			continue
		}
		if err := s.service(ctx, svc.name, svc.conn); err != nil {
			fail(string(svc.name), err)
		}
	}

	if spec.Jobs != nil {
		if err := s.jobs(ctx, spec.Jobs, settings); err != nil {
			fail("jobs", err)
		}
	}

	if len(spec.RuleGroups) > 0 {
		syncErrors = append(syncErrors, s.ruleGroups(ctx, spec.RuleGroups, servers)...)
	}
	return syncErrors
}

// mediaServer sets the media server on a fresh Maintainerr and keeps its
// connection current. It never changes a media server that is already set:
// Maintainerr deletes every collection when it switches.
func (s *maintainerrSync) mediaServer(ctx context.Context, ms *curationv1alpha1.MaintainerrMediaServer, settings map[string]any) error {
	want := mediaPlex
	if ms.Jellyfin != nil {
		want = mediaJellyfin
	}
	current, _ := settings["media_server_type"].(string)
	if current != "" && current != want {
		return fmt.Errorf("maintainerr uses %s; switching it to %s deletes every collection, "+
			"so the operator leaves that to Maintainerr's settings page", current, want)
	}
	if ms.Jellyfin != nil {
		return s.jellyfin(ctx, ms.Jellyfin)
	}
	return s.plex(ctx, ms.Plex, settings, current == "")
}

func (s *maintainerrSync) jellyfin(ctx context.Context, jf *curationv1alpha1.MaintainerrJellyfin) error {
	key, err := reconciler.ResolveSecretKeyRef(ctx, s.c, s.namespace, jf.APIKeySecretRef)
	if err != nil {
		return fmt.Errorf("resolving Jellyfin API key: %w", err)
	}
	stored, err := s.mc.GetJellyfin(ctx)
	if err != nil {
		return fmt.Errorf("reading Jellyfin connection: %w", err)
	}
	url := serviceURL(jf.URL)
	if stored["jellyfin_url"] == url && stored["jellyfin_api_key"] == key &&
		(jf.UserID == "" || stored["jellyfin_user_id"] == jf.UserID) {
		return nil
	}
	conn := map[string]any{"jellyfin_url": url, "jellyfin_api_key": key}
	if jf.UserID != "" {
		conn["jellyfin_user_id"] = jf.UserID
	}
	return s.Apply("mediaServer", mediaJellyfin, func() error { return s.mc.SaveJellyfin(ctx, conn) })
}

func (s *maintainerrSync) plex(ctx context.Context, plex *curationv1alpha1.MaintainerrPlex, settings map[string]any, fresh bool) error {
	token, err := reconciler.ResolveSecretKeyRef(ctx, s.c, s.namespace, plex.TokenSecretRef)
	if err != nil {
		return fmt.Errorf("resolving Plex token: %w", err)
	}
	if fresh {
		err := s.Apply("mediaServer", "type", func() error { return s.mc.SetMediaServerType(ctx, mediaPlex) })
		if err != nil {
			return fmt.Errorf("setting Plex as the media server: %w", err)
		}
	}
	// Maintainerr reads the token back masked only, and refuses server fields
	// until a token is stored, so the token goes first.
	if stored, _ := settings["plex_auth_token"].(string); stored != maskPlexToken(token) {
		err := s.Apply("mediaServer", "plexToken", func() error { return s.mc.SavePlexToken(ctx, token) })
		if err != nil {
			return fmt.Errorf("saving Plex token: %w", err)
		}
	}
	desired := plexFields(plex)
	if reconciler.Covers(settings, mustJSON(desired)) {
		return nil
	}
	return s.Apply("mediaServer", mediaPlex, func() error { return s.mc.UpdateSettings(ctx, desired) })
}

// plexFields renders the Plex server settings as Maintainerr stores them: a
// lowercase host without a scheme, SSL forced on for port 443, and manual mode
// on so Maintainerr does not swap in an address it discovers through plex.tv.
func plexFields(plex *curationv1alpha1.MaintainerrPlex) map[string]any {
	host := strings.ToLower(plex.Hostname)
	host = strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
	port := plex.Port
	if port == 0 {
		port = 32400
	}
	ssl := 0
	if (plex.SSL != nil && *plex.SSL) || port == 443 {
		ssl = 1
	}
	return map[string]any{
		"plex_hostname": host, "plex_port": port, "plex_ssl": ssl, "plex_manual_mode": 1,
	}
}

// maskPlexToken reproduces how Maintainerr masks a secret on read: up to six
// characters become "****", longer ones keep their first and last three.
func maskPlexToken(token string) string {
	switch {
	case token == "":
		return ""
	case len(token) <= 6:
		return "****"
	}
	return token[:3] + "..." + token[len(token)-3:]
}

// arrServers adds or updates the declared servers of a kind and returns every
// server's id by name, for rule groups to refer to.
func (s *maintainerrSync) arrServers(
	ctx context.Context, kind maintainerrclient.Arr, declared []curationv1alpha1.MaintainerrArrServer,
) (map[string]int, error) {
	stored, err := s.mc.ListArr(ctx, kind)
	if err != nil {
		return nil, fmt.Errorf("listing servers: %w", err)
	}
	added := false
	var errs []string
	for _, server := range declared {
		wasAdded, err := s.arrServer(ctx, kind, server, stored)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", server.Name, err))
		}
		added = added || wasAdded
	}
	if added {
		if stored, err = s.mc.ListArr(ctx, kind); err != nil {
			return nil, fmt.Errorf("listing servers: %w", err)
		}
	}
	ids := map[string]int{}
	for _, e := range stored {
		name, _ := e["serverName"].(string)
		if id, ok := e["id"].(float64); ok {
			ids[name] = int(id)
		}
	}
	if len(errs) > 0 {
		return ids, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return ids, nil
}

func (s *maintainerrSync) arrServer(
	ctx context.Context, kind maintainerrclient.Arr, server curationv1alpha1.MaintainerrArrServer, stored []map[string]any,
) (bool, error) {
	key, err := reconciler.ResolveSecretKeyRef(ctx, s.c, s.namespace, server.APIKeySecretRef)
	if err != nil {
		return false, fmt.Errorf("resolving API key: %w", err)
	}
	// Maintainerr lowercases the URL it stores.
	desired := map[string]any{"serverName": server.Name, "url": strings.ToLower(serviceURL(server.URL)), "apiKey": key}
	if server.TagExclusions != nil {
		desired["tagExclusions"] = *server.TagExclusions
	}
	if server.ExclusionTag != "" {
		desired["exclusionTag"] = server.ExclusionTag
	}
	if server.UntagOnUnexclude != nil {
		desired["untagOnUnexclude"] = *server.UntagOnUnexclude
	}

	var match map[string]any
	for _, e := range stored {
		if e["serverName"] != server.Name {
			continue
		}
		if match != nil {
			return false, fmt.Errorf("maintainerr has more than one %s server named %q", kind, server.Name)
		}
		match = e
	}
	if match == nil {
		return !s.Observe, s.Apply(string(kind), server.Name, func() error { return s.mc.AddArr(ctx, kind, desired) })
	}
	if reconciler.Covers(match, mustJSON(desired)) {
		return false, nil
	}
	id, ok := match["id"].(float64)
	if !ok {
		return false, fmt.Errorf("server %q has no valid id", server.Name)
	}
	return false, s.Apply(string(kind), server.Name, func() error { return s.mc.UpdateArr(ctx, kind, int(id), desired) })
}

func (s *maintainerrSync) service(
	ctx context.Context, svc maintainerrclient.Service, conn *curationv1alpha1.MaintainerrServiceConnection,
) error {
	key, err := reconciler.ResolveSecretKeyRef(ctx, s.c, s.namespace, conn.APIKeySecretRef)
	if err != nil {
		return fmt.Errorf("resolving API key: %w", err)
	}
	stored, err := s.mc.GetService(ctx, svc)
	if err != nil {
		return fmt.Errorf("reading connection: %w", err)
	}
	desired := map[string]any{"url": serviceURL(conn.URL), "api_key": key}
	if reconciler.Covers(stored, mustJSON(desired)) {
		return nil
	}
	return s.Apply(string(svc), string(svc), func() error { return s.mc.SaveService(ctx, svc, desired) })
}

func (s *maintainerrSync) jobs(ctx context.Context, jobs *curationv1alpha1.MaintainerrJobs, settings map[string]any) error {
	desired := buildMaintainerrJobs(jobs)
	if len(desired) == 0 || reconciler.Covers(settings, mustJSON(desired)) {
		return nil
	}
	return s.Apply("jobs", "jobs", func() error { return s.mc.UpdateSettings(ctx, desired) })
}

// buildMaintainerrJobs renders the job schedules under Maintainerr's names.
func buildMaintainerrJobs(jobs *curationv1alpha1.MaintainerrJobs) map[string]any {
	desired := map[string]any{}
	if jobs.Rules != "" {
		desired["rules_handler_job_cron"] = jobs.Rules
	}
	if jobs.Collections != "" {
		desired["collection_handler_job_cron"] = jobs.Collections
	}
	return desired
}

// serviceURL is a URL as Maintainerr stores it: trimmed, without trailing slashes.
func serviceURL(url string) string {
	return strings.TrimRight(strings.TrimSpace(url), "/")
}

// SetupWithManager sets up the controller with the Manager.
func (r *MaintainerrConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&curationv1alpha1.MaintainerrConfig{}, builder.WithPredicates(ctrlcommon.ConfigChangedPredicate())).
		Watches(&corev1.Secret{}, ctrlcommon.EnqueueForSecret(r.Client, &curationv1alpha1.MaintainerrConfigList{})).
		Named("maintainerrconfig").
		Complete(r)
}
