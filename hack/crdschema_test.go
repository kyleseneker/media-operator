package hack

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

// crdFile is the subset of a CRD manifest this test cares about.
type crdFile struct {
	Spec struct {
		Names struct {
			Kind string `json:"kind"`
		} `json:"names"`
		Versions []struct {
			Name   string `json:"name"`
			Schema struct {
				OpenAPIV3Schema struct {
					Required []string `json:"required"`
				} `json:"openAPIV3Schema"`
			} `json:"schema"`
		} `json:"versions"`
	} `json:"spec"`
}

// TestCRDRootRequiredIsSpecOnly guards a controller-gen footgun. controller-gen
// does not recognize Go's omitzero struct tag, so a root type whose Status field
// carries no explicit +optional marker gets status listed as required at the
// schema root. The API server then rejects every apply that omits a status
// block, which makes the CRD unusable while still generating and linting fine.
// Only spec belongs in the root required list.
func TestCRDRootRequiredIsSpecOnly(t *testing.T) {
	paths, err := filepath.Glob("../config/crd/bases/*.yaml")
	require.NoError(t, err, "globbing CRDs")
	require.NotEmpty(t, paths, "no CRDs found; run `make manifests` first")

	for _, path := range paths {
		data, err := os.ReadFile(path)
		require.NoError(t, err, "reading %s", path)
		var crd crdFile
		require.NoError(t, yaml.Unmarshal(data, &crd), "parsing %s", path)

		for _, v := range crd.Spec.Versions {
			got := v.Schema.OpenAPIV3Schema.Required
			assert.Equal(t, []string{"spec"}, got,
				"%s/%s: root required must be [spec]; add explicit +optional / +required markers to the %s fields",
				crd.Spec.Names.Kind, v.Name, crd.Spec.Names.Kind)
		}
	}
}
