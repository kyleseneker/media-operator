package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

// TestMetricsRegistered verifies that the init() function successfully registered
// all four custom metrics without panicking (duplicate registration would panic).
// Simply importing the package runs init(); if the test binary linked, init ran.
func TestMetricsRegistered(t *testing.T) {
	assert.NotNil(t, AppAPIRequestDuration)
	assert.NotNil(t, AppAPIErrorsTotal)
	assert.NotNil(t, ResourcesPrunedTotal)
	assert.NotNil(t, ManagedResources)
}

func TestAppAPIRequestDuration_Observe(t *testing.T) {
	// Observing with 3 labels must not panic — confirms label cardinality matches the spec.
	AppAPIRequestDuration.WithLabelValues("test-app", "GET", "success").Observe(0.1)
}

func TestAppAPIErrorsTotal_Inc(t *testing.T) {
	tests := []struct {
		name        string
		app         string
		statusClass string
	}{
		{"4xx error", "test-app", "4xx"},
		{"5xx error", "test-app", "5xx"},
		{"network error", "test-app", "network"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := testutil.ToFloat64(AppAPIErrorsTotal.WithLabelValues(tt.app, tt.statusClass))
			AppAPIErrorsTotal.WithLabelValues(tt.app, tt.statusClass).Inc()
			after := testutil.ToFloat64(AppAPIErrorsTotal.WithLabelValues(tt.app, tt.statusClass))
			assert.Equal(t, before+1, after)
		})
	}
}

func TestResourcesPrunedTotal_Inc(t *testing.T) {
	before := testutil.ToFloat64(ResourcesPrunedTotal.WithLabelValues("test-app", "downloadClients"))
	ResourcesPrunedTotal.WithLabelValues("test-app", "downloadClients").Inc()
	ResourcesPrunedTotal.WithLabelValues("test-app", "downloadClients").Inc()
	after := testutil.ToFloat64(ResourcesPrunedTotal.WithLabelValues("test-app", "downloadClients"))
	assert.Equal(t, before+2, after)
}

func TestManagedResources_Set(t *testing.T) {
	ManagedResources.WithLabelValues("test-app", "tags").Set(5)
	assert.Equal(t, float64(5), testutil.ToFloat64(ManagedResources.WithLabelValues("test-app", "tags")))

	// Overwrites, not adds
	ManagedResources.WithLabelValues("test-app", "tags").Set(3)
	assert.Equal(t, float64(3), testutil.ToFloat64(ManagedResources.WithLabelValues("test-app", "tags")))
}
