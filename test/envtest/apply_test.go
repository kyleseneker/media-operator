// Package envtest runs the sample manifests against a real kube-apiserver.
//
// Schema mistakes in the CRDs — a field the API server rejects, or a root
// required list that makes every apply fail — are invisible to unit tests,
// which never talk to an API server. This applies every committed sample and
// example so those failures surface here instead of in someone's cluster.
//
// Skipped unless KUBEBUILDER_ASSETS is set, so `make test` stays hermetic.
// Run it with `make test-envtest`.
package envtest

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/yaml"
)

const group = "media-operator.dev/v1alpha1"

func TestSamplesApply(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("KUBEBUILDER_ASSETS unset; run via `make test-envtest`")
	}

	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := env.Start()
	if err != nil {
		t.Fatalf("starting control plane: %v", err)
	}
	t.Cleanup(func() { _ = env.Stop() })

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("building scheme: %v", err)
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("building client: %v", err)
	}

	ctx := context.Background()

	dirs := []string{"config/samples", "examples"}
	patterns := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		patterns = append(patterns, filepath.Join("..", "..", dir, "*.yaml"))
	}

	applied := 0
	for _, pattern := range patterns {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("globbing %s: %v", pattern, err)
		}
		for _, path := range paths {
			// Each file gets its own namespace. Samples reuse names across files
			// and span several namespaces; isolating them keeps a collision from
			// masking a genuine schema rejection.
			ns := namespaceFor(path)
			if err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil {
				t.Fatalf("creating namespace %s: %v", ns, err)
			}
			for _, obj := range decode(t, path) {
				if obj.GetAPIVersion() != group {
					continue
				}
				obj.SetNamespace(ns)
				name := obj.GetKind() + "/" + obj.GetName()
				if err := c.Create(ctx, obj); err != nil {
					t.Errorf("%s: applying %s: %v", filepath.Base(path), name, err)
					continue
				}
				applied++
			}
		}
	}
	t.Logf("applied %d resources", applied)
	if applied == 0 {
		t.Fatal("no resources applied; sample discovery is broken")
	}
}

// namespaceFor derives a DNS-1123 namespace name from a manifest path.
func namespaceFor(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), ".yaml")
	base = strings.ToLower(strings.ReplaceAll(base, "_", "-"))
	if parent := filepath.Base(filepath.Dir(path)); parent != "" {
		base = parent + "-" + base
	}
	if len(base) > 63 {
		base = base[:63]
	}
	return strings.Trim(base, "-")
}

func decode(t *testing.T, path string) []*unstructured.Unstructured {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var out []*unstructured.Unstructured
	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
	for {
		doc, err := reader.Read()
		if err != nil {
			break
		}
		if len(strings.TrimSpace(string(doc))) == 0 {
			continue
		}
		obj := &unstructured.Unstructured{}
		if err := yaml.Unmarshal(doc, obj); err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		if obj.GetKind() == "" {
			continue
		}
		out = append(out, obj)
	}
	return out
}
