package reconciler

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SetCondition sets a condition on a list of conditions.
func SetCondition(conditions *[]metav1.Condition, generation int64, condType, status, reason, message string) {
	condition := metav1.Condition{
		Type:               condType,
		Status:             metav1.ConditionStatus(status),
		ObservedGeneration: generation,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
	meta.SetStatusCondition(conditions, condition)
}
