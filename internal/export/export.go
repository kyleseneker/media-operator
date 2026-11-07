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

	pvrv1alpha1 "github.com/kyleseneker/media-operator/api/pvr/v1alpha1"
	pvrclient "github.com/kyleseneker/media-operator/internal/client/pvr"
	ctrlcommon "github.com/kyleseneker/media-operator/internal/controller/common"
	"github.com/kyleseneker/media-operator/internal/engine"
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
// Unfilled lists the Secret keys whose values the app would not reveal.
type Result struct {
	Config   client.Object
	Secret   *corev1.Secret
	Unfilled []string
}

type app struct {
	auth   engine.AuthType
	newObj func() client.Object
	export func(ctx context.Context, hc *engine.HTTPClient, creds *credentials) (map[string]any, error)
}

var apps = map[string]app{
	"sonarr":  pvrApp(pvrclient.Sonarr, func() client.Object { return &pvrv1alpha1.SonarrConfig{} }),
	"radarr":  pvrApp(pvrclient.Radarr, func() client.Object { return &pvrv1alpha1.RadarrConfig{} }),
	"lidarr":  pvrApp(pvrclient.Lidarr, func() client.Object { return &pvrv1alpha1.LidarrConfig{} }),
	"readarr": pvrApp(pvrclient.Readarr, func() client.Object { return &pvrv1alpha1.ReadarrConfig{} }),
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

	creds := &credentials{secret: o.SecretName, values: map[string]string{apiKeyKey: o.APIKey}}
	spec, err := a.export(ctx, hc, creds)
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
		secret.StringData[key] = creds.values[key]
		if creds.values[key] == "" {
			unfilled = append(unfilled, key)
		}
	}
	return Result{Config: obj, Secret: secret, Unfilled: unfilled}, nil
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
