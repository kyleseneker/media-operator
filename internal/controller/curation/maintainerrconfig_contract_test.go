package curation

import (
	"testing"

	"github.com/stretchr/testify/assert"

	curationv1alpha1 "github.com/kyleseneker/media-operator/api/curation/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/controller/internal/contract"
)

// Maintainerr answers 2xx for settings keys it does not use, so a spec field the
// builder never maps is silently inert.
func TestMaintainerrSettingsFieldsAllReachThePayload(t *testing.T) {
	contract.AssertEveryFieldEmitted(t, &curationv1alpha1.MaintainerrSettings{},
		func(s *curationv1alpha1.MaintainerrSettings) map[string]any {
			return buildMaintainerrSettings(s)
		})
}

// An empty spec must produce no payload, so the reconcile skips the write entirely
// rather than clearing settings the CR does not manage.
func TestMaintainerrEmptySettingsProduceNoPayload(t *testing.T) {
	assert.Empty(t, buildMaintainerrSettings(&curationv1alpha1.MaintainerrSettings{}), "empty settings produced a payload")
}
