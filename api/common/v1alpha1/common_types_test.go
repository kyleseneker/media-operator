package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFieldValue_MarshalJSON(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		want string
	}{
		{"nil raw returns null", nil, "null"},
		{"string value", []byte(`"hello"`), `"hello"`},
		{"number value", []byte(`42`), `42`},
		{"bool value", []byte(`true`), `true`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fv := FieldValue{Raw: tt.raw}
			data, err := fv.MarshalJSON()
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(data))
		})
	}
}

func TestFieldValue_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantNil bool
		wantRaw string
	}{
		{"null", "null", true, ""},
		{"string", `"hello"`, false, `"hello"`},
		{"number", "42", false, "42"},
		{"bool", "true", false, "true"},
		{"object", `{"key":"val"}`, false, `{"key":"val"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fv FieldValue
			err := fv.UnmarshalJSON([]byte(tt.input))
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, fv.Raw)
			} else {
				assert.Equal(t, tt.wantRaw, string(fv.Raw))
			}
		})
	}
}

func TestFieldValue_ToInterface(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		want interface{}
	}{
		{"nil returns nil", nil, nil},
		{"string", []byte(`"hello"`), "hello"},
		{"float64", []byte(`42`), float64(42)},
		{"bool", []byte(`true`), true},
		{"invalid json returns string", []byte(`not-json`), "not-json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fv := FieldValue{Raw: tt.raw}
			assert.Equal(t, tt.want, fv.ToInterface())
		})
	}
}

func TestFieldValue_RoundTrip(t *testing.T) {
	original := FieldValue{Raw: []byte(`"test-value"`)}
	data, err := json.Marshal(original)
	require.NoError(t, err)

	var restored FieldValue
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)

	assert.Equal(t, original.Raw, restored.Raw)
}
