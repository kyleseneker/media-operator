package transcode

import (
	"context"
	"encoding/json"
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

	transcodev1alpha1 "github.com/kyleseneker/media-operator/api/transcode/v1alpha1"
	tdarrclient "github.com/kyleseneker/media-operator/internal/client/tdarr"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

// TdarrConfigReconciler reconciles a TdarrConfig object.
type TdarrConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=tdarrconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=tdarrconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *TdarrConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var config transcodev1alpha1.TdarrConfig
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

	hc, err := engine.NewHTTPClient(config.Spec.Connection.URL, engine.AuthAPIKey, engine.WithAPIKey(apiKey), engine.WithAPIKeyHeader("x-api-key"), engine.WithTLSConfig(tlsCfg), engine.WithAppLabel("tdarr"))
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonInvalidConfig, err.Error())
		return ctrl.Result{}, nil
	}
	tc := tdarrclient.NewClient(hc)

	// Health check
	if err := tc.Ping(ctx); err != nil {
		logger.Error(err, "Tdarr unreachable")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonAppUnreachable, err.Error())
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	var syncErrors []string

	// Reconcile libraries
	for _, lib := range config.Spec.Libraries {
		if err := reconcileTdarrLibrary(ctx, tc, lib); err != nil {
			logger.Error(err, "failed to reconcile library", "id", lib.ID)
			syncErrors = append(syncErrors, fmt.Sprintf("library(%s): %v", lib.ID, err))
		}
	}

	// Reconcile flows
	for _, flow := range config.Spec.Flows {
		if err := reconcileTdarrFlow(ctx, tc, flow); err != nil {
			logger.Error(err, "failed to reconcile flow", "id", flow.ID)
			syncErrors = append(syncErrors, fmt.Sprintf("flow(%s): %v", flow.ID, err))
		}
	}

	// Reconcile workers
	if config.Spec.Workers != nil {
		if err := reconcileTdarrWorkers(ctx, tc, config.Spec.Workers); err != nil {
			logger.Error(err, "failed to reconcile workers")
			syncErrors = append(syncErrors, fmt.Sprintf("workers: %v", err))
		}
	}

	if len(syncErrors) > 0 {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, false, engine.ReasonSyncFailed, fmt.Sprintf("partial sync failure: %v", syncErrors))
	} else {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, true, engine.ReasonSynced, "all configuration sections synced")
	}

	return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
}

func reconcileTdarrLibrary(ctx context.Context, tc *tdarrclient.Client, lib transcodev1alpha1.TdarrLibrary) error {
	obj := map[string]interface{}{
		"_id":  lib.ID,
		"name": lib.Name,
	}
	if lib.Folder != "" {
		obj["folder"] = lib.Folder
	}
	if lib.Cache != "" {
		obj["cache"] = lib.Cache
	}
	if lib.Container != "" {
		obj["container"] = lib.Container
	}
	if lib.ContainerFilter != "" {
		obj["containerFilter"] = lib.ContainerFilter
	}
	if lib.FolderWatching != nil {
		obj["folderWatching"] = *lib.FolderWatching
	}
	if lib.ProcessLibrary != nil {
		obj["processLibrary"] = *lib.ProcessLibrary
	}
	if lib.ScanOnStart != nil {
		obj["scanOnStart"] = *lib.ScanOnStart
	}
	if lib.ScheduledScanFindNew != nil {
		obj["scheduledScanFindNew"] = *lib.ScheduledScanFindNew
	}
	if lib.ScannerThreadCount != nil {
		obj["scannerThreadCount"] = *lib.ScannerThreadCount
	}
	if lib.FFmpeg != nil {
		obj["ffmpeg"] = *lib.FFmpeg
	}
	if lib.Priority != nil {
		obj["priority"] = *lib.Priority
	}

	return tc.Upsert(ctx, "LibrarySettingsJSONDB", lib.ID, obj)
}

func reconcileTdarrFlow(ctx context.Context, tc *tdarrclient.Client, flow transcodev1alpha1.TdarrFlow) error {
	obj := map[string]interface{}{
		"_id":  flow.ID,
		"name": flow.Name,
	}
	if flow.Description != "" {
		obj["description"] = flow.Description
	}

	// Unmarshal RawExtension fields
	var flowPlugins interface{}
	if err := json.Unmarshal(flow.FlowPlugins.Raw, &flowPlugins); err != nil {
		return fmt.Errorf("unmarshaling flowPlugins: %w", err)
	}
	obj["flowPlugins"] = flowPlugins

	var flowEdges interface{}
	if err := json.Unmarshal(flow.FlowEdges.Raw, &flowEdges); err != nil {
		return fmt.Errorf("unmarshaling flowEdges: %w", err)
	}
	obj["flowEdges"] = flowEdges

	return tc.Upsert(ctx, "FlowsJSONDB", flow.ID, obj)
}

func reconcileTdarrWorkers(ctx context.Context, tc *tdarrclient.Client, workers *transcodev1alpha1.TdarrWorkers) error {
	nodes, err := tc.GetNodes(ctx)
	if err != nil {
		return fmt.Errorf("getting nodes: %w", err)
	}

	// Use the first node
	var nodeID string
	for id := range nodes {
		nodeID = id
		break
	}
	if nodeID == "" {
		return fmt.Errorf("no nodes registered")
	}

	workerLimits := []struct {
		workerType string
		limit      *int
	}{
		{"transcodeGPU", workers.TranscodeGPU},
		{"transcodeCPU", workers.TranscodeCPU},
		{"healthcheckGPU", workers.HealthcheckGPU},
		{"healthcheckCPU", workers.HealthcheckCPU},
	}

	for _, wl := range workerLimits {
		if wl.limit != nil {
			if err := tc.SetWorkerLimit(ctx, nodeID, wl.workerType, *wl.limit); err != nil {
				return fmt.Errorf("setting %s limit: %w", wl.workerType, err)
			}
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TdarrConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&transcodev1alpha1.TdarrConfig{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
			return ctrlcommon.FindConfigsBySecret(ctx, r.Client, obj, &transcodev1alpha1.TdarrConfigList{}, func(list *transcodev1alpha1.TdarrConfigList) []reconcile.Request {
				var reqs []reconcile.Request
				for _, c := range list.Items {
					if c.Spec.Connection.APIKeySecretRef.Name == obj.GetName() {
						reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&c)})
					}
				}
				return reqs
			})
		})).
		Named("tdarrconfig").
		Complete(r)
}
