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
		if existing, ok := merged[k]; ok {
			merged[k] = mergeValue(existing, v)
			continue
		}
		merged[k] = v
	}

	// Check if anything changed
	changed := !reflect.DeepEqual(current, merged)

	return merged, changed, nil
}

// mergeValue recurses into nested objects and into arrays of named objects,
// so a partial "fields" array does not discard entries the CR omitted.
func mergeValue(current, desired interface{}) interface{} {
	curMap, curIsMap := current.(map[string]interface{})
	desMap, desIsMap := desired.(map[string]interface{})
	if curIsMap && desIsMap {
		out := deepCopyMap(curMap)
		for k, v := range desMap {
			if existing, ok := out[k]; ok {
				out[k] = mergeValue(existing, v)
				continue
			}
			out[k] = v
		}
		return out
	}

	curSlice, curIsSlice := current.([]interface{})
	desSlice, desIsSlice := desired.([]interface{})
	if curIsSlice && desIsSlice && isNamedObjectSlice(curSlice) && isNamedObjectSlice(desSlice) {
		return mergeNamedObjectSlice(curSlice, desSlice)
	}

	return desired
}

func isNamedObjectSlice(s []interface{}) bool {
	if len(s) == 0 {
		return false
	}
	for _, e := range s {
		m, ok := e.(map[string]interface{})
		if !ok {
			return false
		}
		if _, ok := m["name"].(string); !ok {
			return false
		}
	}
	return true
}

func mergeNamedObjectSlice(current, desired []interface{}) []interface{} {
	out := deepCopySlice(current)
	index := make(map[string]int, len(out))
	for i, e := range out {
		if m, ok := e.(map[string]interface{}); ok {
			if name, ok := m["name"].(string); ok {
				index[name] = i
			}
		}
	}

	for _, e := range desired {
		m, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		if i, found := index[name]; found {
			out[i] = mergeValue(out[i], m)
			continue
		}
		out = append(out, e)
	}

	return out
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
