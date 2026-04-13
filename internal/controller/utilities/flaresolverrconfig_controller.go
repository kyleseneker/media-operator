package utilities

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	utilitiesv1alpha1 "github.com/kyleseneker/media-operator/api/utilities/v1alpha1"
	flaresolverrclient "github.com/kyleseneker/media-operator/internal/client/flaresolverr"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
)

// FlareSolverrConfigReconciler reconciles a FlareSolverrConfig object.
type FlareSolverrConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=flaresolverrconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=flaresolverrconfigs/status,verbs=get;update;patch

func (r *FlareSolverrConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var config utilitiesv1alpha1.FlareSolverrConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
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
	if err := reconcileFlareSolverrSessions(ctx, fc, config.Spec.Sessions); err != nil {
		logger.Error(err, "failed to reconcile sessions")
		syncErrors = append(syncErrors, fmt.Sprintf("sessions: %v", err))
	}

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

func reconcileFlareSolverrSessions(ctx context.Context, fc *flaresolverrclient.Client, desired []utilitiesv1alpha1.FlareSolverrSession) error {
	existing, err := fc.ListSessions(ctx)
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	existingSet := make(map[string]bool, len(existing))
	for _, s := range existing {
		existingSet[s] = true
	}

	desiredSet := make(map[string]bool, len(desired))
	for _, s := range desired {
		desiredSet[s.Name] = true
	}

	// Create missing sessions
	for _, s := range desired {
		if !existingSet[s.Name] {
			if err := fc.CreateSession(ctx, s.Name); err != nil {
				return fmt.Errorf("creating session %q: %w", s.Name, err)
			}
		}
	}

	// Destroy sessions not in the desired list (only managed sessions)
	for _, s := range existing {
		if !desiredSet[s] {
			if err := fc.DestroySession(ctx, s); err != nil {
				return fmt.Errorf("destroying session %q: %w", s, err)
			}
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FlareSolverrConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&utilitiesv1alpha1.FlareSolverrConfig{}).
		Named("flaresolverrconfig").
		Complete(r)
}
