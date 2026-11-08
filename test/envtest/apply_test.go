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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
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
	require.NoError(t, err, "starting control plane")
	t.Cleanup(func() { _ = env.Stop() })

	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme), "building scheme")
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	require.NoError(t, err, "building client")

	ctx := context.Background()

	dirs := []string{"config/samples", "examples"}
	patterns := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		patterns = append(patterns, filepath.Join("..", "..", dir, "*.yaml"))
	}

	applied := 0
	for _, pattern := range patterns {
		paths, err := filepath.Glob(pattern)
		require.NoError(t, err, "globbing %s", pattern)
		for _, path := range paths {
			// Each file gets its own namespace. Samples reuse names across files
			// and span several namespaces; isolating them keeps a collision from
			// masking a genuine schema rejection.
			ns := namespaceFor(path)
			require.NoError(t, c.Create(ctx, &corev1.Namespace{Name: ns}), "creating namespace %s", ns)
			for _, obj := range decode(t, path) {
				if obj.GetAPIVersion() != group {
					continue
				}
				obj.SetNamespace(ns)
				name := obj.GetKind() + "/" + obj.GetName()
				if !assert.NoError(t, c.Create(ctx, obj), "%s: applying %s", filepath.Base(path), name) {
					continue
				}
				applied++
			}
		}
	}
	t.Logf("applied %d resources", applied)
	require.NotZero(t, applied, "no resources applied; sample discovery is broken")
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
	require.NoError(t, err, "reading %s", path)
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
		require.NoError(t, yaml.Unmarshal(doc, obj), "parsing %s", path)
		if obj.GetKind() == "" {
			continue
		}
		out = append(out, obj)
	}
	return out
}

func TestDelayProfilesAllowOneWithoutTags(t *testing.T) {
	c := startAPIServerWith(t)
	ctx := context.Background()
	sonarr := func(name, profiles string) *unstructured.Unstructured {
		obj := &unstructured.Unstructured{}
		require.NoError(t, yaml.Unmarshal([]byte(`
apiVersion: media-operator.dev/v1alpha1
kind: SonarrConfig
metadata:
  name: `+name+`
  namespace: default
spec:
  connection:
    url: http://sonarr:8989
    apiKeySecretRef: {name: sonarr, key: api-key}
  delayProfiles: `+profiles), &obj.Object))
		return obj
	}

	require.NoError(t, c.Create(ctx, sonarr("one-default", `[{tagLabels: [anime]}, {usenetDelay: 30}]`)))
	err := c.Create(ctx, sonarr("two-defaults", `[{usenetDelay: 30}, {tagLabels: []}]`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only one delay profile can be without tags")
}
