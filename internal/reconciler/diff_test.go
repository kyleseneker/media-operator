package reconciler

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeDesiredOverCurrent(t *testing.T) {
	tests := []struct {
		name        string
		current     map[string]interface{}
		desired     interface{}
		wantChanged bool
		wantKeys    map[string]interface{}
	}{
		{
			name:        "no changes",
			current:     map[string]interface{}{"a": "1", "b": "2"},
			desired:     map[string]interface{}{"a": "1", "b": "2"},
			wantChanged: false,
			wantKeys:    map[string]interface{}{"a": "1", "b": "2"},
		},
		{
			name:        "override existing field",
			current:     map[string]interface{}{"a": "1", "b": "2"},
			desired:     map[string]interface{}{"b": "changed"},
			wantChanged: true,
			wantKeys:    map[string]interface{}{"a": "1", "b": "changed"},
		},
		{
			name:        "add new field",
			current:     map[string]interface{}{"a": "1"},
			desired:     map[string]interface{}{"b": "new"},
			wantChanged: true,
			wantKeys:    map[string]interface{}{"a": "1", "b": "new"},
		},
		{
			name:        "preserve unset fields",
			current:     map[string]interface{}{"a": "1", "b": "2", "c": "3"},
			desired:     map[string]interface{}{"a": "updated"},
			wantChanged: true,
			wantKeys:    map[string]interface{}{"a": "updated", "b": "2", "c": "3"},
		},
		{
			name:        "desired as struct",
			current:     map[string]interface{}{"name": "old"},
			desired:     struct{ Name string }{Name: "new"},
			wantChanged: true,
			wantKeys:    map[string]interface{}{"Name": "new", "name": "old"},
		},
		{
			name:        "empty desired no change",
			current:     map[string]interface{}{"a": "1"},
			desired:     map[string]interface{}{},
			wantChanged: false,
			wantKeys:    map[string]interface{}{"a": "1"},
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
	_, _, err := MergeDesiredOverCurrent(map[string]interface{}{}, make(chan int))
	assert.Error(t, err)
}

func TestDeepCopyMap(t *testing.T) {
	original := map[string]interface{}{
		"nested": map[string]interface{}{"key": "value"},
		"slice":  []interface{}{"a", "b"},
		"scalar": 42,
	}
	copied := deepCopyMap(original)

	// Modify the copy
	copied["nested"].(map[string]interface{})["key"] = "modified"
	copied["slice"].([]interface{})[0] = "modified"
	copied["scalar"] = 99

	// Original should be unchanged
	assert.Equal(t, "value", original["nested"].(map[string]interface{})["key"])
	assert.Equal(t, "a", original["slice"].([]interface{})[0])
	assert.Equal(t, 42, original["scalar"])
}

func TestDeepCopySlice(t *testing.T) {
	original := []interface{}{
		map[string]interface{}{"key": "value"},
		[]interface{}{1, 2},
		"scalar",
	}
	copied := deepCopySlice(original)

	// Modify the copy
	copied[0].(map[string]interface{})["key"] = "modified"
	copied[1].([]interface{})[0] = 99

	// Original should be unchanged
	assert.Equal(t, "value", original[0].(map[string]interface{})["key"])
	assert.Equal(t, 1, original[1].([]interface{})[0])
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
	var current map[string]interface{}
	if err := json.Unmarshal([]byte(currentJSON), &current); err != nil {
		t.Fatal(err)
	}

	desired := map[string]interface{}{
		"name": "TorrentLeech",
		"fields": []interface{}{
			map[string]interface{}{"name": "baseUrl", "value": "https://x.org"},
		},
	}

	merged, changed, err := MergeDesiredOverCurrent(current, desired)
	if err != nil {
		t.Fatal(err)
	}

	fields, ok := merged["fields"].([]interface{})
	if !ok {
		t.Fatalf("fields missing from merged output")
	}
	if len(fields) != 4 {
		t.Fatalf("expected 4 field entries preserved, got %d", len(fields))
	}
	for _, f := range fields {
		m := f.(map[string]interface{})
		if m["name"] == "minimumSeeders" && m["value"] != float64(5) {
			t.Errorf("minimumSeeders was clobbered: %v", m["value"])
		}
		if m["name"] == "baseUrl" && m["order"] != float64(0) {
			t.Errorf("baseUrl lost its API metadata: %v", m)
		}
	}
	if changed {
		t.Errorf("declaring an already-matching value reported changed=true; this re-PUTs forever")
	}
}

func TestMergeAppliesFieldValueChange(t *testing.T) {
	current := map[string]interface{}{
		"name": "TL",
		"fields": []interface{}{
			map[string]interface{}{"name": "minimumSeeders", "value": float64(5), "order": float64(1)},
		},
	}
	desired := map[string]interface{}{
		"fields": []interface{}{
			map[string]interface{}{"name": "minimumSeeders", "value": float64(9)},
		},
	}
	merged, changed, err := MergeDesiredOverCurrent(current, desired)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("a real value change must report changed=true")
	}
	f := merged["fields"].([]interface{})[0].(map[string]interface{})
	if f["value"] != float64(9) {
		t.Errorf("value not applied: %v", f["value"])
	}
	if f["order"] != float64(1) {
		t.Errorf("metadata lost while applying a change: %v", f)
	}
}

func TestMergeAppendsNewFieldEntry(t *testing.T) {
	current := map[string]interface{}{
		"fields": []interface{}{
			map[string]interface{}{"name": "baseUrl", "value": "https://x"},
		},
	}
	desired := map[string]interface{}{
		"fields": []interface{}{
			map[string]interface{}{"name": "apiKey", "value": "abc"},
		},
	}
	merged, _, err := MergeDesiredOverCurrent(current, desired)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(merged["fields"].([]interface{})); got != 2 {
		t.Errorf("expected existing + appended = 2 entries, got %d", got)
	}
}
