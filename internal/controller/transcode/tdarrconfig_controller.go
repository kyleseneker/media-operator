package transcode

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	transcodev1alpha1 "github.com/kyleseneker/media-operator/api/transcode/v1alpha1"
	tdarrclient "github.com/kyleseneker/media-operator/internal/client/tdarr"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

// TdarrConfigReconciler reconciles a TdarrConfig object.
type TdarrConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=tdarrconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=tdarrconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=media-operator.dev,resources=tdarrconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *TdarrConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var config transcodev1alpha1.TdarrConfig
	if err := r.Get(ctx, req.NamespacedName, &config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if done, after := ctrlcommon.HandleLifecycle(ctx, r.Client, r.Recorder, &config, nil); done {
		return ctrl.Result{RequeueAfter: after}, nil
	}

	// Resolve API key
	apiKey, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, config.Spec.Connection.APIKeySecretRef)
	if err != nil {
		logger.Error(err, "failed to resolve API key secret")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Client, &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	tlsCfg, err := engine.ResolveTLSConfig(ctx, r.Client, config.Namespace, config.Spec.Connection.TLS)
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Client, &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	hc, err := engine.NewHTTPClient(config.Spec.Connection.URL, engine.AuthAPIKey, engine.WithAPIKey(apiKey), engine.WithAPIKeyHeader("x-api-key"), engine.WithTLSConfig(tlsCfg), engine.WithAppLabel("tdarr"), engine.WithTimeout(3*time.Minute))
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Client, &config, engine.ReasonInvalidConfig, err.Error())
		return ctrl.Result{}, nil
	}
	tc := tdarrclient.NewClient(hc)

	// Health check
	if err := tc.Ping(ctx); err != nil {
		logger.Error(err, "Tdarr unreachable")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Client, &config, engine.ReasonAppUnreachable, err.Error())
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	sync := &tdarrSync{App: "tdarr", Observe: ctrlcommon.ObserveOnly(config.Spec.Reconcile), tc: tc}
	syncErrors := sync.run(ctx, &config.Spec)
	ctrlcommon.UpdateSyncStatus(ctx, r.Client, &config, sync.Observe, len(sync.Drift()) > 0, syncErrors)

	return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
}

// tdarrSync applies one TdarrConfig. With observe set it only reads, and
// records what it would have written.
type tdarrSync struct {
	engine.Writes
	tc *tdarrclient.Client
}

func (s *tdarrSync) run(ctx context.Context, spec *transcodev1alpha1.TdarrConfigSpec) []string {
	logger := log.FromContext(ctx)
	var syncErrors []string
	for _, lib := range spec.Libraries {
		if err := s.library(ctx, lib); err != nil {
			logger.Error(err, "failed to reconcile library", "id", lib.ID)
			syncErrors = append(syncErrors, fmt.Sprintf("library(%s): %v", lib.ID, err))
		}
	}
	for _, flow := range spec.Flows {
		if err := s.flow(ctx, flow); err != nil {
			logger.Error(err, "failed to reconcile flow", "id", flow.ID)
			syncErrors = append(syncErrors, fmt.Sprintf("flow(%s): %v", flow.ID, err))
		}
	}
	if spec.Workers != nil {
		if err := s.workers(ctx, spec.Workers); err != nil {
			logger.Error(err, "failed to reconcile workers")
			syncErrors = append(syncErrors, fmt.Sprintf("workers: %v", err))
		}
	}
	return syncErrors
}

func (s *tdarrSync) library(ctx context.Context, lib transcodev1alpha1.TdarrLibrary) error {
	if err := s.upsertDoc(ctx, "LibrarySettingsJSONDB", lib.ID, buildTdarrLibraryDoc(lib)); err != nil {
		return err
	}
	return s.libraryVariables(ctx, lib)
}

// variablesCollection holds Tdarr's user variables. Library variables are
// documents here rather than fields on the library itself.
const variablesCollection = "VariablesJSONDB"

// libraryVariableType is the type tag Tdarr stores on a library-scoped variable.
// Flows read these as args.userVariables.library.<key>.
func libraryVariableType(libraryID string) string {
	return "library:" + libraryID
}

// libraryVariables converges the VariablesJSONDB documents for a library. Each
// variable is its own document rather than a field on the library, so they are
// matched by key within the library's type tag.
func (s *tdarrSync) libraryVariables(ctx context.Context, lib transcodev1alpha1.TdarrLibrary) error {
	varType := libraryVariableType(lib.ID)

	docs, err := s.tc.GetAll(ctx, variablesCollection)
	if err != nil {
		return fmt.Errorf("listing variables: %w", err)
	}

	existing := make(map[string]map[string]any, len(docs))
	for _, doc := range docs {
		if t, _ := doc["type"].(string); t != varType {
			continue
		}
		if key, _ := doc["key"].(string); key != "" {
			existing[key] = doc
		}
	}

	keys := make([]string, 0, len(lib.Variables))
	for k := range lib.Variables {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		value := lib.Variables[key]
		doc, found := existing[key]
		if !found {
			obj := map[string]any{
				"key":   key,
				"value": value,
				"type":  varType,
				"date":  time.Now().UnixMilli(),
			}
			err := s.Apply(variablesCollection, lib.ID+"/"+key, func() error {
				return s.tc.Insert(ctx, variablesCollection, "", obj)
			})
			if err != nil {
				return fmt.Errorf("inserting variable %s: %w", key, err)
			}
			continue
		}
		if current, _ := doc["value"].(string); current == value {
			continue
		}
		id, _ := doc["_id"].(string)
		obj := map[string]any{
			"_id":   id,
			"key":   key,
			"value": value,
			"type":  varType,
			"date":  time.Now().UnixMilli(),
		}
		err := s.Apply(variablesCollection, lib.ID+"/"+key, func() error {
			return s.tc.Update(ctx, variablesCollection, id, obj)
		})
		if err != nil {
			return fmt.Errorf("updating variable %s: %w", key, err)
		}
	}

	for key, doc := range existing {
		if _, declared := lib.Variables[key]; declared {
			continue
		}
		id, _ := doc["_id"].(string)
		if id == "" {
			continue
		}
		err := s.Apply(variablesCollection, lib.ID+"/"+key, func() error {
			return s.tc.Remove(ctx, variablesCollection, id)
		})
		if err != nil {
			return fmt.Errorf("removing variable %s: %w", key, err)
		}
	}

	return nil
}

const scheduleHoursPerWeek = 168

// alwaysOnSchedule renders a schedule with every hour of the week enabled.
func alwaysOnSchedule() []any {
	schedule := make([]any, scheduleHoursPerWeek)
	for i := range schedule {
		schedule[i] = map[string]any{"checked": true}
	}
	return schedule
}

// buildTdarrLibraryDoc renders the library document Tdarr stores.
func buildTdarrLibraryDoc(lib transcodev1alpha1.TdarrLibrary) map[string]any {
	obj := map[string]any{
		"_id":             lib.ID,
		"name":            lib.Name,
		"schedule":        alwaysOnSchedule(),
		"foldersToIgnore": lib.FoldersToIgnore,
	}
	if lib.Folder != "" {
		obj["folder"] = lib.Folder
	}
	if lib.Cache != "" {
		obj["cache"] = lib.Cache
	}
	if lib.Container != "" {
		obj["container"] = lib.Container
	}
	if lib.ContainerFilter != "" {
		obj["containerFilter"] = lib.ContainerFilter
	}
	if lib.FolderWatching != nil {
		obj["folderWatching"] = *lib.FolderWatching
	}
	if lib.ProcessLibrary != nil {
		obj["processLibrary"] = *lib.ProcessLibrary
	}
	if lib.ProcessTranscodes != nil {
		obj["processTranscodes"] = *lib.ProcessTranscodes
	}
	if lib.ProcessHealthChecks != nil {
		obj["processHealthChecks"] = *lib.ProcessHealthChecks
	}
	if lib.HealthCheckMode != "" {
		obj["handbrakescan"] = lib.HealthCheckMode == "quick"
		obj["ffmpegscan"] = lib.HealthCheckMode == "thorough"
	}
	if lib.DecisionMode != "" {
		obj["decisionMaker"] = map[string]any{
			"settingsFlows":      lib.DecisionMode == "flows",
			"settingsPlugin":     lib.DecisionMode == "plugins",
			"settingsVideo":      false,
			"settingsAudio":      false,
			"videoExcludeSwitch": false,
			"audioExcludeSwitch": false,
		}
	}
	if lib.ScanOnStart != nil {
		obj["scanOnStart"] = *lib.ScanOnStart
	}
	if lib.ScheduledScanFindNew != nil {
		obj["scheduledScanFindNew"] = *lib.ScheduledScanFindNew
	}
	if lib.ScannerThreadCount != nil {
		obj["scannerThreadCount"] = *lib.ScannerThreadCount
	}
	if lib.FFmpeg != nil {
		obj["ffmpeg"] = *lib.FFmpeg
	}
	if lib.Priority != nil {
		obj["priority"] = *lib.Priority
	}
	if lib.FlowID != "" {
		obj["flowId"] = lib.FlowID
	}
	if len(lib.PluginStack) > 0 {
		obj["pluginStack"] = lib.PluginStack
	}
	if len(lib.Variables) > 0 {
		user := make(map[string]any, len(lib.Variables))
		for k, v := range lib.Variables {
			user[k] = v
		}
		obj["variables"] = map[string]any{"user": user}
	}

	return obj
}

func (s *tdarrSync) flow(ctx context.Context, flow transcodev1alpha1.TdarrFlow) error {
	obj := map[string]any{
		"_id":  flow.ID,
		"name": flow.Name,
	}
	if flow.Description != "" {
		obj["description"] = flow.Description
	}

	plugins, err := decodeRawList(flow.FlowPlugins)
	if err != nil {
		return fmt.Errorf("unmarshaling flowPlugins: %w", err)
	}
	obj["flowPlugins"] = plugins

	edges, err := decodeRawList(flow.FlowEdges)
	if err != nil {
		return fmt.Errorf("unmarshaling flowEdges: %w", err)
	}
	obj["flowEdges"] = edges

	return s.upsertDoc(ctx, "FlowsJSONDB", flow.ID, obj)
}

// upsertDoc writes a library or flow only when Tdarr's stored document does not
// already hold every declared value. Updating on every pass rewrote each library
// and flow every few minutes, and Tdarr rescans on a library save.
func (s *tdarrSync) upsertDoc(ctx context.Context, collection, id string, obj map[string]any) error {
	existing, err := s.tc.GetByID(ctx, collection, id)
	if err != nil {
		return fmt.Errorf("looking up %s/%s: %w", collection, id, err)
	}
	if len(existing) == 0 {
		return s.Apply(collection, id, func() error { return s.tc.Insert(ctx, collection, id, obj) })
	}
	// Compare in JSON terms: the stored document decodes to float64s and []any.
	raw, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	var desired any
	if err := json.Unmarshal(raw, &desired); err != nil {
		return err
	}
	if reconciler.Covers(existing, desired) {
		return nil
	}
	return s.Apply(collection, id, func() error { return s.tc.Update(ctx, collection, id, obj) })
}

// decodeRawList turns a list of raw JSON nodes into plain values. Tdarr stores
// flowPlugins and flowEdges as arrays, so each element decodes independently.
func decodeRawList(items []runtime.RawExtension) ([]any, error) {
	out := make([]any, 0, len(items))
	for i, it := range items {
		var v any
		if err := json.Unmarshal(it.Raw, &v); err != nil {
			return nil, fmt.Errorf("element %d: %w", i, err)
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *tdarrSync) workers(ctx context.Context, workers *transcodev1alpha1.TdarrWorkers) error {
	nodes, err := s.tc.GetNodes(ctx)
	if err != nil {
		return fmt.Errorf("getting nodes: %w", err)
	}

	nodeID, err := workerNode(nodes, workers.NodeName)
	if err != nil {
		return err
	}

	workerLimits := []struct {
		workerType string
		limit      *int
	}{
		{"transcodegpu", workers.TranscodeGPU},
		{"transcodecpu", workers.TranscodeCPU},
		{"healthcheckgpu", workers.HealthcheckGPU},
		{"healthcheckcpu", workers.HealthcheckCPU},
	}

	current := currentWorkerLimits(nodes[nodeID])
	for _, wl := range workerLimits {
		if wl.limit == nil {
			continue
		}
		if err := s.convergeWorkerLimit(ctx, nodeID, wl.workerType, current[wl.workerType], *wl.limit); err != nil {
			return fmt.Errorf("setting %s limit: %w", wl.workerType, err)
		}
	}

	return nil
}

// currentWorkerLimits reads a node's per-type worker counts. Tdarr keys them in
// lower case.
func currentWorkerLimits(node any) map[string]int {
	out := map[string]int{}
	n, ok := node.(map[string]any)
	if !ok {
		return out
	}
	limits, ok := n["workerLimits"].(map[string]any)
	if !ok {
		return out
	}
	for k, v := range limits {
		if f, ok := v.(float64); ok {
			out[strings.ToLower(k)] = int(f)
		}
	}
	return out
}

// workerNode picks the node whose worker limits the spec sets: the named one,
// or the only one. Limits are per node, and picking among several unnamed
// nodes would let each pass step a different one.
func workerNode(nodes map[string]any, name string) (string, error) {
	names := make([]string, 0, len(nodes))
	var matches []string
	for id, node := range nodes {
		n, _ := node.(map[string]any)
		nodeName, _ := n["nodeName"].(string)
		names = append(names, nodeName)
		if name != "" && nodeName == name {
			matches = append(matches, id)
		}
	}
	slices.Sort(names)
	switch {
	case len(nodes) == 0:
		return "", fmt.Errorf("no nodes registered")
	case name == "" && len(nodes) == 1:
		for id := range nodes {
			return id, nil
		}
	case name == "":
		return "", fmt.Errorf("found %d nodes (%s); set workers.nodeName to the one these limits are for",
			len(nodes), strings.Join(names, ", "))
	case len(matches) == 1:
		return matches[0], nil
	case len(matches) > 1:
		return "", fmt.Errorf("node name %q is ambiguous (%d nodes)", name, len(matches))
	}
	return "", fmt.Errorf("no node named %q (nodes: %s)", name, strings.Join(names, ", "))
}

// convergeWorkerLimit steps a worker count toward target. Tdarr's endpoint only
// increases or decreases by one, so reaching a target takes repeated calls.
func (s *tdarrSync) convergeWorkerLimit(ctx context.Context, nodeID, workerType string, current, target int) error {
	if target < 0 {
		return fmt.Errorf("negative worker count %d", target)
	}
	process := "increase"
	steps := target - current
	if steps < 0 {
		process, steps = "decrease", -steps
	}
	if steps == 0 {
		return nil
	}
	return s.Apply("workers", workerType, func() error {
		for range steps {
			if err := s.tc.StepWorkerLimit(ctx, nodeID, workerType, process); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *TdarrConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&transcodev1alpha1.TdarrConfig{}, builder.WithPredicates(ctrlcommon.ConfigChangedPredicate())).
		Watches(&corev1.Secret{}, ctrlcommon.EnqueueForSecret(r.Client, &transcodev1alpha1.TdarrConfigList{})).
		Named("tdarrconfig").
		Complete(r)
}
