package mediaservers

import (
	"testing"

	mediaserversv1alpha1 "github.com/kyleseneker/media-operator/api/mediaservers/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/controller/internal/contract"
)

// Plex accepts a preferences PUT for keys it does not recognise and answers 200,
// so a spec field the builder never maps is silently inert.
func TestPlexServerFieldsAllMapToPreferences(t *testing.T) {
	contract.AssertEveryFieldEmitted(t, &mediaserversv1alpha1.PlexServer{},
		func(s *mediaserversv1alpha1.PlexServer) map[string]string {
			return buildPlexPreferences(mediaserversv1alpha1.PlexConfigSpec{Server: s})
		})
}

func TestPlexTranscoderFieldsAllMapToPreferences(t *testing.T) {
	contract.AssertEveryFieldEmitted(t, &mediaserversv1alpha1.PlexTranscoder{},
		func(s *mediaserversv1alpha1.PlexTranscoder) map[string]string {
			return buildPlexPreferences(mediaserversv1alpha1.PlexConfigSpec{Transcoder: s})
		})
}

func TestPlexNetworkFieldsAllMapToPreferences(t *testing.T) {
	contract.AssertEveryFieldEmitted(t, &mediaserversv1alpha1.PlexNetwork{},
		func(s *mediaserversv1alpha1.PlexNetwork) map[string]string {
			return buildPlexPreferences(mediaserversv1alpha1.PlexConfigSpec{Network: s})
		})
}

// An empty spec must not send preferences at all, so an unset section never
// overwrites settings the CR does not manage.
func TestPlexEmptySpecSendsNoPreferences(t *testing.T) {
	if got := buildPlexPreferences(mediaserversv1alpha1.PlexConfigSpec{}); len(got) != 0 {
		t.Errorf("empty spec produced preferences: %v", got)
	}
}
