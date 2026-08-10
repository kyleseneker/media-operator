package mediaservers

import (
	"reflect"
	"testing"

	mediaserversv1alpha1 "github.com/kyleseneker/media-operator/api/mediaservers/v1alpha1"
)

// buildPlexPreferences takes the whole spec, so cover the server section directly.
func TestPlexServerFieldsAllEmitted(t *testing.T) {
	spec := mediaserversv1alpha1.PlexConfigSpec{Server: &mediaserversv1alpha1.PlexServer{}}
	fill(spec.Server)
	v := numFields(spec.Server)
	prefs := buildPlexPreferences(spec)
	if len(prefs) != v {
		t.Errorf("PlexServer declares %d fields but only %d preferences were emitted; an unmapped field is silently inert in Plex", v, len(prefs))
		for k := range prefs {
			t.Logf("  emitted: %s", k)
		}
	}
}

func fill(s *mediaserversv1alpha1.PlexServer) {
	v := reflect.ValueOf(s).Elem()
	for i := range v.NumField() {
		f := v.Field(i)
		switch f.Kind() {
		case reflect.Pointer:
			f.Set(reflect.New(f.Type().Elem()))
		case reflect.String:
			f.SetString("x")
		}
	}
}

func numFields(s *mediaserversv1alpha1.PlexServer) int {
	return reflect.ValueOf(s).Elem().NumField()
}
