package downloads

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
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	downloadsv1alpha1 "github.com/kyleseneker/media-operator/api/downloads/v1alpha1"
	sabnzbdclient "github.com/kyleseneker/media-operator/internal/client/sabnzbd"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/metrics"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

type SabnzbdConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=sabnzbdconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=sabnzbdconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=media-operator.dev,resources=sabnzbdconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *SabnzbdConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var config downloadsv1alpha1.SabnzbdConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if done, after := ctrlcommon.HandleLifecycle(ctx, r.Client, r.Recorder, &config, nil); done {
		return ctrl.Result{RequeueAfter: after}, nil
	}

	apiKey, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, config.Spec.Connection.APIKeySecretRef)
	if err != nil {
		logger.Error(err, "failed to resolve API key")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	tlsCfg, err := engine.ResolveTLSConfig(ctx, r.Client, config.Namespace, config.Spec.Connection.TLS)
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	hc, err := engine.NewHTTPClient(config.Spec.Connection.URL, engine.AuthNone, engine.WithTLSConfig(tlsCfg), engine.WithAppLabel("sabnzbd"))
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonInvalidConfig, err.Error())
		return ctrl.Result{}, nil
	}
	sc := sabnzbdclient.NewClient(hc, apiKey)

	if err := sc.Ping(ctx); err != nil {
		logger.Error(err, "SABnzbd unreachable")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonAppUnreachable, err.Error())
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	current, err := sc.GetConfig(ctx)
	if err != nil {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, false, engine.ReasonSyncFailed, fmt.Sprintf("reading config: %v", err))
		return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
	}
	servers, syncErrors := r.resolveSabnzbdServers(ctx, &config)
	writes := planSabnzbd(current, config.Spec, servers, string(config.UID))

	if ctrlcommon.ObserveOnly(config.Spec.Reconcile) {
		for _, w := range writes {
			metrics.DriftObservedTotal.WithLabelValues("sabnzbd", w.resourceType, w.name).Inc()
		}
		var observeErr error
		if len(syncErrors) > 0 {
			observeErr = fmt.Errorf("%v", syncErrors)
		}
		ctrlcommon.UpdateObservationStatus(ctx, r.Status(), &config, len(writes) > 0, observeErr)
		return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
	}

	syncErrors = append(syncErrors, applySabnzbd(ctx, sc, writes)...)
	// A fresh SABnzbd creates its default categories on the first category
	// write and discards that write, so check once more after writing.
	if len(writes) > 0 && len(syncErrors) == 0 {
		if current, err = sc.GetConfig(ctx); err != nil {
			syncErrors = append(syncErrors, fmt.Sprintf("re-reading config: %v", err))
		} else {
			syncErrors = append(syncErrors, applySabnzbd(ctx, sc, planSabnzbd(current, config.Spec, servers, string(config.UID)))...)
		}
	}

	if len(syncErrors) > 0 {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, false, engine.ReasonSyncFailed, fmt.Sprintf("partial sync: %v", syncErrors))
	} else {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, true, engine.ReasonSynced, "all configuration synced")
	}
	return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
}

func (r *SabnzbdConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&downloadsv1alpha1.SabnzbdConfig{}, builder.WithPredicates(ctrlcommon.ConfigChangedPredicate())).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			return ctrlcommon.FindConfigsBySecret(ctx, r.Client, obj, &downloadsv1alpha1.SabnzbdConfigList{}, func(list *downloadsv1alpha1.SabnzbdConfigList) []reconcile.Request {
				var reqs []reconcile.Request
				for _, c := range list.Items {
					if c.Spec.Connection.APIKeySecretRef.Name == obj.GetName() {
						reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&c)})
					}
				}
				return reqs
			})
		})).
		Named("sabnzbdconfig").
		Complete(r)
}

func buildSabnzbdFolderValues(f *downloadsv1alpha1.SabnzbdFolders) map[string]string {
	vals := make(map[string]string)
	if f.CompleteDir != "" {
		vals["complete_dir"] = f.CompleteDir
	}
	if f.IncompleteDir != "" {
		vals["download_dir"] = f.IncompleteDir
	}
	if f.TempDownloadDir != "" {
		vals["tmp_dir"] = f.TempDownloadDir
	}
	if f.NzbBackupDir != "" {
		vals["nzb_backup_dir"] = f.NzbBackupDir
	}
	if f.ScriptDir != "" {
		vals["script_dir"] = f.ScriptDir
	}
	return vals
}

func buildSabnzbdGeneralValues(g *downloadsv1alpha1.SabnzbdGeneral) map[string]string {
	vals := make(map[string]string)
	if g.DownloadSpeedLimit != "" {
		vals["bandwidth_max"] = g.DownloadSpeedLimit
	}
	if g.PreCheck != nil {
		vals["pre_check"] = ctrlcommon.BoolTo01(*g.PreCheck)
	}
	if g.PauseOnPostProcessing != nil {
		vals["pause_on_post_processing"] = ctrlcommon.BoolTo01(*g.PauseOnPostProcessing)
	}
	if g.AutoSort != nil {
		vals["auto_sort"] = ctrlcommon.BoolTo01(*g.AutoSort)
	}
	if g.ScriptDir != "" {
		vals["script_dir"] = g.ScriptDir
	}
	return vals
}

func sabnzbdServerValues(srv downloadsv1alpha1.SabnzbdServer) map[string]string {
	vals := map[string]string{
		"host": srv.Host,
		"port": strconv.Itoa(srv.Port),
	}
	if srv.SSL != nil {
		vals["ssl"] = ctrlcommon.BoolTo01(*srv.SSL)
	}
	if srv.Connections != nil {
		vals["connections"] = strconv.Itoa(*srv.Connections)
	}
	if srv.Priority != nil {
		vals["priority"] = strconv.Itoa(*srv.Priority)
	}
	if srv.Retention != nil {
		vals["retention"] = strconv.Itoa(*srv.Retention)
	}
	if srv.Enable != nil {
		vals["enable"] = ctrlcommon.BoolTo01(*srv.Enable)
	}

	return vals
}

// applySabnzbd performs the planned writes and returns the failures.
func applySabnzbd(ctx context.Context, sc *sabnzbdclient.Client, writes []sabWrite) []string {
	var problems []string
	for _, w := range writes {
		metrics.DriftCorrectedTotal.WithLabelValues("sabnzbd", w.resourceType, w.name).Inc()
		var err error
		if w.section == "misc" {
			err = sc.SetConfig(ctx, w.section, w.name, w.values["value"])
		} else {
			err = sc.SetNamed(ctx, w.section, w.name, w.values)
		}
		if err != nil {
			log.FromContext(ctx).Error(err, "failed to write sabnzbd config", "section", w.section, "name", w.name)
			problems = append(problems, fmt.Sprintf("%s(%s): %v", w.resourceType, w.name, err))
			continue
		}
		reconciler.RecordSecrets(w.secretKey, w.secretDigest)
	}
	return problems
}

// sabWrite is one set_config call the reconcile needs.
type sabWrite struct {
	section      string // misc, servers or categories
	name         string // the misc keyword, or the server or category name
	values       map[string]string
	resourceType string
	secretKey    string
	secretDigest string
}

// resolveSabnzbdServers reads each server's credentials. A server whose Secret
// cannot be read is skipped and reported, so the rest still reconcile.
func (r *SabnzbdConfigReconciler) resolveSabnzbdServers(
	ctx context.Context, config *downloadsv1alpha1.SabnzbdConfig,
) (map[string]map[string]string, []string) {
	servers := map[string]map[string]string{}
	var problems []string
	for _, srv := range config.Spec.Servers {
		vals := sabnzbdServerValues(srv)
		ok := true
		for field, ref := range map[string]*commonv1alpha1.SecretKeyRef{"username": srv.UsernameSecretRef, "password": srv.PasswordSecretRef} {
			if ref == nil {
				continue
			}
			value, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, *ref)
			if err != nil {
				problems = append(problems, fmt.Sprintf("server(%s).%s: %v", srv.Name, field, err))
				ok = false
				continue
			}
			vals[field] = value
		}
		if ok {
			servers[srv.Name] = vals
		}
	}
	return servers, problems
}

// planSabnzbd compares the declared settings with get_config and returns only
// the writes needed. Writing every setting on every pass made each reconcile a
// burst of set_config calls and hid which settings actually drifted.
func planSabnzbd(
	current map[string]any, spec downloadsv1alpha1.SabnzbdConfigSpec, servers map[string]map[string]string, owner string,
) []sabWrite {
	var writes []sabWrite

	misc := map[string]string{}
	if spec.Folders != nil {
		maps.Copy(misc, buildSabnzbdFolderValues(spec.Folders))
	}
	if spec.General != nil {
		maps.Copy(misc, buildSabnzbdGeneralValues(spec.General))
	}
	currentMisc, _ := current["misc"].(map[string]any)
	for _, key := range slices.Sorted(maps.Keys(misc)) {
		// A switch reads back as 0/1 until written and as true/false after.
		value := currentMisc[key]
		if b, ok := value.(bool); ok {
			value = ctrlcommon.BoolTo01(b)
		}
		out, err := reconciler.MergeDesired(map[string]any{key: value}, map[string]any{key: misc[key]})
		if err == nil && !out.Changed {
			continue
		}
		writes = append(writes, sabWrite{section: "misc", name: key, values: map[string]string{"value": misc[key]}, resourceType: "settings"})
	}

	for _, srv := range spec.Servers {
		vals, ok := servers[srv.Name]
		if !ok {
			continue
		}
		key := fmt.Sprintf("sabnzbd/%s/server/%q", owner, srv.Name)
		if w, needed := planNamed(current, "servers", srv.Name, vals, key); needed {
			w.resourceType = "servers"
			writes = append(writes, w)
		}
	}

	for _, cat := range spec.Categories {
		if w, needed := planNamed(current, "categories", cat.Name, sabnzbdCategoryValues(cat), ""); needed {
			w.resourceType = "categories"
			writes = append(writes, w)
		}
	}
	return writes
}

// planNamed decides whether a server or category needs writing. Passwords read
// back masked, so a change there is noticed through the remembered digest.
func planNamed(current map[string]any, section, name string, vals map[string]string, secretKey string) (sabWrite, bool) {
	w := sabWrite{section: section, name: name, values: vals, secretKey: secretKey}
	entries, _ := current[section].([]any)
	for _, e := range entries {
		entry, _ := e.(map[string]any)
		if entry["name"] != name {
			continue
		}
		desired := make(map[string]any, len(vals))
		for k, v := range vals {
			desired[k] = v
		}
		out, err := reconciler.MergeDesired(entry, desired)
		if err != nil {
			return w, true
		}
		w.secretDigest = out.SecretDigest
		return w, out.Changed || reconciler.SecretsChangedSince(secretKey, out.SecretDigest)
	}
	// A new entry reads back with its password masked; remember what is
	// written now, or the next pass rewrites it once.
	if password, ok := vals["password"]; ok {
		if out, err := reconciler.MergeDesired(map[string]any{"password": "**********"}, map[string]any{"password": password}); err == nil {
			w.secretDigest = out.SecretDigest
		}
	}
	return w, true
}

func sabnzbdCategoryValues(cat downloadsv1alpha1.SabnzbdCategory) map[string]string {
	vals := map[string]string{}
	if cat.Dir != "" {
		vals["dir"] = cat.Dir
	}
	if cat.Script != "" {
		vals["script"] = cat.Script
	}
	if cat.Priority != nil {
		vals["priority"] = strconv.Itoa(*cat.Priority)
	}
	return vals
}
