package engine

import "github.com/kyleseneker/media-operator/internal/metrics"

// Writes gates the writes an integration sends through its own client on the
// drift policy. Enforce mode sends each write and counts it in
// drift_corrected_total; observe mode sends nothing and records the difference
// in drift_observed_total instead. The zero value enforces.
type Writes struct {
	// App is the metrics label for the target app.
	App string
	// Observe records differences instead of writing them.
	Observe bool

	drift []string
}

// Apply sends write, or in observe mode records the difference it would have
// corrected.
func (w *Writes) Apply(kind, name string, write func() error) error {
	if w.Observe {
		w.RecordDrift(kind, name)
		return nil
	}
	metrics.DriftCorrectedTotal.WithLabelValues(w.App, kind, name).Inc()
	return write()
}

// RecordDrift records a difference observe mode found without a write to
// withhold, such as one that depends on a resource observe did not create.
func (w *Writes) RecordDrift(kind, name string) {
	metrics.DriftObservedTotal.WithLabelValues(w.App, kind, name).Inc()
	w.drift = append(w.drift, kind+"/"+name)
}

// Drift lists the differences observe mode recorded, as kind/name.
func (w *Writes) Drift() []string {
	return w.drift
}
