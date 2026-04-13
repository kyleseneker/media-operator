package reconciler

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// MergeDesiredOverCurrent takes the current state from the API and overlays
// the desired state from the CR spec. Fields not set in desired are preserved
// from current. Returns the merged result and whether any changes were detected.
func MergeDesiredOverCurrent(current map[string]interface{}, desired interface{}) (map[string]interface{}, bool, error) {
	// Marshal desired to JSON, then unmarshal to map to get only non-nil fields
	desiredJSON, err := json.Marshal(desired)
	if err != nil {
		return nil, false, fmt.Errorf("marshaling desired state: %w", err)
	}

	var desiredMap map[string]interface{}
	if err := json.Unmarshal(desiredJSON, &desiredMap); err != nil {
		return nil, false, fmt.Errorf("unmarshaling desired state: %w", err)
	}

	// Deep copy current
	merged := deepCopyMap(current)

	// Overlay desired fields onto current
	for k, v := range desiredMap {
		merged[k] = v
	}

	// Check if anything changed
	changed := !reflect.DeepEqual(current, merged)

	return merged, changed, nil
}

func deepCopyMap(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case map[string]interface{}:
			result[k] = deepCopyMap(val)
		case []interface{}:
			result[k] = deepCopySlice(val)
		default:
			result[k] = v
		}
	}
	return result
}

func deepCopySlice(s []interface{}) []interface{} {
	result := make([]interface{}, len(s))
	for i, v := range s {
		switch val := v.(type) {
		case map[string]interface{}:
			result[i] = deepCopyMap(val)
		case []interface{}:
			result[i] = deepCopySlice(val)
		default:
			result[i] = v
		}
	}
	return result
}
