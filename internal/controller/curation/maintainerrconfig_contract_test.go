package curation

import (
	"testing"

	"github.com/stretchr/testify/assert"

	curationv1alpha1 "github.com/kyleseneker/media-operator/api/curation/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/controller/internal/contract"
)

// Maintainerr strips settings keys it does not know without an error, so a spec
// field the builder never maps is silently inert.
func TestMaintainerrJobsFieldsAllReachThePayload(t *testing.T) {
	contract.AssertEveryFieldEmitted(t, &curationv1alpha1.MaintainerrJobs{}, buildMaintainerrJobs)
}

// An empty spec must produce no payload, so the reconcile skips the write entirely
// rather than clearing schedules the CR does not manage.
func TestMaintainerrEmptyJobsProduceNoPayload(t *testing.T) {
	assert.Empty(t, buildMaintainerrJobs(&curationv1alpha1.MaintainerrJobs{}), "empty jobs produced a payload")
}
