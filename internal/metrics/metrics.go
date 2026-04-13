// Package metrics defines custom Prometheus metrics for the media-operator.
// All metrics are auto-registered with the controller-runtime metrics registry
// via the init() function, so they are exposed on the same /metrics endpoint
// that controller-runtime already serves. No separate HTTP server is required.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

const namespace = "media_operator"

var (
	// AppAPIRequestDuration is a histogram of outbound HTTP call latencies to target apps.
	// Labels: app (sonarr/radarr/etc), method (GET/PUT/POST/DELETE), outcome (success/error).
	AppAPIRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "app_api_request_duration_seconds",
			Help:      "Duration of outbound HTTP requests to target apps.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"app", "method", "outcome"},
	)

	// AppAPIErrorsTotal counts non-2xx responses and network errors from target apps.
	// Labels: app, status_class (2xx/3xx/4xx/5xx/network).
	AppAPIErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "app_api_errors_total",
			Help:      "Total number of non-2xx responses and network errors from target apps.",
		},
		[]string{"app", "status_class"},
	)

	// ResourcesPrunedTotal counts resources deleted by the prune logic.
	// Pruning is destructive, so this is the most critical custom metric —
	// alert on `rate(media_operator_resources_pruned_total[5m]) > threshold`.
	// Labels: app, resource_type (downloadClients/indexers/qualityProfiles/etc).
	ResourcesPrunedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "resources_pruned_total",
			Help:      "Total number of unmanaged resources deleted by the prune logic.",
		},
		[]string{"app", "resource_type"},
	)

	// ManagedResources is a gauge of resources declared per CR, by type.
	// Updated on each successful reconcile.
	// Labels: app, resource_type.
	ManagedResources = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "managed_resources",
			Help:      "Number of resources declared in each CR, by type.",
		},
		[]string{"app", "resource_type"},
	)
)

func init() {
	ctrlmetrics.Registry.MustRegister(
		AppAPIRequestDuration,
		AppAPIErrorsTotal,
		ResourcesPrunedTotal,
		ManagedResources,
	)
}
