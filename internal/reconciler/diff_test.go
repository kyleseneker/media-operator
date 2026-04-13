package reconciler

import (
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
