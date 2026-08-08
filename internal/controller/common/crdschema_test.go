package common

import (
	"os"
	"path/filepath"
	"testing"

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
	paths, err := filepath.Glob("../../../config/crd/bases/*.yaml")
	if err != nil {
		t.Fatalf("globbing CRDs: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("no CRDs found; run `make manifests` first")
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		var crd crdFile
		if err := yaml.Unmarshal(data, &crd); err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}

		for _, v := range crd.Spec.Versions {
			got := v.Schema.OpenAPIV3Schema.Required
			if len(got) != 1 || got[0] != "spec" {
				t.Errorf("%s/%s: root required = %v, want [spec]; "+
					"add explicit +optional / +required markers to the %s fields",
					crd.Spec.Names.Kind, v.Name, got, crd.Spec.Names.Kind)
			}
		}
	}
}
