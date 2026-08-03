package common

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
)

func newSecretClient() *fake.ClientBuilder {
	return fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tracker", Namespace: "arr"},
		Data:       map[string][]byte{"passkey": []byte("s3cr3t")},
	})
}

func TestResolveConfigFieldsFromSecret(t *testing.T) {
	c := newSecretClient().Build()
	fields := []commonv1alpha1.ConfigField{
		{Name: "apiKey", ValueFrom: &commonv1alpha1.SecretKeyRef{Name: "tracker", Key: "passkey"}},
	}
	out, err := ResolveConfigFields(context.Background(), c, "arr", "indexer x", fields)
	if err != nil {
		t.Fatal(err)
	}
	if out[0].ValueFrom != nil {
		t.Error("valueFrom should be cleared after resolution")
	}
	b, _ := json.Marshal(out[0].Value)
	if string(b) != `"s3cr3t"` {
		t.Errorf("expected quoted secret value, got %s", string(b))
	}
	if fields[0].Value != nil {
		t.Error("input slice was mutated")
	}
}

func TestResolveConfigFieldsRejectsBothValueAndValueFrom(t *testing.T) {
	c := newSecretClient().Build()
	raw, _ := json.Marshal("literal")
	fields := []commonv1alpha1.ConfigField{{
		Name:      "apiKey",
		Value:     &commonv1alpha1.FieldValue{Raw: raw},
		ValueFrom: &commonv1alpha1.SecretKeyRef{Name: "tracker", Key: "passkey"},
	}}
	if _, err := ResolveConfigFields(context.Background(), c, "arr", "indexer x", fields); err == nil {
		t.Fatal("expected an error when both value and valueFrom are set")
	}
}

func TestResolveConfigFieldsMissingSecret(t *testing.T) {
	c := newSecretClient().Build()
	fields := []commonv1alpha1.ConfigField{
		{Name: "apiKey", ValueFrom: &commonv1alpha1.SecretKeyRef{Name: "nope", Key: "passkey"}},
	}
	if _, err := ResolveConfigFields(context.Background(), c, "arr", "indexer x", fields); err == nil {
		t.Fatal("expected an error for a missing secret")
	}
}

func TestListsReferenceSecret(t *testing.T) {
	idx := []commonv1alpha1.Indexer{{
		Name:   "tl",
		Fields: []commonv1alpha1.ConfigField{{Name: "apiKey", ValueFrom: &commonv1alpha1.SecretKeyRef{Name: "tracker", Key: "passkey"}}},
	}}
	if !ListsReferenceSecret(idx, nil, nil, "tracker") {
		t.Error("should match the referenced secret")
	}
	if ListsReferenceSecret(idx, nil, nil, "other") {
		t.Error("should not match an unrelated secret")
	}
}
