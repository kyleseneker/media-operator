package reconciler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSetCondition(t *testing.T) {
	var conditions []metav1.Condition
	SetCondition(&conditions, 1, "Synced", "True", "Synced", "all good")

	require.Len(t, conditions, 1)
	assert.Equal(t, "Synced", conditions[0].Type)
	assert.Equal(t, metav1.ConditionStatus("True"), conditions[0].Status)
	assert.Equal(t, "Synced", conditions[0].Reason)
	assert.Equal(t, "all good", conditions[0].Message)
	assert.Equal(t, int64(1), conditions[0].ObservedGeneration)
}

func TestSetCondition_UpdateExisting(t *testing.T) {
	conditions := []metav1.Condition{
		{
			Type:               "Synced",
			Status:             metav1.ConditionTrue,
			Reason:             "Synced",
			Message:            "old",
			ObservedGeneration: 1,
			LastTransitionTime: metav1.Now(),
		},
	}

	SetCondition(&conditions, 2, "Synced", "False", "SyncFailed", "something broke")

	require.Len(t, conditions, 1)
	assert.Equal(t, metav1.ConditionStatus("False"), conditions[0].Status)
	assert.Equal(t, "SyncFailed", conditions[0].Reason)
	assert.Equal(t, "something broke", conditions[0].Message)
	assert.Equal(t, int64(2), conditions[0].ObservedGeneration)
}
