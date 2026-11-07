//go:build compat

// Package compat reconciles every integration against a real, pinned application
// container. Each case proves three things for that application version: the
// operator reaches Synced, the application reflects the declared settings, and a
// second pass makes no writes, so nothing the operator sends is rewritten by the
// application into a value the operator then sees as drift.
//
// Run with `make test-compat`; it needs docker (or CONTAINER_TOOL=podman).
package compat

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-logr/logr/testr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"

	automationv1alpha1 "github.com/kyleseneker/media-operator/api/automation/v1alpha1"
	curationv1alpha1 "github.com/kyleseneker/media-operator/api/curation/v1alpha1"
	downloadsv1alpha1 "github.com/kyleseneker/media-operator/api/downloads/v1alpha1"
	indexersv1alpha1 "github.com/kyleseneker/media-operator/api/indexers/v1alpha1"
	mediaserversv1alpha1 "github.com/kyleseneker/media-operator/api/mediaservers/v1alpha1"
	pvrv1alpha1 "github.com/kyleseneker/media-operator/api/pvr/v1alpha1"
	requestsv1alpha1 "github.com/kyleseneker/media-operator/api/requests/v1alpha1"
	subtitlesv1alpha1 "github.com/kyleseneker/media-operator/api/subtitles/v1alpha1"
	transcodev1alpha1 "github.com/kyleseneker/media-operator/api/transcode/v1alpha1"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/export"
)

var (
	// hostIP is the address the operator and the containers use to reach each
	// other and the suite's fake services.
	hostIP string
	k8s    client.Client
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		fmt.Fprintln(os.Stderr, "KUBEBUILDER_ASSETS unset; run via `make test-compat`")
		return 1
	}
	var err error
	if hostIP, err = outboundIP(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	env := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := env.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "starting control plane: %v\n", err)
		return 1
	}
	defer func() { _ = env.Stop() }()

	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		clientgoscheme.AddToScheme, automationv1alpha1.AddToScheme, curationv1alpha1.AddToScheme,
		downloadsv1alpha1.AddToScheme, indexersv1alpha1.AddToScheme, mediaserversv1alpha1.AddToScheme,
		pvrv1alpha1.AddToScheme, requestsv1alpha1.AddToScheme, subtitlesv1alpha1.AddToScheme,
		transcodev1alpha1.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			fmt.Fprintf(os.Stderr, "registering scheme: %v\n", err)
			return 1
		}
	}
	if k8s, err = client.New(cfg, client.Options{Scheme: scheme}); err != nil {
		fmt.Fprintf(os.Stderr, "building client: %v\n", err)
		return 1
	}
	return m.Run()
}

// outboundIP is the source address of the default route. MEDIA_OPERATOR_COMPAT_HOST
// overrides it when that address is not reachable from containers.
func outboundIP() (string, error) {
	if ip := os.Getenv("MEDIA_OPERATOR_COMPAT_HOST"); ip != "" {
		return ip, nil
	}
	conn, err := net.Dial("udp", "192.0.2.1:9")
	if err != nil {
		return "", fmt.Errorf("finding the host's outbound address: %w", err)
	}
	defer func() { _ = conn.Close() }()
	return conn.LocalAddr().(*net.UDPAddr).IP.String(), nil
}

// compatCase is one application version under test.
type compatCase struct {
	// fixture is a CR under testdata/; ${VAR} references are filled from vars.
	fixture string
	vars    map[string]string
	// secrets become one Secret named "compat-credentials" in the test namespace.
	secrets    map[string]string
	newObj     func() ctrlcommon.ConfigResource
	reconciler func(client.Client) reconcile.Reconciler
	proxy      *recordingProxy
	// read classifies requests the application treats as reads; nil means GET only.
	read isRead
	// observe is true when the integration supports driftPolicy: observe.
	observe bool
	// verify reads the application directly after the first reconcile.
	verify func(t *testing.T)
	// export names the app for media-operator export, whose output must then
	// describe the application exactly: observe mode finds nothing different.
	export string
}

func checkCompat(t *testing.T, tc compatCase) {
	t.Helper()
	ctx := context.Background()
	read := tc.read
	if read == nil {
		read = getOnly
	}

	ns := &corev1.Namespace{GenerateName: "compat-"}
	require.NoError(t, k8s.Create(ctx, ns), "creating namespace")
	secret := &corev1.Secret{Name: "compat-credentials", Namespace: ns.Name, StringData: tc.secrets}
	require.NoError(t, k8s.Create(ctx, secret), "creating secret")

	obj := tc.newObj()
	loadFixture(t, tc.fixture, tc.vars, obj)
	obj.SetNamespace(ns.Name)
	require.NoError(t, k8s.Create(ctx, obj), "creating %s", tc.fixture)
	r := tc.reconciler(k8s)
	key := types.NamespacedName{Namespace: ns.Name, Name: obj.GetName()}

	synced := reconcileUntilCurrent(t, r, key, tc.newObj)
	if !isSynced(synced) {
		t.Logf("writes during the first reconcile:\n%s", formatRequests(tc.proxy.writes(read)))
	}
	requireSynced(t, synced, "first reconcile")
	if tc.verify != nil {
		tc.verify(t)
	}

	tc.proxy.reset()
	second := reconcileUntilCurrent(t, r, key, tc.newObj)
	if !isSynced(second) {
		t.Logf("requests during the second reconcile:\n%s", formatRequests(tc.proxy.all()))
	}
	requireSynced(t, second, "second reconcile")
	writes := tc.proxy.writes(read)
	assert.Empty(t, writes, "second reconcile wrote to the application; these settings do not read back as written:\n%s",
		formatRequests(writes))

	if !tc.observe {
		return
	}
	patch := client.RawPatch(types.MergePatchType, []byte(`{"spec":{"reconcile":{"driftPolicy":"observe"}}}`))
	require.NoError(t, k8s.Patch(ctx, synced, patch), "switching to observe")
	tc.proxy.reset()
	requireSynced(t, reconcileUntilCurrent(t, r, key, tc.newObj), "observe reconcile")
	writes = tc.proxy.writes(read)
	assert.Empty(t, writes, "observe mode wrote to the application:\n%s", formatRequests(writes))

	if tc.export != "" {
		checkExport(t, tc, r, ns.Name, read)
	}
}

func checkExport(t *testing.T, tc compatCase, r reconcile.Reconciler, namespace string, read isRead) {
	t.Helper()
	ctx := context.Background()
	res, err := export.Run(ctx, export.Options{
		App: tc.export, URL: tc.proxy.url(), APIKey: tc.secrets["api-key"], Name: "exported", Namespace: namespace,
	})
	require.NoError(t, err, "exporting %s", tc.export)
	out, err := res.YAML()
	require.NoError(t, err)
	for _, key := range res.Unfilled {
		res.Secret.StringData[key] = "unreadable"
	}
	require.NoError(t, k8s.Create(ctx, res.Secret), "creating the exported Secret")
	obj, ok := res.Config.(ctrlcommon.ConfigResource)
	require.True(t, ok)
	require.NoError(t, k8s.Create(ctx, obj), "creating the exported config:\n%s", out)
	patch := client.RawPatch(types.MergePatchType, []byte(`{"spec":{"reconcile":{"driftPolicy":"observe"}}}`))
	require.NoError(t, k8s.Patch(ctx, obj, patch), "switching the exported config to observe")

	tc.proxy.reset()
	exported := reconcileUntilCurrent(t, r, types.NamespacedName{Namespace: namespace, Name: obj.GetName()}, tc.newObj)
	if !isSynced(exported) {
		t.Logf("exported config:\n%s\nobserved differences: %v", out, observedDrift(t, tc.export))
	}
	requireSynced(t, exported, "exported config")
	writes := tc.proxy.writes(read)
	assert.Empty(t, writes, "reconciling the exported config wrote to the application:\n%s", formatRequests(writes))
}

// reconcileUntilCurrent reconciles until status describes the current generation;
// the first pass may only add the finalizer.
func reconcileUntilCurrent(
	t *testing.T, r reconcile.Reconciler, key types.NamespacedName, newObj func() ctrlcommon.ConfigResource,
) ctrlcommon.ConfigResource {
	t.Helper()
	// Debug verbosity includes each failed request's full response, which status
	// conditions omit because an application may echo credentials back.
	ctx := logf.IntoContext(context.Background(), testr.NewWithOptions(t, testr.Options{Verbosity: 1}))
	for range 5 {
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		require.NoError(t, err, "reconcile")
		obj := newObj()
		require.NoError(t, k8s.Get(ctx, key, obj), "reading %s", key)
		if observed := obj.GetObservedGeneration(); observed != nil && *observed == obj.GetGeneration() {
			return obj
		}
	}
	require.FailNow(t, "status never reached the current generation", "%s", key)
	return nil
}

func isSynced(obj ctrlcommon.ConfigResource) bool {
	for _, c := range *obj.GetConditions() {
		if c.Type == engine.ConditionSynced {
			return c.Status == metav1.ConditionTrue
		}
	}
	return false
}

func requireSynced(t *testing.T, obj ctrlcommon.ConfigResource, stage string) {
	t.Helper()
	for _, c := range *obj.GetConditions() {
		if c.Type != engine.ConditionSynced {
			continue
		}
		require.Equal(t, metav1.ConditionTrue, c.Status, "%s: Synced=%s (%s): %s", stage, c.Status, c.Reason, c.Message)
		return
	}
	require.FailNow(t, "no Synced condition", stage)
}

func loadFixture(t *testing.T, name string, vars map[string]string, into client.Object) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	missing := map[string]bool{}
	expanded := os.Expand(string(raw), func(key string) string {
		value, ok := vars[key]
		if !ok {
			missing[key] = true
		}
		return value
	})
	require.Empty(t, missing, "%s references unset variables", name)
	require.NoError(t, yaml.UnmarshalStrict([]byte(expanded), into), "decoding %s", name)
}

func formatRequests(rs []request) string {
	lines := make([]string, len(rs))
	for i, r := range rs {
		lines[i] = "  " + r.String()
	}
	return strings.Join(lines, "\n")
}

// waitFor polls until ready returns nil, for applications that start slowly.
func waitFor(t *testing.T, what string, timeout time.Duration, ready func() error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var err error
	for time.Now().Before(deadline) {
		if err = ready(); err == nil {
			return
		}
		time.Sleep(time.Second)
	}
	require.NoError(t, err, "%s not ready after %s", what, timeout)
}

func observedDrift(t *testing.T, app string) []string {
	t.Helper()
	families, err := ctrlmetrics.Registry.Gather()
	require.NoError(t, err)
	var drift []string
	for _, family := range families {
		if family.GetName() != "media_operator_drift_observed_total" {
			continue
		}
		for _, m := range family.GetMetric() {
			labels := map[string]string{}
			for _, l := range m.GetLabel() {
				labels[l.GetName()] = l.GetValue()
			}
			if labels["app"] == app {
				drift = append(drift, labels["resource_type"]+"/"+labels["name"])
			}
		}
	}
	return drift
}
