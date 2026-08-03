package common

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kyleseneker/media-operator/internal/engine"
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
func HandleDeletion(ctx context.Context, c client.Client, recorder record.EventRecorder, obj ConfigResource, remove func(context.Context) error) (bool, error) {
	if !controllerutil.ContainsFinalizer(obj, Finalizer) {
		return false, nil
	}
	logger := log.FromContext(ctx)

	policy := ""
	if rc := obj.GetReconcileConfig(); rc != nil && rc.DeletionPolicy != nil {
		policy = *rc.DeletionPolicy
	}

	if policy == DeletionPolicyDelete && remove != nil {
		if err := remove(ctx); err != nil {
			if expired(obj) {
				logger.Error(err, "giving up removing remote resources; releasing finalizer")
				if recorder != nil {
					recorder.Event(obj, "Warning", engine.ReasonSyncFailed,
						fmt.Sprintf("released finalizer after %s without removing remote resources: %v", DeletionGiveUpAfter, err))
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
