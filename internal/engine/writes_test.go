package engine

import (
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kyleseneker/media-operator/internal/metrics"
)

func TestWritesApplyEnforceWritesAndCountsTheCorrection(t *testing.T) {
	corrected := metrics.DriftCorrectedTotal.WithLabelValues("writes-enforce", "settings", "main")
	before := testutil.ToFloat64(corrected)

	w := &Writes{App: "writes-enforce"}
	wrote := false
	require.NoError(t, w.Apply("settings", "main", func() error { wrote = true; return nil }))

	assert.True(t, wrote, "enforce mode must send the write")
	assert.Equal(t, before+1, testutil.ToFloat64(corrected), "drift_corrected_total")
	assert.Empty(t, w.Drift(), "enforce mode records no drift")
}

func TestWritesApplyEnforceReturnsTheWriteError(t *testing.T) {
	w := &Writes{}
	err := w.Apply("settings", "main", func() error { return errors.New("rejected") })
	require.EqualError(t, err, "rejected")
}

func TestWritesApplyObserveRecordsInsteadOfWriting(t *testing.T) {
	observed := metrics.DriftObservedTotal.WithLabelValues("writes-observe", "settings", "main")
	corrected := metrics.DriftCorrectedTotal.WithLabelValues("writes-observe", "settings", "main")
	beforeObserved, beforeCorrected := testutil.ToFloat64(observed), testutil.ToFloat64(corrected)

	w := &Writes{App: "writes-observe", Observe: true}
	require.NoError(t, w.Apply("settings", "main", func() error {
		require.Fail(t, "observe mode must not write")
		return nil
	}))

	assert.Equal(t, []string{"settings/main"}, w.Drift())
	assert.Equal(t, beforeObserved+1, testutil.ToFloat64(observed), "drift_observed_total")
	assert.Equal(t, beforeCorrected, testutil.ToFloat64(corrected), "drift_corrected_total moved in observe mode")
}

func TestWritesRecordDrift(t *testing.T) {
	observed := metrics.DriftObservedTotal.WithLabelValues("writes-record", "action", "a")
	before := testutil.ToFloat64(observed)

	w := &Writes{App: "writes-record", Observe: true}
	w.RecordDrift("action", "a")
	w.RecordDrift("action", "b")

	assert.Equal(t, []string{"action/a", "action/b"}, w.Drift())
	assert.Equal(t, before+1, testutil.ToFloat64(observed), "drift_observed_total for action/a")
}

func TestWritesPruneObserveRecordsWhatItWouldDelete(t *testing.T) {
	owned := NewOwnership("writes-prune", map[string][]string{"feeds": {"old", "kept"}})
	w := &Writes{App: "writes-prune", Observe: true}

	require.NoError(t, w.Prune(owned, "feeds", []string{"kept"}, []string{"old", "kept", "hand-made"}, func(string) error {
		require.Fail(t, "observe mode must not delete")
		return nil
	}))

	assert.Equal(t, []string{"feeds/old"}, w.Drift())
	assert.Equal(t, map[string][]string{"feeds": {"old", "kept"}}, owned.Managed(), "observe mode changed ownership")
}

func TestWritesPruneEnforceDeletes(t *testing.T) {
	owned := NewOwnership("writes-prune", map[string][]string{"feeds": {"old"}})
	var removed []string
	require.NoError(t, (&Writes{}).Prune(owned, "feeds", []string{"kept"}, []string{"old", "kept"}, func(name string) error {
		removed = append(removed, name)
		return nil
	}))
	assert.Equal(t, []string{"old"}, removed)
}
