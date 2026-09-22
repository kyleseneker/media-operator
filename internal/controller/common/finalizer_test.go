package common

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	pvrv1alpha1 "github.com/kyleseneker/media-operator/api/pvr/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/engine"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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

func TestUnsupportedOrObserveDeletionDoesNotSilentlySucceed(t *testing.T) {
	for _, observe := range []bool{false, true} {
		t.Run(fmt.Sprint(observe), func(t *testing.T) {
			obj := newConfig(DeletionPolicyDelete, time.Minute)
			policy := DriftPolicyObserve
			if observe {
				obj.Spec.Reconcile.DriftPolicy = &policy
			}
			c := testClient(obj).Build()
			called := false
			var remove func(context.Context) error
			if observe {
				remove = func(context.Context) error { called = true; return nil }
			}
			_, err := HandleDeletion(context.Background(), c, nil, obj, remove)
			if err == nil {
				t.Fatal("expected explicit cleanup failure")
			}
			if called {
				t.Fatal("observe mode must not invoke cleanup")
			}
			if !controllerutil.ContainsFinalizer(obj, Finalizer) {
				t.Fatal("finalizer released without reporting failure")
			}
		})
	}
}

func TestLifecycleRejectsInvalidDeletionPolicies(t *testing.T) {
	for _, observe := range []bool{false, true} {
		obj := newConfig(DeletionPolicyDelete, 0)
		policy := DriftPolicyObserve
		if observe {
			obj.Spec.Reconcile.DriftPolicy = &policy
		}
		c := testClient(obj).WithStatusSubresource(obj).Build()
		var remove func(context.Context) error
		if observe {
			remove = func(context.Context) error { t.Fatal("normal reconcile must not clean up"); return nil }
		}
		done, after := HandleLifecycle(context.Background(), c, nil, obj, remove)
		if !done || after <= 0 {
			t.Fatalf("invalid policy was not stopped: done=%v after=%s", done, after)
		}
		condition := meta.FindStatusCondition(obj.Status.Conditions, "Synced")
		if condition == nil || condition.Reason != "InvalidConfig" {
			t.Fatalf("missing invalid config condition: %+v", obj.Status.Conditions)
		}
	}
}

func TestCleanupAppWithoutOwnedPrunableResourcesNeedsNoCredentials(t *testing.T) {
	obj := newConfig(DeletionPolicyDelete, 0)
	obj.Status.ManagedResources = map[string][]string{"tags": {"shared"}}
	def := engine.AppDefinition{Resources: []engine.ResourceEndpoint{{Name: "tags"}, {Name: "indexers", Prunable: true}}}
	if err := CleanupApp(context.Background(), nil, nil, obj, commonv1alpha1.AppConnection{}, def, "sonarr"); err != nil {
		t.Fatal(err)
	}
}
