package indexers

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	indexersv1alpha1 "github.com/kyleseneker/media-operator/api/indexers/v1alpha1"
	flaresolverrclient "github.com/kyleseneker/media-operator/internal/client/flaresolverr"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
)

// FlareSolverrConfigReconciler reconciles a FlareSolverrConfig object.
type FlareSolverrConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=flaresolverrconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=flaresolverrconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=media-operator.dev,resources=flaresolverrconfigs/finalizers,verbs=update

func (r *FlareSolverrConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var config indexersv1alpha1.FlareSolverrConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if done, after := ctrlcommon.HandleLifecycle(ctx, r.Client, r.Recorder, &config, nil); done {
		return ctrl.Result{RequeueAfter: after}, nil
	}

	tlsCfg, err := engine.ResolveTLSConfig(ctx, r.Client, config.Namespace, config.Spec.Connection.TLS)
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	hc, err := engine.NewHTTPClient(config.Spec.Connection.URL, engine.AuthNone, engine.WithTLSConfig(tlsCfg), engine.WithAppLabel("flaresolverr"))
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonInvalidConfig, err.Error())
		return ctrl.Result{}, nil
	}
	fc := flaresolverrclient.NewClient(hc)

	// Health check
	if err := fc.Ping(ctx); err != nil {
		logger.Error(err, "FlareSolverr unreachable")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonAppUnreachable, err.Error())
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	var syncErrors []string

	// Reconcile sessions
	managed, err := reconcileFlareSolverrSessions(ctx, fc, config.Spec.Sessions,
		ctrlcommon.PruneEnabled(config.Spec.Reconcile), config.Status.ManagedResources["sessions"])
	if err != nil {
		logger.Error(err, "failed to reconcile sessions")
		syncErrors = append(syncErrors, fmt.Sprintf("sessions: %v", err))
	}
	ctrlcommon.UpdateStatusManaged(&config, map[string][]string{"sessions": managed})

	// Update active session count
	if sessions, err := fc.ListSessions(ctx); err == nil {
		config.Status.ActiveSessions = len(sessions)
	}

	if len(syncErrors) > 0 {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, false, engine.ReasonSyncFailed, fmt.Sprintf("partial sync failure: %v", syncErrors))
	} else {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, true, engine.ReasonSynced, "all configuration sections synced")
	}

	return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
}

// reconcileFlareSolverrSessions creates the declared sessions and, when prune is
// enabled, destroys ones this operator previously created that have since been
// dropped from the spec. Sessions it did not create are left alone: Prowlarr
// opens sessions on demand, and tearing those down mid-request breaks the very
// indexers FlareSolverr exists to serve.
// Returns the session names now under management.
func reconcileFlareSolverrSessions(ctx context.Context, fc *flaresolverrclient.Client, desired []indexersv1alpha1.FlareSolverrSession, prune bool, previouslyManaged []string) ([]string, error) {
	existing, err := fc.ListSessions(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing sessions: %w", err)
	}

	existingSet := make(map[string]bool, len(existing))
	for _, s := range existing {
		existingSet[s] = true
	}

	desiredSet := make(map[string]bool, len(desired))
	for _, s := range desired {
		desiredSet[s.Name] = true
	}

	managed := make([]string, 0, len(desired))
	for _, s := range desired {
		if !existingSet[s.Name] {
			if err := fc.CreateSession(ctx, s.Name); err != nil {
				return managed, fmt.Errorf("creating session %q: %w", s.Name, err)
			}
		}
		managed = append(managed, s.Name)
	}

	if !prune {
		return managed, nil
	}

	managedSet := make(map[string]bool, len(previouslyManaged))
	for _, s := range previouslyManaged {
		managedSet[s] = true
	}
	for _, s := range existing {
		if desiredSet[s] || !managedSet[s] {
			continue
		}
		if err := fc.DestroySession(ctx, s); err != nil {
			return managed, fmt.Errorf("destroying session %q: %w", s, err)
		}
	}

	return managed, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FlareSolverrConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&indexersv1alpha1.FlareSolverrConfig{}).
		Named("flaresolverrconfig").
		Complete(r)
}
