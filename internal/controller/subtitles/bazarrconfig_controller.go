package subtitles

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	subtitlesv1alpha1 "github.com/kyleseneker/media-operator/api/subtitles/v1alpha1"
	bazarrclient "github.com/kyleseneker/media-operator/internal/client/bazarr"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/metrics"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

// BazarrConfigReconciler reconciles a BazarrConfig object.
type BazarrConfigReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
}

// +kubebuilder:rbac:groups=media-operator.dev,resources=bazarrconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=media-operator.dev,resources=bazarrconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=media-operator.dev,resources=bazarrconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *BazarrConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	var config subtitlesv1alpha1.BazarrConfig
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
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	tlsCfg, err := engine.ResolveTLSConfig(ctx, r.Client, config.Namespace, config.Spec.Connection.TLS)
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonSecretNotFound, err.Error())
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	hc, err := engine.NewHTTPClient(config.Spec.Connection.URL, engine.AuthFormEncoded, engine.WithAPIKey(apiKey), engine.WithAPIKeyHeader("X-API-KEY"), engine.WithTLSConfig(tlsCfg), engine.WithAppLabel("bazarr"))
	if err != nil {
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonInvalidConfig, err.Error())
		return ctrl.Result{}, nil
	}
	bc := bazarrclient.NewClient(hc)

	// Health check
	if err := bc.Ping(ctx); err != nil {
		logger.Error(err, "Bazarr unreachable")
		ctrlcommon.UpdateStatusUnreachable(ctx, r.Status(), &config, engine.ReasonAppUnreachable, err.Error())
		return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
	}

	desired, problems := r.bazarrDesired(ctx, &config)
	settings, err := bc.GetSettings(ctx)
	if err != nil {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, false, engine.ReasonSyncFailed, fmt.Sprintf("reading settings: %v", err))
		return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
	}
	var profiles []map[string]any
	if desired.Has("languages-profiles") {
		if profiles, err = bc.GetLanguageProfiles(ctx); err != nil {
			ctrlcommon.UpdateStatus(ctx, r.Status(), &config, false, engine.ReasonSyncFailed, fmt.Sprintf("reading language profiles: %v", err))
			return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
		}
	}
	changes := bazarrChanges(desired, settings, profiles)

	if ctrlcommon.ObserveOnly(config.Spec.Reconcile) {
		for key := range changes {
			metrics.DriftObservedTotal.WithLabelValues("bazarr", bazarrSettingSection(key), key).Inc()
		}
		var observeErr error
		if len(problems) > 0 {
			observeErr = fmt.Errorf("%v", problems)
		}
		ctrlcommon.UpdateObservationStatus(ctx, r.Status(), &config, len(changes) > 0, observeErr)
		return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
	}

	if len(changes) > 0 {
		for key := range changes {
			metrics.DriftCorrectedTotal.WithLabelValues("bazarr", bazarrSettingSection(key), key).Inc()
		}
		// One request with only what differs: every save reconnects Bazarr to
		// Sonarr and Radarr, and enabling useSonarr in a request before the one
		// carrying the connection hangs while Bazarr retries 127.0.0.1.
		if err := bc.PostForm(ctx, "/api/system/settings", changes); err != nil {
			logger.Error(err, "failed to write bazarr settings")
			problems = append(problems, fmt.Sprintf("settings: %v", err))
		}
	}

	if len(problems) > 0 {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, false, engine.ReasonSyncFailed, fmt.Sprintf("partial sync failure: %v", problems))
	} else {
		ctrlcommon.UpdateStatus(ctx, r.Status(), &config, true, engine.ReasonSynced, "all configuration sections synced")
	}

	return ctrl.Result{RequeueAfter: ctrlcommon.ReconcileInterval(config.Spec.Reconcile)}, nil
}

// bazarrDesired renders every declared setting as the form fields Bazarr's
// settings endpoint takes. Sections whose Secrets cannot be read are left out
// and reported, so the rest still reconcile.
func (r *BazarrConfigReconciler) bazarrDesired(ctx context.Context, config *subtitlesv1alpha1.BazarrConfig) (url.Values, []string) {
	spec := config.Spec
	form := url.Values{}
	var problems []string
	merge := func(v url.Values) { maps.Copy(form, v) }

	if spec.General != nil {
		merge(bazarrclient.StructToFormData("general", spec.General))
	}
	if spec.SubSync != nil {
		merge(bazarrclient.StructToFormData("subsync", spec.SubSync))
	}
	for _, c := range []struct {
		section string
		conn    *subtitlesv1alpha1.BazarrAppConnection
	}{{"sonarr", spec.SonarrConnection}, {"radarr", spec.RadarrConnection}} {
		if c.conn == nil {
			continue
		}
		key, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, config.Namespace, c.conn.APIKeySecretRef)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%sConnection: resolving apiKeySecretRef: %v", c.section, err))
			continue
		}
		merge(bazarrclient.StructToFormData(c.section, c.conn))
		form.Set(fmt.Sprintf("settings-%s-apikey", c.section), key)
	}
	if len(spec.Providers) > 0 {
		providers, err := r.bazarrProviderForm(ctx, config.Namespace, spec.Providers)
		if err != nil {
			problems = append(problems, fmt.Sprintf("providers: %v", err))
		} else {
			merge(providers)
		}
	}
	if spec.Languages != nil {
		languages, err := bazarrclient.LanguagesForm(spec.Languages)
		if err != nil {
			problems = append(problems, fmt.Sprintf("languages: %v", err))
		} else {
			merge(languages)
		}
	}
	return form, problems
}

func (r *BazarrConfigReconciler) bazarrProviderForm(ctx context.Context, namespace string, providers []subtitlesv1alpha1.BazarrProvider) (url.Values, error) {
	form := url.Values{}
	for _, p := range providers {
		if p.Enabled == nil || *p.Enabled {
			form.Add("settings-general-enabled_providers", p.Name)
		}
		for k, v := range p.Settings {
			// "ENV:secretName:secretKey" reads the value from a Secret.
			resolvedVal := v
			if strings.HasPrefix(v, "ENV:") {
				parts := strings.SplitN(v[4:], ":", 2)
				if len(parts) == 2 {
					ref := commonv1alpha1.SecretKeyRef{Name: parts[0], Key: parts[1]}
					secret, err := reconciler.ResolveSecretKeyRef(ctx, r.Client, namespace, ref)
					if err != nil {
						return nil, fmt.Errorf("resolving secret for provider %s field %s: %w", p.Name, k, err)
					}
					resolvedVal = secret
				}
			}
			form.Set(fmt.Sprintf("settings-%s-%s", p.Name, k), resolvedVal)
		}
	}
	return form, nil
}

// bazarrChanges returns the desired form fields whose value differs from what
// Bazarr reports, so a reconcile writes nothing when nothing changed.
func bazarrChanges(desired url.Values, settings map[string]any, profiles []map[string]any) url.Values {
	changes := url.Values{}
	for key, values := range desired {
		if key == "languages-profiles" {
			if !sameBazarrProfiles(profiles, values[0]) {
				changes[key] = values
			}
			continue
		}
		section, name, _ := strings.Cut(strings.TrimPrefix(key, "settings-"), "-")
		current, _ := settings[section].(map[string]any)
		if !bazarrSettingMatches(current[name], values) {
			changes[key] = values
		}
	}
	return changes
}

func bazarrSettingSection(key string) string {
	if key == "languages-profiles" {
		return "languages"
	}
	section, _, _ := strings.Cut(strings.TrimPrefix(key, "settings-"), "-")
	return section
}

// bazarrSettingMatches compares a stored setting with its form value(s). Lists
// (enabled providers and languages) compare as sets.
func bazarrSettingMatches(current any, values []string) bool {
	if list, ok := current.([]any); ok {
		want := values
		if len(values) == 1 && strings.HasPrefix(values[0], "[") {
			if err := json.Unmarshal([]byte(values[0]), &want); err != nil {
				return false
			}
		}
		have := make([]string, 0, len(list))
		for _, v := range list {
			have = append(have, fmt.Sprint(v))
		}
		return slices.Equal(slices.Sorted(slices.Values(have)), slices.Sorted(slices.Values(want)))
	}
	return len(values) == 1 && bazarrScalarText(current) == values[0]
}

func bazarrScalarText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return fmt.Sprint(x)
	}
}

// sameBazarrProfiles compares only the keys the operator sends. Bazarr adds
// keys of its own (tag) and reports originalFormat as 0/1; Covers allows both.
func sameBazarrProfiles(current []map[string]any, desiredJSON string) bool {
	var want []any
	if err := json.Unmarshal([]byte(desiredJSON), &want); err != nil {
		return false
	}
	have := make([]any, len(current))
	for i, p := range current {
		have[i] = p
	}
	byID := func(list []any) {
		slices.SortStableFunc(list, func(a, b any) int {
			ai, _ := a.(map[string]any)["profileId"].(float64)
			bi, _ := b.(map[string]any)["profileId"].(float64)
			return int(ai - bi)
		})
	}
	byID(have)
	byID(want)
	return reconciler.Covers(any(have), any(want))
}

// SetupWithManager sets up the controller with the Manager.
func (r *BazarrConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&subtitlesv1alpha1.BazarrConfig{}, builder.WithPredicates(ctrlcommon.ConfigChangedPredicate())).
		Watches(&corev1.Secret{}, ctrlcommon.EnqueueForSecret(r.Client, &subtitlesv1alpha1.BazarrConfigList{})).
		Named("bazarrconfig").
		Complete(r)
}
