package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReconcileResult_Success(t *testing.T) {
	tests := []struct {
		name   string
		result ReconcileResult
		want   bool
	}{
		{"no errors", ReconcileResult{Synced: []string{"a"}}, true},
		{"empty", ReconcileResult{}, true},
		{"with errors", ReconcileResult{Errors: []string{"fail"}}, false},
		{"synced and errors", ReconcileResult{Synced: []string{"a"}, Errors: []string{"b"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.result.Success())
		})
	}
}

func TestReconcileResult_Message(t *testing.T) {
	tests := []struct {
		name   string
		result ReconcileResult
		want   string
	}{
		{
			name:   "all synced",
			result: ReconcileResult{Synced: []string{"mediaManagement", "naming"}},
			want:   "synced: [mediaManagement naming]",
		},
		{
			name:   "nothing synced no errors",
			result: ReconcileResult{},
			want:   "all configuration sections synced",
		},
		{
			name:   "errors only",
			result: ReconcileResult{Errors: []string{"naming: failed"}},
			want:   "errors: [naming: failed]",
		},
		{
			name:   "synced and errors",
			result: ReconcileResult{Synced: []string{"ui"}, Errors: []string{"naming: fail"}},
			want:   "synced: [ui]; errors: [naming: fail]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.result.Message())
		})
	}
}

func TestIsNilInterface(t *testing.T) {
	tests := []struct {
		name string
		val  interface{}
		want bool
	}{
		{"nil", nil, true},
		{"typed nil pointer", (*string)(nil), true},
		{"non-nil pointer", ptrTo("hello"), false},
		{"non-pointer value", "hello", false},
		{"integer", 42, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isNilInterface(tt.val))
		})
	}
}

func ptrTo[T any](v T) *T { return &v }
