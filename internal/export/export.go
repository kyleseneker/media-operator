// Package export reads an app's configuration and writes it as a config
// resource the operator can manage, with a Secret for the credentials.
package export

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	indexersv1alpha1 "github.com/kyleseneker/media-operator/api/indexers/v1alpha1"
	pvrv1alpha1 "github.com/kyleseneker/media-operator/api/pvr/v1alpha1"
	pvrclient "github.com/kyleseneker/media-operator/internal/client/pvr"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/kyleseneker/media-operator/internal/reconciler"
)

const apiKeyKey = "api-key"

// Options selects the app to read and names what export writes.
type Options struct {
	App string
	// URL is where export reads the app; ConnectionURL, if set, is the URL the
	// operator will use instead, such as a Service name when URL is a port-forward.
	URL                string
	ConnectionURL      string
	APIKey             string
	Name               string
	Namespace          string
	SecretName         string
	Exclude            []string
	InsecureSkipVerify bool
}

// Result is an exported config and the Secret it reads credentials from.
// Unfilled lists the Secret keys whose values the app would not reveal, and
// Warnings what the app has that the config cannot express.
type Result struct {
	Config   client.Object
	Secret   *corev1.Secret
	Unfilled []string
	Warnings []string
}

type app struct {
	auth   engine.AuthType
	newObj func() client.Object
	export func(r *reader) (map[string]any, error)
}

var apps = map[string]app{
	"sonarr":  pvrApp(pvrclient.Sonarr, func() client.Object { return &pvrv1alpha1.SonarrConfig{} }),
	"radarr":  pvrApp(pvrclient.Radarr, func() client.Object { return &pvrv1alpha1.RadarrConfig{} }),
	"lidarr":  pvrApp(pvrclient.Lidarr, func() client.Object { return &pvrv1alpha1.LidarrConfig{} }),
	"readarr": pvrApp(pvrclient.Readarr, func() client.Object { return &pvrv1alpha1.ReadarrConfig{} }),
	"prowlarr": {
		auth: engine.AuthAPIKey, newObj: func() client.Object { return &indexersv1alpha1.ProwlarrConfig{} }, export: exportProwlarr,
	},
}

// Apps lists the apps export supports.
func Apps() []string {
	return slices.Sorted(maps.Keys(apps))
}

// Run reads an app and returns a config that describes it.
func Run(ctx context.Context, o Options) (Result, error) {
	a, ok := apps[o.App]
	if !ok {
		return Result{}, fmt.Errorf("unsupported app %q; supported: %s", o.App, strings.Join(Apps(), ", "))
	}
	if o.Name == "" {
		o.Name = o.App
	}
	if o.SecretName == "" {
		o.SecretName = o.Name + "-credentials"
	}
	if o.ConnectionURL == "" {
		o.ConnectionURL = o.URL
	}
	obj := a.newObj()
	sections := specFields(obj)
	for _, x := range o.Exclude {
		if !slices.Contains(sections, x) {
			return Result{}, fmt.Errorf("cannot exclude %q; %s has: %s", x, o.App, strings.Join(sections, ", "))
		}
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	if o.InsecureSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // the user asked to skip verification
	}
	hc, err := engine.NewHTTPClient(o.URL, a.auth,
		engine.WithAPIKey(o.APIKey), engine.WithTransport(transport), engine.WithAppLabel(o.App))
	if err != nil {
		return Result{}, err
	}

	r := &reader{ctx: ctx, hc: hc, creds: &credentials{secret: o.SecretName, values: map[string]string{apiKeyKey: o.APIKey}}}
	spec, err := a.export(r)
	if err != nil {
		return Result{}, err
	}
	for _, x := range o.Exclude {
		delete(spec, x)
	}
	spec["connection"] = map[string]any{
		"url":             o.ConnectionURL,
		"apiKeySecretRef": map[string]any{"name": o.SecretName, "key": apiKeyKey},
	}
	raw, err := json.Marshal(map[string]any{
		"apiVersion": "media-operator.dev/v1alpha1",
		"kind":       reflect.TypeOf(obj).Elem().Name(),
		"metadata":   map[string]any{"name": o.Name, "namespace": o.Namespace},
		"spec":       spec,
	})
	if err != nil {
		return Result{}, err
	}
	if err := json.Unmarshal(raw, obj); err != nil {
		return Result{}, fmt.Errorf("reading %s into %T: %w", o.App, obj, err)
	}

	secret := &corev1.Secret{
		APIVersion: "v1", Kind: "Secret",
		Name: o.SecretName, Namespace: o.Namespace,
		StringData: map[string]string{},
	}
	var unfilled []string
	for _, key := range referencedKeys(obj, o.SecretName) {
		secret.StringData[key] = r.creds.values[key]
		if r.creds.values[key] == "" {
			unfilled = append(unfilled, key)
		}
	}
	return Result{Config: obj, Secret: secret, Unfilled: unfilled, Warnings: r.warnings}, nil
}

// YAML renders the Secret and the config as one multi-document YAML stream.
func (r Result) YAML() ([]byte, error) {
	var docs []string
	for _, obj := range []any{r.Secret, r.Config} {
		raw, err := json.Marshal(obj)
		if err != nil {
			return nil, err
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		delete(m, "status")
		if meta, ok := m["metadata"].(map[string]any); ok {
			delete(meta, "creationTimestamp")
		}
		out, err := yaml.Marshal(m)
		if err != nil {
			return nil, err
		}
		docs = append(docs, string(out))
	}
	return []byte(strings.Join(docs, "---\n")), nil
}

type credentials struct {
	secret string
	values map[string]string
}

var invalidKeyChars = regexp.MustCompile(`[^a-z0-9.]+`)

func (c *credentials) ref(value string, parts ...string) map[string]any {
	base := strings.Trim(invalidKeyChars.ReplaceAllString(strings.ToLower(strings.Join(parts, "-")), "-"), "-")
	key := base
	for i := 2; ; i++ {
		if _, taken := c.values[key]; !taken {
			break
		}
		key = fmt.Sprintf("%s-%d", base, i)
	}
	c.values[key] = value
	return map[string]any{"name": c.secret, "key": key}
}

func specFields(obj client.Object) []string {
	spec, _ := reflect.TypeOf(obj).Elem().FieldByName("Spec")
	var names []string
	for field := range spec.Type.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name != "" && name != "connection" && name != "reconcile" {
			names = append(names, name)
		}
	}
	return names
}

func referencedKeys(obj any, secret string) []string {
	var keys []string
	for _, ref := range ctrlcommon.SecretKeyRefs(obj) {
		if ref.Name == secret && !slices.Contains(keys, ref.Key) {
			keys = append(keys, ref.Key)
		}
	}
	return keys
}

type reader struct {
	ctx      context.Context
	hc       *engine.HTTPClient
	prefix   string
	creds    *credentials
	tags     map[float64]string
	warnings []string
}

type resource struct {
	key    string
	path   string
	schema string
	build  func(item map[string]any, defaults map[string]map[string]any) map[string]any
}

func (r *reader) warn(format string, args ...any) {
	r.warnings = append(r.warnings, fmt.Sprintf(format, args...))
}

func (r *reader) list(path string) ([]map[string]any, error) {
	return r.hc.GetJSONList(r.ctx, r.prefix+path)
}

func (r *reader) names(path, field string) (map[float64]string, error) {
	items, err := r.list(path)
	if err != nil {
		return nil, err
	}
	out := map[float64]string{}
	for _, item := range items {
		out[num(item["id"])], _ = item[field].(string)
	}
	return out, nil
}

func (r *reader) defaults(path string) (map[string]map[string]any, error) {
	schemas, err := r.list(path)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]any{}
	for _, schema := range schemas {
		impl, _ := schema["implementation"].(string)
		values := map[string]any{}
		for _, f := range fieldList(schema) {
			name, _ := f["name"].(string)
			values[name] = f["value"]
		}
		out[impl] = values
	}
	return out, nil
}

func (r *reader) resources(spec map[string]any, resources []resource) error {
	for _, res := range resources {
		items, err := r.list(res.path)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			continue
		}
		var defaults map[string]map[string]any
		if res.schema != "" {
			if defaults, err = r.defaults(res.schema); err != nil {
				return err
			}
		}
		out := make([]any, 0, len(items))
		for _, item := range items {
			out = append(out, res.build(item, defaults))
		}
		spec[res.key] = out
	}
	return nil
}

// fields keeps provider fields that differ from the implementation's defaults,
// turning masked values into Secret references where the resource can read
// one (secrets) and warning where it cannot.
func (r *reader) fields(item map[string]any, defaults map[string]map[string]any, skip map[string]bool, kind string, secrets bool) []any {
	impl, _ := item["implementation"].(string)
	name, _ := item["name"].(string)
	var out []any
	for _, f := range fieldList(item) {
		key, _ := f["name"].(string)
		value := f["value"]
		switch {
		case skip[key]:
		case reconciler.IsMasked(value) && secrets:
			out = append(out, map[string]any{"name": key, "valueFrom": r.creds.ref("", kind, name, key)})
		case reconciler.IsMasked(value):
			r.warn("%s %q: the app hides %s and the config can't read it from a Secret; set it in the app", kind, name, key)
		case reflect.DeepEqual(value, defaults[impl][key]):
		default:
			out = append(out, map[string]any{"name": key, "value": value})
		}
	}
	return out
}

func (r *reader) tagLabels(item map[string]any) []any {
	var labels []any
	ids, _ := item["tags"].([]any)
	for _, id := range ids {
		if label, ok := r.tags[num(id)]; ok {
			labels = append(labels, label)
		}
	}
	return labels
}

var notificationEvents = func() []string {
	var events []string
	t := reflect.TypeFor[commonv1alpha1.Notification]()
	for field := range t.Fields() {
		if field.Type == reflect.TypeFor[*bool]() {
			name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			events = append(events, name)
		}
	}
	return events
}()

func (r *reader) notification(item map[string]any, defaults map[string]map[string]any) map[string]any {
	n := pick(item, "name", "implementation")
	for _, event := range notificationEvents {
		v, ok := item[event]
		if !ok {
			continue
		}
		if supported, known := item["supports"+strings.ToUpper(event[:1])+event[1:]].(bool); known && !supported {
			continue
		}
		n[event] = v
	}
	contract(n, item)
	n["fields"] = r.fields(item, defaults, nil, "notification", true)
	n["tagLabels"] = r.tagLabels(item)
	return n
}

func tag(item map[string]any, _ map[string]map[string]any) map[string]any {
	return map[string]any{"label": item["label"]}
}

func protocol(out map[string]any) {
	if p, ok := out["protocol"].(string); ok {
		out["protocol"] = strings.ToLower(strings.TrimSuffix(p, "DownloadProtocol"))
	}
}

func contract(out, item map[string]any) {
	impl, _ := item["implementation"].(string)
	if contract, _ := item["configContract"].(string); contract != "" && contract != impl+"Settings" {
		out["configContract"] = contract
	}
}

func fieldValues(item map[string]any) map[string]any {
	values := map[string]any{}
	for _, f := range fieldList(item) {
		key, _ := f["name"].(string)
		values[key] = f["value"]
	}
	return values
}

func pick(item map[string]any, keys ...string) map[string]any {
	out := map[string]any{}
	for _, k := range keys {
		if v, ok := item[k]; ok {
			out[k] = v
		}
	}
	return out
}

func list(v any) []map[string]any {
	raw, _ := v.([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func fieldList(item map[string]any) []map[string]any {
	return list(item["fields"])
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}
