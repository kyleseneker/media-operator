package common

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
)

// ConfigResource is the common interface implemented by all media-operator config CRDs.
// It provides access to the status fields that every config resource shares.
type ConfigResource interface {
	client.Object
	GetConditions() *[]metav1.Condition
	GetObservedGeneration() *int64
	GetLastSyncTime() **metav1.Time
	GetReconcileConfig() *commonv1alpha1.ReconcileConfig
}
