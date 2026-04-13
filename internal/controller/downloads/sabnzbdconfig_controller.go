package downloads

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

	downloadsv1alpha1 "github.com/kyleseneker/media-operator/api/downloads/v1alpha1"
	sabnzbdclient "github.com/kyleseneker/media-operator/internal/client/sabnzbd"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

type SabnzbdConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=sabnzbdconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=sabnzbdconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *SabnzbdConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var config downloadsv1alpha1.SabnzbdConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
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

	var syncErrors []string

	// Folders
	if f := config.Spec.Folders; f != nil {
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
		if len(vals) > 0 {
			if err := sc.SetConfigMulti(ctx, "misc", vals); err != nil {
				syncErrors = append(syncErrors, fmt.Sprintf("folders: %v", err))
			}
		}
	}

	// General settings
	if g := config.Spec.General; g != nil {
		vals := make(map[string]string)
		if g.DownloadSpeedLimit != "" {
			vals["bandwidth_max"] = g.DownloadSpeedLimit
		}
		if g.PreCheck != nil {
			vals["pre_check"] = ctrlcommon.BoolTo01(*g.PreCheck)
		}
		if len(vals) > 0 {
			if err := sc.SetConfigMulti(ctx, "misc", vals); err != nil {
				syncErrors = append(syncErrors, fmt.Sprintf("general: %v", err))
			}
		}
	}

	// Servers
	for i, srv := range config.Spec.Servers {
		vals := map[string]string{
			"name": srv.Name,
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
		if srv.UsernameSecretRef != nil {
			username, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, *srv.UsernameSecretRef)
			if err != nil {
				syncErrors = append(syncErrors, fmt.Sprintf("server(%s).username: %v", srv.Name, err))
				continue
			}
			vals["username"] = username
		}
		if srv.PasswordSecretRef != nil {
			password, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, *srv.PasswordSecretRef)
			if err != nil {
				syncErrors = append(syncErrors, fmt.Sprintf("server(%s).password: %v", srv.Name, err))
				continue
			}
			vals["password"] = password
		}
		section := fmt.Sprintf("servers,%d", i)
		if err := sc.SetConfigMulti(ctx, section, vals); err != nil {
			syncErrors = append(syncErrors, fmt.Sprintf("server(%s): %v", srv.Name, err))
		}
	}

	// Categories
	for i, cat := range config.Spec.Categories {
		vals := map[string]string{
			"name": cat.Name,
		}
		if cat.Dir != "" {
			vals["dir"] = cat.Dir
		}
		if cat.Script != "" {
			vals["script"] = cat.Script
		}
		if cat.Priority != nil {
			vals["priority"] = strconv.Itoa(*cat.Priority)
		}
		section := fmt.Sprintf("categories,%d", i)
		if err := sc.SetConfigMulti(ctx, section, vals); err != nil {
			syncErrors = append(syncErrors, fmt.Sprintf("category(%s): %v", cat.Name, err))
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
		For(&downloadsv1alpha1.SabnzbdConfig{}).
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
