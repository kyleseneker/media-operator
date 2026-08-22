package reconciler

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// MergeOutcome is the result of overlaying desired state onto current state.
type MergeOutcome struct {
	// Merged is the body to send, carrying the real secret values.
	Merged map[string]any
	// Changed reports whether anything the app can actually show back differs.
	Changed bool
	// SecretDigest hashes the desired values that landed on fields the app
	// masks on read. Empty when the resource has no such fields.
	SecretDigest string
}

// MergeDesiredOverCurrent takes the current state from the API and overlays
// the desired state from the CR spec. Fields not set in desired are preserved
// from current. Returns the merged result and whether any changes were detected.
func MergeDesiredOverCurrent(current map[string]any, desired any) (map[string]any, bool, error) {
	out, err := MergeDesired(current, desired)
	return out.Merged, out.Changed, err
}

// MergeDesired is MergeDesiredOverCurrent with the masked-field digest the
// caller needs to notice a rotated secret.
func MergeDesired(current map[string]any, desired any) (MergeOutcome, error) {
	// Marshal desired to JSON, then unmarshal to map to get only non-nil fields
	desiredJSON, err := json.Marshal(desired)
	if err != nil {
		return MergeOutcome{}, fmt.Errorf("marshaling desired state: %w", err)
	}

	var desiredMap map[string]any
	if err := json.Unmarshal(desiredJSON, &desiredMap); err != nil {
		return MergeOutcome{}, fmt.Errorf("unmarshaling desired state: %w", err)
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

	// A field the app masks on read cannot be compared, so it is not drift.
	// Collect what was overlaid onto those fields instead: a change there is
	// only visible by remembering what was last written.
	var secrets []string
	changed := differs(current, merged, &secrets)

	return MergeOutcome{Merged: merged, Changed: changed, SecretDigest: digestOf(secrets)}, nil
}

// differs walks current against merged and reports whether they differ,
// treating a masked current value as equal to whatever replaced it. Desired
// values landing on masked fields are appended to secrets.
func differs(current, merged any, secrets *[]string) bool {
	if IsMasked(current) {
		if s, ok := merged.(string); ok && !IsMasked(merged) {
			*secrets = append(*secrets, s)
		}
		return false
	}

	curMap, curIsMap := current.(map[string]any)
	mergedMap, mergedIsMap := merged.(map[string]any)
	if curIsMap && mergedIsMap {
		if len(curMap) != len(mergedMap) {
			return true
		}
		for k, mv := range mergedMap {
			cv, ok := curMap[k]
			if !ok {
				return true
			}
			if differs(cv, mv, secrets) {
				return true
			}
		}
		return false
	}

	curSlice, curIsSlice := current.([]any)
	mergedSlice, mergedIsSlice := merged.([]any)
	if curIsSlice && mergedIsSlice {
		// mergeNamedObjectSlice preserves the order of current and appends
		// anything new, so equal lengths mean index-wise correspondence.
		if len(curSlice) != len(mergedSlice) {
			return true
		}
		for i := range curSlice {
			if differs(curSlice[i], mergedSlice[i], secrets) {
				return true
			}
		}
		return false
	}

	return !reflect.DeepEqual(current, merged)
}

// mergeValue recurses into nested objects and into arrays of named objects,
// so a partial "fields" array does not discard entries the CR omitted.
func mergeValue(current, desired any) any {
	curMap, curIsMap := current.(map[string]any)
	desMap, desIsMap := desired.(map[string]any)
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

	curSlice, curIsSlice := current.([]any)
	desSlice, desIsSlice := desired.([]any)
	if curIsSlice && desIsSlice && isNamedObjectSlice(curSlice) && isNamedObjectSlice(desSlice) {
		return mergeNamedObjectSlice(curSlice, desSlice)
	}

	return desired
}

func isNamedObjectSlice(s []any) bool {
	if len(s) == 0 {
		return false
	}
	for _, e := range s {
		m, ok := e.(map[string]any)
		if !ok {
			return false
		}
		if _, ok := m["name"].(string); !ok {
			return false
		}
	}
	return true
}

func mergeNamedObjectSlice(current, desired []any) []any {
	out := deepCopySlice(current)
	index := make(map[string]int, len(out))
	for i, e := range out {
		if m, ok := e.(map[string]any); ok {
			if name, ok := m["name"].(string); ok {
				index[name] = i
			}
		}
	}

	for _, e := range desired {
		m, ok := e.(map[string]any)
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

func deepCopyMap(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case map[string]any:
			result[k] = deepCopyMap(val)
		case []any:
			result[k] = deepCopySlice(val)
		default:
			result[k] = v
		}
	}
	return result
}

func deepCopySlice(s []any) []any {
	result := make([]any, len(s))
	for i, v := range s {
		switch val := v.(type) {
		case map[string]any:
			result[i] = deepCopyMap(val)
		case []any:
			result[i] = deepCopySlice(val)
		default:
			result[i] = v
		}
	}
	return result
}
