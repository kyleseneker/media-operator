package downloads

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	downloadsv1alpha1 "github.com/kyleseneker/media-operator/api/downloads/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/controller/internal/contract"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func ptr[T any](v T) *T { return &v }

func TestQBPreferencePayloadUsesAPIKeyNames(t *testing.T) {
	prefs := &downloadsv1alpha1.QBittorrentPreferences{
		SavePath:              "/data/torrents",
		Locale:                "en",
		TempPathEnabled:       ptr(true),
		DHT:                   ptr(false),
		PEX:                   ptr(false),
		LSD:                   ptr(false),
		Encryption:            ptr(1),
		MaxConnec:             ptr(200),
		MaxConnecPerTorrent:   ptr(50),
		MaxUploads:            ptr(20),
		MaxUploadsPerTorrent:  ptr(5),
		MaxRatioEnabled:       ptr(true),
		MaxRatio:              ptr("2.0"),
		MaxRatioAction:        ptr("stop"),
		MaxSeedingTimeEnabled: ptr(true),
		MaxSeedingTime:        ptr(10080),
		PreallocateAll:        ptr(true),
		WebUIPort:             ptr(8080),
	}

	got, err := qbPreferencePayload(prefs)
	if err != nil {
		t.Fatalf("qbPreferencePayload: %v", err)
	}

	want := map[string]any{
		"save_path":                "/data/torrents",
		"locale":                   "en",
		"temp_path_enabled":        true,
		"dht":                      false,
		"pex":                      false,
		"lsd":                      false,
		"encryption":               1,
		"max_connec":               200,
		"max_connec_per_torrent":   50,
		"max_uploads":              20,
		"max_uploads_per_torrent":  5,
		"max_ratio_enabled":        true,
		"max_ratio":                2.0,
		"max_ratio_act":            0,
		"max_seeding_time_enabled": true,
		"max_seeding_time":         10080,
		"preallocate_all":          true,
		"web_ui_port":              8080,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("payload mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestQBPreferencePayloadCoversEverySpecField(t *testing.T) {
	contract.AssertEveryFieldEmitted(t, &downloadsv1alpha1.QBittorrentPreferences{},
		func(p *downloadsv1alpha1.QBittorrentPreferences) map[string]any {
			out, err := qbPreferencePayload(p)
			if err != nil {
				t.Fatalf("qbPreferencePayload: %v", err)
			}
			return out
		},
		func(p *downloadsv1alpha1.QBittorrentPreferences) {
			p.MaxRatio = ptr("1.0")
			p.MaxRatioAction = ptr("stop")
		})
}

func TestQBPreferencePayloadRejectsBadValues(t *testing.T) {
	if _, err := qbPreferencePayload(&downloadsv1alpha1.QBittorrentPreferences{MaxRatio: ptr("abc")}); err == nil {
		t.Error("expected error for unparseable maxRatio")
	}
	if _, err := qbPreferencePayload(&downloadsv1alpha1.QBittorrentPreferences{MaxRatioAction: ptr("nope")}); err == nil {
		t.Error("expected error for unknown maxRatioAction")
	}
}

func TestQBPreferencePayloadOmitsUnsetFields(t *testing.T) {
	got, err := qbPreferencePayload(&downloadsv1alpha1.QBittorrentPreferences{MaxRatioAction: ptr("removeWithContent")})
	if err != nil {
		t.Fatalf("qbPreferencePayload: %v", err)
	}
	if len(got) != 1 || got["max_ratio_act"] != 3 {
		t.Errorf("expected only max_ratio_act=3, got %#v", got)
	}
}

func TestQBObserveReadsWithoutChangingPreferencesOrCategories(t *testing.T) {
	for _, tt := range []struct{ name, path, failure, reason string }{
		{"matching", "/existing", "", "Observed"},
		{"empty spec", "", "", "Observed"},
		{"empty preferences", "", "", "Observed"},
		{"invalid ratio", "", "", "SyncFailed"},
		{"drift", "/new", "", "DriftDetected"},
		{"preferences unavailable", "/existing", "/api/v2/app/preferences", "SyncFailed"},
		{"categories unavailable", "/existing", "/api/v2/torrents/categories", "SyncFailed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var requests []string
			var mu sync.Mutex
			ln, err := net.Listen("tcp", net.JoinHostPort(routableAddr(t), "0"))
			if err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				requests = append(requests, r.Method+" "+r.URL.Path)
				mu.Unlock()
				if r.URL.Path == tt.failure {
					w.WriteHeader(500)
					return
				}
				switch r.URL.Path {
				case "/api/v2/auth/login":
					http.SetCookie(w, &http.Cookie{Name: "SID", Value: "session", Path: "/"})
					_, _ = w.Write([]byte("Ok."))
				case "/api/v2/app/version":
					_, _ = w.Write([]byte("v5"))
				case "/api/v2/app/preferences":
					_, _ = w.Write([]byte(`{"save_path":"/existing","unmanaged":true}`))
				case "/api/v2/torrents/categories":
					_, _ = w.Write([]byte(`{"tv":{"savePath":"/existing"}}`))
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			_ = srv.Listener.Close()
			srv.Listener = ln
			srv.Start()
			defer srv.Close()
			scheme := runtime.NewScheme()
			if err := downloadsv1alpha1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			policy := "observe"
			cfg := &downloadsv1alpha1.QBittorrentConfig{ObjectMeta: metav1.ObjectMeta{Name: "qbit", Namespace: "media"}}
			cfg.Spec.Connection = downloadsv1alpha1.QBittorrentConnection{URL: srv.URL,
				UsernameSecretRef: commonv1alpha1.SecretKeyRef{Name: "creds", Key: "user"}, PasswordSecretRef: commonv1alpha1.SecretKeyRef{Name: "creds", Key: "password"}}
			cfg.Spec.Reconcile = &commonv1alpha1.ReconcileConfig{DriftPolicy: &policy}
			cfg.Spec.Preferences = &downloadsv1alpha1.QBittorrentPreferences{SavePath: tt.path}
			cfg.Spec.Categories = []downloadsv1alpha1.QBittorrentCategory{{Name: "tv", SavePath: tt.path}}
			switch tt.name {
			case "empty spec":
				cfg.Spec.Preferences = nil
				cfg.Spec.Categories = nil
			case "empty preferences":
				cfg.Spec.Preferences = &downloadsv1alpha1.QBittorrentPreferences{}
				cfg.Spec.Categories = nil
			case "invalid ratio":
				cfg.Spec.Preferences.MaxRatio = ptr("invalid")
			}
			if tt.name == "drift" {
				cfg.Spec.Categories = append(cfg.Spec.Categories, downloadsv1alpha1.QBittorrentCategory{Name: "missing"})
			}
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "media"}, Data: map[string][]byte{"user": []byte("user"), "password": []byte("password")}}
			c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(cfg).WithObjects(cfg, secret).Build()
			rec := &QBittorrentConfigReconciler{Client: c, Scheme: scheme}
			key := types.NamespacedName{Name: cfg.Name, Namespace: cfg.Namespace}
			if _, err := rec.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
				t.Fatal(err)
			}
			if err := c.Get(context.Background(), key, cfg); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			for _, req := range requests {
				if strings.HasPrefix(req, "POST ") && req != "POST /api/v2/auth/login" {
					t.Errorf("observe wrote: %s", req)
				}
			}
			for _, condition := range cfg.Status.Conditions {
				if condition.Type == "Synced" && condition.Reason == tt.reason {
					return
				}
			}
			t.Fatalf("want %s, got %+v", tt.reason, cfg.Status.Conditions)
		})
	}
}
