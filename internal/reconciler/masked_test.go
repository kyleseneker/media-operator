package reconciler

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsMasked(t *testing.T) {
	assert.True(t, IsMasked("********"))
	assert.True(t, IsMasked("****"))
	assert.False(t, IsMasked("***"))
	assert.False(t, IsMasked(""))
	assert.False(t, IsMasked("hunter2"))
	assert.False(t, IsMasked("abc****"))
	assert.False(t, IsMasked(nil))
	assert.False(t, IsMasked(8))
}

// The apps return "********" for secret fields on read, so comparing the real
// value against what comes back always reports drift and the write never
// settles. A masked field is unverifiable, not drifted.
func TestMergeDesiredMaskedFieldIsNotDrift(t *testing.T) {
	current := map[string]any{
		"name": "Jellyfin",
		"fields": []any{
			map[string]any{"name": "host", "value": "jellyfin.svc"},
			map[string]any{"name": "apiKey", "value": "********"},
		},
	}
	desired := map[string]any{
		"name": "Jellyfin",
		"fields": []any{
			map[string]any{"name": "host", "value": "jellyfin.svc"},
			map[string]any{"name": "apiKey", "value": "real-api-key"},
		},
	}

	out, err := MergeDesired(current, desired)
	require.NoError(t, err)
	assert.False(t, out.Changed, "a masked field must not read as drift")
	assert.NotEmpty(t, out.SecretDigest, "the desired secret must be digested")

	// The body still has to carry the real secret, so a write for any other
	// reason sets the right value rather than storing the mask.
	fields := out.Merged["fields"].([]any)
	apiKey := fields[1].(map[string]any)
	assert.Equal(t, "real-api-key", apiKey["value"])
}

func TestMergeDesiredRealDriftStillDetectedAlongsideMask(t *testing.T) {
	current := map[string]any{
		"name": "qBittorrent",
		"fields": []any{
			map[string]any{"name": "password", "value": "********"},
			map[string]any{"name": "tvCategory", "value": "wrong"},
		},
	}
	desired := map[string]any{
		"name": "qBittorrent",
		"fields": []any{
			map[string]any{"name": "password", "value": "pw"},
			map[string]any{"name": "tvCategory", "value": "tv"},
		},
	}

	out, err := MergeDesired(current, desired)
	require.NoError(t, err)
	assert.True(t, out.Changed, "a real field change must still be drift")
}

func TestMergeDesiredNoMaskedFieldsHasNoDigest(t *testing.T) {
	out, err := MergeDesired(
		map[string]any{"a": "1"},
		map[string]any{"a": "1"},
	)
	require.NoError(t, err)
	assert.False(t, out.Changed)
	assert.Empty(t, out.SecretDigest)
}

// A rotated secret is invisible in the fetched state, so the only signal is a
// digest that differs from the one last written.
func TestSecretsChangedSinceTracksRotation(t *testing.T) {
	const key = "sonarr|notifications|Jellyfin"

	first := digestOf([]string{"key-v1"})
	require.NotEmpty(t, first)

	assert.True(t, SecretsChangedSince(key, first), "never written, so it must write")
	RecordSecrets(key, first)
	assert.False(t, SecretsChangedSince(key, first), "unchanged secret must stay quiet")

	rotated := digestOf([]string{"key-v2"})
	assert.True(t, SecretsChangedSince(key, rotated), "rotated secret must write")
	RecordSecrets(key, rotated)
	assert.False(t, SecretsChangedSince(key, rotated))
}

func TestSecretsChangedSinceIgnoresEmptyDigest(t *testing.T) {
	assert.False(t, SecretsChangedSince("app|type|name", ""))
}

func TestDigestOfIsOrderIndependent(t *testing.T) {
	assert.Equal(t, digestOf([]string{"a", "b"}), digestOf([]string{"b", "a"}))
	assert.NotEqual(t, digestOf([]string{"a"}), digestOf([]string{"b"}))
	assert.Empty(t, digestOf(nil))
}
