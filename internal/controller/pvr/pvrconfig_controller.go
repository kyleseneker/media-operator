package pvr

import (
	"context"
	"time"

	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	pvrclient "github.com/kyleseneker/media-operator/internal/client/pvr"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

// pvrConfig is what reconciling Sonarr, Radarr, Lidarr or Readarr reads from
// the app's config resource. The four apps share one API shape, so everything
// else about reconciling them is the same.
type pvrConfig struct {
	app        string // the app's metrics label and cleanup name
	apiVersion string
	obj        ctrlcommon.ConfigResource
	spec       any // the whole spec, for its settings sections
	connection commonv1alpha1.AppConnection
	policy     *commonv1alpha1.ReconcileConfig
	// options holds the spec's resources, before their secrets are resolved,
	// and the app's field names.
	options pvrclient.Options
}

func reconcilePVR(ctx context.Context, c client.Client, recorder events.EventRecorder, cfg pvrConfig) (ctrl.Result, error) {
	obj, conn, namespace := cfg.obj, cfg.connection, cfg.obj.GetNamespace()

	cleanup := func(ctx context.Context) error {
		return ctrlcommon.CleanupApp(ctx, c, recorder, obj, conn, pvrclient.Definition(cfg.apiVersion), cfg.app)
	}
	if done, after := ctrlcommon.HandleLifecycle(ctx, c, recorder, obj, cleanup); done {
		return ctrl.Result{RequeueAfter: after}, nil
	}

	missingSecret := func(err error) (ctrl.Result, error) {
		ctrlcommon.UpdateStatusUnreachable(ctx, c.Status(), obj, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	apiKey, err := reconciler.ResolveSecretKeyRef(ctx, c, namespace, conn.APIKeySecretRef)
	if err != nil {
		return missingSecret(err)
	}
	opts := cfg.options
	if opts.DCSecrets, err = ctrlcommon.ResolveDownloadClientSecrets(ctx, c, namespace, opts.DownloadClients); err != nil {
		return missingSecret(err)
	}
	tlsCfg, err := engine.ResolveTLSConfig(ctx, c, namespace, conn.TLS)
	if err != nil {
		return missingSecret(err)
	}

	hc, err := engine.NewHTTPClient(conn.URL, engine.AuthAPIKey,
		engine.WithOwner(namespace+"/"+obj.GetName()+"/"+string(obj.GetUID())),
		engine.WithAPIKey(apiKey), engine.WithTLSConfig(tlsCfg), engine.WithAppLabel(cfg.app))
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, c.Status(), obj, engine.ReasonInvalidConfig, err.Error())
		return ctrl.Result{}, nil
	}
	if err := hc.Ping(ctx, "/api/"+cfg.apiVersion+"/system/status"); err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, c.Status(), obj, engine.ReasonAppUnreachable, err.Error())
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	if opts.Indexers, err = ctrlcommon.ResolveIndexerSecrets(ctx, c, namespace, opts.Indexers); err != nil {
		return missingSecret(err)
	}
	if opts.Notifications, err = ctrlcommon.ResolveNotificationSecrets(ctx, c, namespace, opts.Notifications); err != nil {
		return missingSecret(err)
	}
	if opts.ImportLists, err = ctrlcommon.ResolveImportListSecrets(ctx, c, namespace, opts.ImportLists); err != nil {
		return missingSecret(err)
	}

	result, err := pvrclient.Reconcile(ctx, hc, cfg.apiVersion, cfg.spec, opts, ctrlcommon.Policy(cfg.policy), *obj.GetManagedResources())
	if err != nil {
		ctrlcommon.UpdateStatus(ctx, c.Status(), obj, false, engine.ReasonSyncFailed, err.Error())
		return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(cfg.policy)}, nil
	}
	ctrlcommon.UpdateStatusManaged(obj, result.Managed)
	ctrlcommon.EmitPruneEvents(recorder, obj, result.Pruned)
	ctrlcommon.UpdateResultStatus(ctx, c.Status(), obj, result)

	return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(cfg.policy)}, nil
}

// pvrReferencesSecret reports whether a PVR config reads the named Secret, so
// a change to it re-reconciles the config.
func pvrReferencesSecret(conn commonv1alpha1.AppConnection, downloadClients []commonv1alpha1.DownloadClient,
	indexers []commonv1alpha1.Indexer, notifications []commonv1alpha1.Notification,
	importLists []commonv1alpha1.ImportList, name string,
) bool {
	return conn.APIKeySecretRef.Name == name ||
		ctrlcommon.DownloadClientReferencesSecret(downloadClients, name) ||
		ctrlcommon.ListsReferenceSecret(indexers, notifications, importLists, name)
}
