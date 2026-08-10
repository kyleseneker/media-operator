package downloads

import (
	"testing"

	downloadsv1alpha1 "github.com/kyleseneker/media-operator/api/downloads/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/controller/internal/contract"
)

func TestSabnzbdGeneralValuesCoversEveryField(t *testing.T) {
	contract.AssertEveryFieldEmitted(t, &downloadsv1alpha1.SabnzbdGeneral{}, buildSabnzbdGeneralValues)
}

func TestSabnzbdFolderValuesCoversEveryField(t *testing.T) {
	contract.AssertEveryFieldEmitted(t, &downloadsv1alpha1.SabnzbdFolders{}, buildSabnzbdFolderValues)
}
