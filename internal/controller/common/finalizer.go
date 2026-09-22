package common

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

// Finalizer is added to every config resource so deletion can be handled.
const Finalizer = "media-operator.dev/finalizer"

// DeletionGiveUpAfter bounds how long deletion will retry against an
// unreachable app before releasing the finalizer, so a dead app can never
// block deletion of the resource indefinitely.
const DeletionGiveUpAfter = 10 * time.Minute

// DeletionPolicyDelete removes the resources the operator created.
const DeletionPolicyDelete = "delete"

// EnsureFinalizer adds the finalizer if it is missing.
func EnsureFinalizer(ctx context.Context, c client.Client, obj ConfigResource) error {
	if controllerutil.ContainsFinalizer(obj, Finalizer) {
		return nil
	}
	controllerutil.AddFinalizer(obj, Finalizer)
	return c.Update(ctx, obj)
}

// HandleDeletion runs when the resource is being deleted. It calls remove only
// when the deletion policy is "delete", then releases the finalizer. If remove
// fails it retries until DeletionGiveUpAfter has elapsed since deletion was
// requested, after which the finalizer is released regardless.
func HandleDeletion(ctx context.Context, c client.Client, recorder events.EventRecorder, obj ConfigResource, remove func(context.Context) error) (bool, error) {
	if !controllerutil.ContainsFinalizer(obj, Finalizer) {
		return false, nil
	}
	logger := log.FromContext(ctx)

	policy := ""
	if rc := obj.GetReconcileConfig(); rc != nil && rc.DeletionPolicy != nil {
		policy = *rc.DeletionPolicy
	}

	if policy == DeletionPolicyDelete {
		// Observe forbids application changes, including cleanup during deletion.
		// Unsupported cleanup must be reported instead of silently orphaning.
		var cleanupErr error
		switch {
		case ObserveOnly(obj.GetReconcileConfig()):
			cleanupErr = fmt.Errorf("deletionPolicy delete conflicts with driftPolicy observe")
		case remove == nil:
			cleanupErr = fmt.Errorf("deletionPolicy delete is not supported by this integration")
		default:
			cleanupErr = remove(ctx)
		}
		if err := cleanupErr; err != nil {
			if expired(obj) {
				logger.Error(err, "giving up removing remote resources; releasing finalizer")
				if recorder != nil {
					recorder.Eventf(obj, nil, corev1.EventTypeWarning, engine.ReasonSyncFailed, "ReleaseFinalizer",
						"released finalizer after %s without removing remote resources: %v", DeletionGiveUpAfter, err)
				}
			} else {
				return true, err
			}
		}
	}

	controllerutil.RemoveFinalizer(obj, Finalizer)
	if err := c.Update(ctx, obj); err != nil && !errors.IsNotFound(err) {
		return true, err
	}
	return true, nil
}

func expired(obj ConfigResource) bool {
	ts := obj.GetDeletionTimestamp()
	if ts == nil {
		return false
	}
	return metav1.Now().Sub(ts.Time) > DeletionGiveUpAfter
}

// HandleLifecycle performs the deletion and finalizer bookkeeping every
// controller shares. It returns done=true when the caller should stop
// reconciling, either because the object is being deleted or because the
// finalizer could not be written.
func HandleLifecycle(ctx context.Context, c client.Client, recorder events.EventRecorder, obj ConfigResource, remove func(context.Context) error) (bool, time.Duration) {
	if !obj.GetDeletionTimestamp().IsZero() {
		if _, err := HandleDeletion(ctx, c, recorder, obj, remove); err != nil {
			return true, 30 * time.Second
		}
		return true, 0
	}
	if err := EnsureFinalizer(ctx, c, obj); err != nil {
		return true, 30 * time.Second
	}
	if rc := obj.GetReconcileConfig(); rc != nil && rc.DeletionPolicy != nil && *rc.DeletionPolicy == DeletionPolicyDelete {
		if remove == nil || ObserveOnly(rc) {
			message := "deletionPolicy delete is not supported by this integration; use orphan"
			if ObserveOnly(rc) {
				message = "deletionPolicy delete conflicts with driftPolicy observe; use orphan"
			}
			UpdateStatusUnreachable(ctx, c.Status(), obj, engine.ReasonInvalidConfig, message)
			return true, ReconcileInterval(rc)
		}
	}
	return false, 0
}

// CleanupApp resolves only the credentials needed for deletion. Unrelated spec
// Secrets must not prevent cleanup, and empty ownership needs no app connection.
func CleanupApp(ctx context.Context, c client.Reader, recorder events.EventRecorder, obj ConfigResource, connection commonv1alpha1.AppConnection, def engine.AppDefinition, app string) error {
	managed := *obj.GetManagedResources()
	hasResources := false
	for _, endpoint := range def.Resources {
		if endpoint.Prunable && len(managed[endpoint.Name]) > 0 {
			hasResources = true
			break
		}
	}
	if !hasResources {
		return nil
	}
	apiKey, err := reconciler.ResolveSecretKeyRef(ctx, c, obj.GetNamespace(), connection.APIKeySecretRef)
	if err != nil {
		return err
	}
	tlsConfig, err := engine.ResolveTLSConfig(ctx, c, obj.GetNamespace(), connection.TLS)
	if err != nil {
		return err
	}
	hc, err := engine.NewHTTPClient(connection.URL, engine.AuthAPIKey, engine.WithAPIKey(apiKey), engine.WithTLSConfig(tlsConfig), engine.WithAppLabel(app))
	if err != nil {
		return err
	}
	deleted, err := engine.DeleteManagedResources(ctx, hc, def, managed)
	EmitPruneEvents(recorder, obj, deleted)
	return err
}
