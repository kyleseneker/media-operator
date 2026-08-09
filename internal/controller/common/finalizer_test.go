package common

import (
	"context"
	"errors"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	pvrv1alpha1 "github.com/kyleseneker/media-operator/api/pvr/v1alpha1"
)

func newConfig(policy string, deletedAgo time.Duration) *pvrv1alpha1.SonarrConfig {
	c := &pvrv1alpha1.SonarrConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "arr", Finalizers: []string{Finalizer}},
	}
	if policy != "" {
		c.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{DeletionPolicy: &policy}
	}
	if deletedAgo > 0 {
		ts := metav1.NewTime(time.Now().Add(-deletedAgo))
		c.DeletionTimestamp = &ts
	}
	return c
}

func testClient(o *pvrv1alpha1.SonarrConfig) *fake.ClientBuilder {
	_ = pvrv1alpha1.AddToScheme(scheme.Scheme)
	return fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(o)
}

func TestOrphanIsTheDefaultAndDoesNotRemove(t *testing.T) {
	c := newConfig("", time.Minute)
	cl := testClient(c).Build()
	called := false
	handled, err := HandleDeletion(context.Background(), cl, nil, c, func(context.Context) error {
		called = true
		return nil
	})
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if called {
		t.Error("remove must not run under the default orphan policy")
	}
	if controllerutil.ContainsFinalizer(c, Finalizer) {
		t.Error("finalizer should have been released")
	}
}

func TestDeletePolicyRemovesThenReleases(t *testing.T) {
	c := newConfig(DeletionPolicyDelete, time.Minute)
	cl := testClient(c).Build()
	called := false
	handled, err := HandleDeletion(context.Background(), cl, nil, c, func(context.Context) error {
		called = true
		return nil
	})
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if !called {
		t.Error("remove should run under the delete policy")
	}
	if controllerutil.ContainsFinalizer(c, Finalizer) {
		t.Error("finalizer should have been released")
	}
}

func TestUnreachableAppRetriesButDoesNotWedgeForever(t *testing.T) {
	// Recently deleted: keep retrying, hold the finalizer.
	c := newConfig(DeletionPolicyDelete, time.Minute)
	cl := testClient(c).Build()
	_, err := HandleDeletion(context.Background(), cl, nil, c, func(context.Context) error {
		return errors.New("app unreachable")
	})
	if err == nil {
		t.Error("expected an error so the request is retried")
	}
	if !controllerutil.ContainsFinalizer(c, Finalizer) {
		t.Error("finalizer must be held while still within the give-up window")
	}

	// Past the give-up window: release regardless.
	c2 := newConfig(DeletionPolicyDelete, DeletionGiveUpAfter+time.Minute)
	cl2 := testClient(c2).Build()
	handled, err := HandleDeletion(context.Background(), cl2, nil, c2, func(context.Context) error {
		return errors.New("app still unreachable")
	})
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	if controllerutil.ContainsFinalizer(c2, Finalizer) {
		t.Error("a dead app must not wedge deletion past the give-up window")
	}
}
