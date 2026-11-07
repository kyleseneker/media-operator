package reconciler

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testModified = "modified"

func TestMergeDesiredOverCurrent(t *testing.T) {
	tests := []struct {
		name        string
		current     map[string]any
		desired     any
		wantChanged bool
		wantKeys    map[string]any
	}{
		{
			name:        "no changes",
			current:     map[string]any{"a": "1", "b": "2"},
			desired:     map[string]any{"a": "1", "b": "2"},
			wantChanged: false,
			wantKeys:    map[string]any{"a": "1", "b": "2"},
		},
		{
			name:        "override existing field",
			current:     map[string]any{"a": "1", "b": "2"},
			desired:     map[string]any{"b": "changed"},
			wantChanged: true,
			wantKeys:    map[string]any{"a": "1", "b": "changed"},
		},
		{
			name:        "add new field",
			current:     map[string]any{"a": "1"},
			desired:     map[string]any{"b": "new"},
			wantChanged: true,
			wantKeys:    map[string]any{"a": "1", "b": "new"},
		},
		{
			name:        "preserve unset fields",
			current:     map[string]any{"a": "1", "b": "2", "c": "3"},
			desired:     map[string]any{"a": "updated"},
			wantChanged: true,
			wantKeys:    map[string]any{"a": "updated", "b": "2", "c": "3"},
		},
		{
			name:        "desired as struct",
			current:     map[string]any{"name": "old"},
			desired:     struct{ Name string }{Name: "new"},
			wantChanged: true,
			wantKeys:    map[string]any{"Name": "new", "name": "old"},
		},
		{
			name:        "empty desired no change",
			current:     map[string]any{"a": "1"},
			desired:     map[string]any{},
			wantChanged: false,
			wantKeys:    map[string]any{"a": "1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged, changed, err := MergeDesiredOverCurrent(tt.current, tt.desired)
			require.NoError(t, err)
			assert.Equal(t, tt.wantChanged, changed)
			for k, v := range tt.wantKeys {
				assert.Equal(t, v, merged[k], "key %q", k)
			}
		})
	}
}

func TestMergeDesiredOverCurrent_Error(t *testing.T) {
	// Channels can't be marshaled to JSON
	_, _, err := MergeDesiredOverCurrent(map[string]any{}, make(chan int))
	assert.Error(t, err)
}

func TestDeepCopyMap(t *testing.T) {
	original := map[string]any{
		"nested": map[string]any{"key": "value"},
		"slice":  []any{"a", "b"},
		"scalar": 42,
	}
	copied := deepCopyMap(original)

	// Modify the copy
	copied["nested"].(map[string]any)["key"] = testModified
	copied["slice"].([]any)[0] = testModified
	copied["scalar"] = 99

	// Original should be unchanged
	assert.Equal(t, "value", original["nested"].(map[string]any)["key"])
	assert.Equal(t, "a", original["slice"].([]any)[0])
	assert.Equal(t, 42, original["scalar"])
}

func TestDeepCopySlice(t *testing.T) {
	original := []any{
		map[string]any{"key": "value"},
		[]any{1, 2},
		"scalar",
	}
	copied := deepCopySlice(original)

	// Modify the copy
	copied[0].(map[string]any)["key"] = testModified
	copied[1].([]any)[0] = 99

	// Original should be unchanged
	assert.Equal(t, "value", original[0].(map[string]any)["key"])
	assert.Equal(t, 1, original[1].([]any)[0])
}

func TestMergePreservesUnmanagedFieldEntries(t *testing.T) {
	currentJSON := `{
	  "id": 3, "name": "TorrentLeech", "implementation": "TorrentleechAPI",
	  "fields": [
	    {"name":"baseUrl","value":"https://x.org","order":0,"type":"textbox"},
	    {"name":"minimumSeeders","value":5,"order":1,"type":"number"},
	    {"name":"seedCriteria.seedRatio","value":2.0,"order":2,"type":"number"},
	    {"name":"additionalParameters","value":"&freeleech=1","order":3,"type":"textbox"}
	  ]}`
	var current map[string]any
	require.NoError(t, json.Unmarshal([]byte(currentJSON), &current))

	desired := map[string]any{
		"name": "TorrentLeech",
		"fields": []any{
			map[string]any{"name": "baseUrl", "value": "https://x.org"},
		},
	}

	merged, changed, err := MergeDesiredOverCurrent(current, desired)
	require.NoError(t, err)

	fields, ok := merged["fields"].([]any)
	require.True(t, ok, "fields missing from merged output")
	require.Len(t, fields, 4, "expected 4 field entries preserved")
	for _, f := range fields {
		m := f.(map[string]any)
		if m["name"] == "minimumSeeders" {
			assert.Equal(t, float64(5), m["value"], "minimumSeeders was clobbered")
		}
		if m["name"] == "baseUrl" {
			assert.Equal(t, float64(0), m["order"], "baseUrl lost its API metadata: %v", m)
		}
	}
	assert.False(t, changed, "declaring an already-matching value reported changed=true; this re-PUTs forever")
}

func TestMergeAppliesFieldValueChange(t *testing.T) {
	current := map[string]any{
		"name": "TL",
		"fields": []any{
			map[string]any{"name": "minimumSeeders", "value": float64(5), "order": float64(1)},
		},
	}
	desired := map[string]any{
		"fields": []any{
			map[string]any{"name": "minimumSeeders", "value": float64(9)},
		},
	}
	merged, changed, err := MergeDesiredOverCurrent(current, desired)
	require.NoError(t, err)
	require.True(t, changed, "a real value change must report changed=true")
	f := merged["fields"].([]any)[0].(map[string]any)
	assert.Equal(t, float64(9), f["value"], "value not applied")
	assert.Equal(t, float64(1), f["order"], "metadata lost while applying a change: %v", f)
}

func TestMergeAppendsNewFieldEntry(t *testing.T) {
	current := map[string]any{
		"fields": []any{
			map[string]any{"name": "baseUrl", "value": "https://x"},
		},
	}
	desired := map[string]any{
		"fields": []any{
			map[string]any{"name": "apiKey", "value": "abc"},
		},
	}
	merged, _, err := MergeDesiredOverCurrent(current, desired)
	require.NoError(t, err)
	assert.Len(t, merged["fields"].([]any), 2, "expected existing + appended = 2 entries")
}

// Prowlarr field values are strings in the CRD; the app reads them back typed.
func TestStringMatchingTheAppsTypedValueIsNotDrift(t *testing.T) {
	current := map[string]any{"fields": []any{
		map[string]any{"name": "port", "value": float64(8080)},
		map[string]any{"name": "useSsl", "value": false},
	}}
	desired := map[string]any{"fields": []any{
		map[string]any{"name": "port", "value": "8080"},
		map[string]any{"name": "useSsl", "value": "false"},
	}}
	_, changed, err := MergeDesiredOverCurrent(current, desired)
	require.NoError(t, err)
	assert.False(t, changed, "a quoted 8080 is the app's 8080; comparing types rewrites it on every pass")

	desired["fields"].([]any)[0].(map[string]any)["value"] = "9090"
	_, changed, _ = MergeDesiredOverCurrent(current, desired)
	assert.True(t, changed, "a different value must still be drift")
}
