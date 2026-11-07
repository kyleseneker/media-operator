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

// Prune deletes what owned.Prune would, or in observe mode records each item it
// would delete as a difference.
func (w *Writes) Prune(owned *Ownership, kind string, declared, existing []string, remove func(name string) error) error {
	if !w.Observe {
		return owned.Prune(kind, declared, existing, remove)
	}
	candidates := owned.Candidates(kind, declared, existing)
	if err := pruneLimit(kind, len(candidates)); err != nil {
		return err
	}
	for _, name := range candidates {
		w.RecordDrift(kind, name)
	}
	return nil
}

// RecordDrift records a difference observe mode found without a write to
// withhold, such as one that depends on a resource observe did not create.
func (w *Writes) RecordDrift(kind, name string) {
	metrics.DriftObservedTotal.WithLabelValues(w.App, kind, name).Inc()
	w.drift = append(w.drift, kind+"("+name+")")
}

// Compare records a difference between current and desired, compared as
// ObserveDifference compares them.
func (w *Writes) Compare(kind, name string, current map[string]any, desired any) error {
	changed, err := ObserveDifference(w.App, kind, name, current, desired)
	if changed {
		w.drift = append(w.drift, kind+"("+name+")")
	}
	return err
}

// Drift lists the differences observe mode recorded, as kind(name).
func (w *Writes) Drift() []string {
	return w.drift
}
